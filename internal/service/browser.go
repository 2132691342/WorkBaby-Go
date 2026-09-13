// Package service 托管浏览器服务：拉起专用 Chromium（本机 Edge/Chrome + 独立 profile），CDP 驱动导航 / 交互 / 截图 / 快照。
// browser_* 工具与右侧浏览器面板共用同一实例；应用退出兜底回收。
package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"WorkBaby/internal/browser"
	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
)

// browserCmdTimeout 单条 CDP 命令默认超时；页面导航类给足加载时间。
const (
	browserCmdTimeout   = 15 * time.Second
	browserNavTimeout   = 30 * time.Second
	browserStartTimeout = 20 * time.Second
	browserViewportW    = 1280
	browserViewportH    = 860
	browserTextLimit    = 4000 // 快照正文上限（rune）
	browserElementLimit = 80   // 快照可交互元素上限
	browserClickTimeout = 30 * time.Second
)

// BrowserService 浏览器托管。
type BrowserService struct {
	home string // {home}：profile 目录挂这

	mu      sync.Mutex
	cdp     *browser.CDP
	proc    *exec.Cmd
	browser string
	started time.Time
	gen     int // Stop 代数：防「首启自重启」重连与用户主动 Stop 竞态
}

// NewBrowserService 构造。
func NewBrowserService(home string) *BrowserService {
	return &BrowserService{home: home}
}

// browserCandidates Windows 下本机 Chromium 系浏览器探测顺序（环境变量可强制指定）。
func browserCandidates() []string {
	if p := strings.TrimSpace(os.Getenv("WB_BROWSER_EXE")); p != "" {
		return []string{p}
	}
	prog := os.Getenv("ProgramFiles")
	progX86 := os.Getenv("ProgramFiles(x86)")
	local := os.Getenv("LocalAppData")
	paths := make([]string, 0, 6)
	for _, root := range []string{prog, progX86, local} {
		if root == "" {
			continue
		}
		paths = append(paths,
			filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe"),
		)
	}
	return paths
}

// Start 拉起托管浏览器并 attach 页面；已在跑则直接返回。
func (s *BrowserService) Start() (*domain.BrowserStatusRESP, error) {
	s.mu.Lock()
	if s.cdp != nil {
		s.mu.Unlock()
		return s.statusLocked(), nil
	}
	s.mu.Unlock()

	exe := ""
	for _, p := range browserCandidates() {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			exe = p
			break
		}
	}
	if exe == "" {
		return nil, pkg.New(domain.ErrBrowserLaunch.Code, "未找到本机 Chrome/Edge，可用环境变量 WB_BROWSER_EXE 指定", "")
	}
	profile := filepath.Join(s.home, "browser-profile")
	_ = os.MkdirAll(profile, 0o755)

	// 残留实例清理：孤儿浏览器进程会占住 profile（启动器退出后其子进程树不受控），
	// 占住后新启动会触发「实例移交」导致调试端口不可用。匹配本应用专属
	// user-data-dir，不波及用户日常浏览器实例。
	s.killProfileInstances()
	_ = os.Remove(filepath.Join(profile, "DevToolsActivePort"))

	// 复用已运行实例：上次 launcher 换代后浏览器本体可能仍健康地跑着（端口文件在），
	// 直接 attach 即可，不必再拉新进程。
	if port, wsPath, err := s.readDevToolsPort(profile); err == nil {
		var reused *browser.CDP
		for i := 0; i < 3 && reused == nil; i++ {
			c, derr := browser.Dial(context.Background(), fmt.Sprintf("ws://127.0.0.1:%d%s", port, wsPath))
			if derr != nil {
				break
			}
			if _, aerr := c.Attach(context.Background(), "about:blank"); aerr == nil {
				reused = c
				break
			}
			c.Close()
			break
		}
		if reused != nil {
			s.mu.Lock()
			s.cdp = reused
			if s.browser == "" {
				s.browser = filepath.Base(exe)
			}
			s.mu.Unlock()
			return s.Status()
		}
		_ = os.Remove(filepath.Join(profile, "DevToolsActivePort"))
	}

	args := []string{
		"--remote-debugging-port=0",
		"--user-data-dir=" + profile,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-features=Translate",
		fmt.Sprintf("--window-size=%d,%d", browserViewportW, browserViewportH),
		"about:blank",
	}
	proc := exec.Command(exe, args...)
	if err := proc.Start(); err != nil {
		return nil, pkg.Wrap(domain.ErrBrowserLaunch.Code, "start browser", err)
	}

	// 端口发现：--remote-debugging-port=0 由 Chromium 写入 DevToolsActivePort（首行端口、次行 ws path）
	port, wsPath, err := s.waitDevToolsPort(profile)
	if err != nil {
		treeKill(proc)
		return nil, err
	}
	// HTTP 端点就绪略晚于端口文件落盘：短重试兜底
	var cdp *browser.CDP
	for i := 0; i < 5; i++ {
		cdp, err = browser.Dial(context.Background(), fmt.Sprintf("ws://127.0.0.1:%d%s", port, wsPath))
		if err == nil {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if err != nil {
		treeKill(proc)
		return nil, err
	}
	if _, err := cdp.Attach(context.Background(), "about:blank"); err != nil {
		cdp.Close()
		treeKill(proc)
		return nil, err
	}

	s.mu.Lock()
	s.cdp = cdp
	s.proc = proc
	s.browser = filepath.Base(exe)
	s.started = time.Now()
	s.mu.Unlock()

	// 进程退出监听：Chromium「启动器进程」模型——启动器拉起真正的浏览器进程后
	// 自己退出（~1s），退出 ≠ 下线。改为周期性健康探测，连接死透才回落 stopped。
	go func() {
		_ = proc.Wait()
		s.mu.Lock()
		if s.proc == proc {
			s.proc = nil
		}
		myGen := s.gen
		s.mu.Unlock()
		for {
			time.Sleep(3 * time.Second)
			s.mu.Lock()
			alive := s.gen == myGen && s.cdp != nil
			cur := s.cdp
			s.mu.Unlock()
			if !alive || !s.ping(cur) {
				s.mu.Lock()
				if s.gen == myGen && s.cdp == cur {
					s.cdp = nil
				}
				s.mu.Unlock()
				return
			}
		}
	}()

	return s.Status()
}

// ping 轻量 CDP 健康探测。
func (s *BrowserService) ping(c *browser.CDP) bool {
	cctx, cancel := cmdCtx(context.Background(), 3*time.Second)
	defer cancel()
	return c.Command(cctx, "Runtime.evaluate", map[string]any{
		"expression": "1", "returnByValue": true,
	}, &struct{}{}) == nil
}

// killProfileInstances 终止占用托管 profile 的残留浏览器进程（best-effort；
// PowerShell CIM 按命令行精确匹配，不会波及用户日常浏览器实例）。
func (s *BrowserService) killProfileInstances() {
	profile := filepath.Join(s.home, "browser-profile")
	script := fmt.Sprintf(
		`Get-CimInstance Win32_Process -Filter "Name='msedge.exe' OR Name='chrome.exe'" | `+
			`Where-Object { $_.CommandLine -like '*%s*' } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }`,
		profile,
	)
	c := exec.Command("powershell", "-NoProfile", "-Command", script)
	_ = c.Run()
}

// treeKill 进程树终止（主进程 + 全部子进程），避免孤儿进程占住 profile。
func treeKill(proc *exec.Cmd) {
	if proc == nil || proc.Process == nil {
		return
	}
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(proc.Process.Pid)).Run()
}

// waitDevToolsPort 轮询 DevToolsActivePort 文件直至就绪。
func (s *BrowserService) waitDevToolsPort(profile string) (int, string, error) {
	deadline := time.Now().Add(browserStartTimeout)
	for time.Now().Before(deadline) {
		port, wsPath, err := s.readDevToolsPort(profile)
		if err == nil {
			return port, wsPath, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return 0, "", pkg.New(domain.ErrBrowserLaunch.Code, "browser devtools port not ready in time", profile)
}

// readDevToolsPort 单次读取 DevToolsActivePort（首行端口、次行 ws path）。
func (s *BrowserService) readDevToolsPort(profile string) (int, string, error) {
	bs, err := os.ReadFile(filepath.Join(profile, "DevToolsActivePort"))
	if err != nil {
		return 0, "", err
	}
	lines := strings.Split(strings.TrimSpace(string(bs)), "\n")
	if len(lines) < 2 {
		return 0, "", fmt.Errorf("devtools port file incomplete")
	}
	var port int
	if _, err := fmt.Sscanf(strings.TrimSpace(lines[0]), "%d", &port); err != nil || port <= 0 {
		return 0, "", fmt.Errorf("devtools port invalid")
	}
	return port, strings.TrimSpace(lines[1]), nil
}

// cmdCtx 命令级超时（caller 可给更长的导航窗）。
func cmdCtx(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, d)
}

// Navigate 当前页导航。
func (s *BrowserService) Navigate(ctx context.Context, rawURL string) error {
	c, err := s.client()
	if err != nil {
		return err
	}
	u := rawURL
	if u != "" && !strings.Contains(u, "://") {
		// 无 scheme 的输入按搜索/域名处理：含空格或非域名形态走搜索引擎
		u = normalizeBrowserURL(u)
	}
	cctx, cancel := cmdCtx(ctx, browserNavTimeout)
	defer cancel()
	return c.Command(cctx, "Page.navigate", map[string]any{"url": u}, nil)
}

// normalizeBrowserURL 无 scheme 输入补全：含空格 → 搜索引擎查询；否则默认 https。
func normalizeBrowserURL(input string) string {
	if strings.ContainsAny(input, " \t") {
		return "https://www.bing.com/search?q=" + strings.ReplaceAll(input, " ", "+")
	}
	return "https://" + input
}

// Click 视口坐标点击（CSS 像素，mousePressed + mouseReleased）。
func (s *BrowserService) Click(ctx context.Context, x, y int) error {
	c, err := s.client()
	if err != nil {
		return err
	}
	cctx, cancel := cmdCtx(ctx, browserClickTimeout)
	defer cancel()
	base := map[string]any{"x": x, "y": y, "button": "left", "clickCount": 1}
	if err := c.Command(cctx, "Input.dispatchMouseEvent", merge(base, map[string]any{"type": "mousePressed"}), nil); err != nil {
		return err
	}
	return c.Command(cctx, "Input.dispatchMouseEvent", merge(base, map[string]any{"type": "mouseReleased"}), nil)
}

// Type 向焦点元素插入文本（Input.insertText，CJK 安全），submit 时补回车。
func (s *BrowserService) Type(ctx context.Context, text string, submit bool) error {
	c, err := s.client()
	if err != nil {
		return err
	}
	cctx, cancel := cmdCtx(ctx, browserCmdTimeout)
	defer cancel()
	if text != "" {
		if err := c.Command(cctx, "Input.insertText", map[string]any{"text": text}, nil); err != nil {
			return err
		}
	}
	if submit {
		return s.sendKey(cctx, c, "Enter")
	}
	return nil
}

// Key 发送按键（Enter / Tab / Escape / 方向键等）。
func (s *BrowserService) Key(ctx context.Context, name string) error {
	c, err := s.client()
	if err != nil {
		return err
	}
	cctx, cancel := cmdCtx(ctx, browserCmdTimeout)
	defer cancel()
	return s.sendKey(cctx, c, name)
}

// keyCodes 常用键的 CDP 映射（key/code/windowsVirtualKeyCode）。
var keyCodes = map[string][3]any{
	"Enter":      {"Enter", "Enter", 13},
	"Tab":        {"Tab", "Tab", 9},
	"Escape":     {"Escape", "Escape", 27},
	"Backspace":  {"Backspace", "Backspace", 8},
	"Delete":     {"Delete", "Delete", 46},
	"ArrowUp":    {"ArrowUp", "ArrowUp", 38},
	"ArrowDown":  {"ArrowDown", "ArrowDown", 40},
	"ArrowLeft":  {"ArrowLeft", "ArrowLeft", 37},
	"ArrowRight": {"ArrowRight", "ArrowRight", 39},
	"PageUp":     {"PageUp", "PageUp", 33},
	"PageDown":   {"PageDown", "PageDown", 34},
	"Home":       {"Home", "Home", 36},
	"End":        {"End", "End", 35},
}

// sendKey 组合 rawKeyDown / keyUp 发送按键。
func (s *BrowserService) sendKey(ctx context.Context, c *browser.CDP, name string) error {
	kk, ok := keyCodes[name]
	if !ok {
		return pkg.New(domain.ErrBrowserInvalidArg.Code, "unsupported key", name)
	}
	down := map[string]any{"type": "rawKeyDown", "key": kk[0], "code": kk[1], "windowsVirtualKeyCode": kk[2]}
	up := map[string]any{"type": "keyUp", "key": kk[0], "code": kk[1], "windowsVirtualKeyCode": kk[2]}
	if err := c.Command(ctx, "Input.dispatchKeyEvent", down, nil); err != nil {
		return err
	}
	return c.Command(ctx, "Input.dispatchKeyEvent", up, nil)
}

// Scroll 滚轮滚动（正数向下/向右）。
func (s *BrowserService) Scroll(ctx context.Context, dx, dy int) error {
	c, err := s.client()
	if err != nil {
		return err
	}
	cctx, cancel := cmdCtx(ctx, browserCmdTimeout)
	defer cancel()
	return c.Command(cctx, "Input.dispatchMouseEvent", map[string]any{
		"type": "mouseWheel", "x": browserViewportW / 2, "y": browserViewportH / 2,
		"deltaX": -dx, "deltaY": -dy,
	}, nil)
}

// snapshotJS 结构化快照：标题 + 可见正文 + 可交互元素（含视口坐标，模型点选用）。
const snapshotJS = `(function(){
	const vis = el => { const r = el.getBoundingClientRect(); return r.width>0 && r.height>0 && r.bottom>0 && r.right>0 && r.top<innerHeight && r.left<innerWidth; };
	const els = [];
	const sel = 'a[href],button,input:not([type=hidden]),textarea,select,[role=button],[onclick]';
	document.querySelectorAll(sel).forEach(el => {
		if (!vis(el) || els.length >= 80) return;
		const r = el.getBoundingClientRect();
		const label = (el.innerText || el.value || el.placeholder || el.getAttribute('aria-label') || '').trim().replace(/\s+/g,' ').slice(0,60);
		els.push({tag: el.tagName.toLowerCase(), text: label, x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.width), h: Math.round(r.height)});
	});
	const text = (document.body ? document.body.innerText : '').replace(/\n{3,}/g,'\n\n').slice(0,4000);
	return {url: location.href, title: document.title, text: text, elements: els};
})()`

// Snapshot 结构化页面快照（工具与面板共用）。
func (s *BrowserService) Snapshot(ctx context.Context) (*BrowserSnapshot, error) {
	c, err := s.client()
	if err != nil {
		return nil, err
	}
	cctx, cancel := cmdCtx(ctx, browserCmdTimeout)
	defer cancel()
	var out struct {
		Result struct {
			Value struct {
				URL      string                      `json:"url"`
				Title    string                      `json:"title"`
				Text     string                      `json:"text"`
				Elements []domain.BrowserElementRESP `json:"elements"`
			} `json:"value"`
		} `json:"result"`
	}
	if err := c.Command(cctx, "Runtime.evaluate", map[string]any{
		"expression": snapshotJS, "returnByValue": true,
	}, &out); err != nil {
		return nil, err
	}
	v := out.Result.Value
	return &BrowserSnapshot{URL: v.URL, Title: v.Title, Text: v.Text, Elements: v.Elements}, nil
}

// BrowserSnapshot 快照载荷。
type BrowserSnapshot struct {
	URL      string                      `json:"url"`
	Title    string                      `json:"title"`
	Text     string                      `json:"text"`
	Elements []domain.BrowserElementRESP `json:"elements"`
}

// Screenshot 视口截图（JPEG base64）+ CSS 视口尺寸。
func (s *BrowserService) Screenshot(ctx context.Context) (*domain.BrowserScreenshotRESP, error) {
	c, err := s.client()
	if err != nil {
		return nil, err
	}
	cctx, cancel := cmdCtx(ctx, browserCmdTimeout)
	defer cancel()
	var size struct {
		Result struct {
			Value struct {
				W int `json:"w"`
				H int `json:"h"`
			} `json:"value"`
		} `json:"result"`
	}
	if err := c.Command(cctx, "Runtime.evaluate", map[string]any{
		"expression": "({w: window.innerWidth, h: window.innerHeight})", "returnByValue": true,
	}, &size); err != nil {
		return nil, err
	}
	var shot struct {
		Data string `json:"data"`
	}
	if err := c.Command(cctx, "Page.captureScreenshot", map[string]any{
		"format": "jpeg", "quality": 60,
	}, &shot); err != nil {
		return nil, err
	}
	return &domain.BrowserScreenshotRESP{
		Image: shot.Data,
		W:     size.Result.Value.W,
		H:     size.Result.Value.H,
	}, nil
}

// Status 运行态（URL/Title 借快照口令取，面板轮询友好）。
func (s *BrowserService) Status() (*domain.BrowserStatusRESP, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked(), nil
}

// statusLocked 调用方须持锁；URL/Title 尽力取（无连接时留空）。
func (s *BrowserService) statusLocked() *domain.BrowserStatusRESP {
	resp := &domain.BrowserStatusRESP{Running: s.cdp != nil, Browser: s.browser}
	if s.cdp == nil {
		return resp
	}
	cctx, cancel := cmdCtx(context.Background(), 3*time.Second)
	defer cancel()
	var out struct {
		Result struct {
			Value struct {
				URL   string `json:"url"`
				Title string `json:"title"`
			} `json:"value"`
		} `json:"result"`
	}
	if err := s.cdp.Command(cctx, "Runtime.evaluate", map[string]any{
		"expression": "({url: location.href, title: document.title})", "returnByValue": true,
	}, &out); err == nil {
		resp.URL = out.Result.Value.URL
		resp.Title = out.Result.Value.Title
	}
	return resp
}

// Stop 关闭托管浏览器（用户主动 / 应用退出）：进程树终止 + 清残留实例，
// 防孤儿进程占住 profile；gen++ 使自重启重连协程放弃。
func (s *BrowserService) Stop() error {
	s.mu.Lock()
	cdp, proc := s.cdp, s.proc
	s.cdp, s.proc = nil, nil
	s.gen++
	s.mu.Unlock()
	if cdp != nil {
		cdp.Close()
	}
	treeKill(proc)
	s.killProfileInstances()
	return nil
}

// Shutdown 应用退出兜底回收。
func (s *BrowserService) Shutdown() { _ = s.Stop() }

// client 取 CDP 连接（未启动报 8701）。
func (s *BrowserService) client() (*browser.CDP, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cdp == nil {
		return nil, domain.ErrBrowserNotRunning
	}
	return s.cdp, nil
}

// merge 浅合并两个 map（右侧覆盖）。
func merge(base, extra map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}
