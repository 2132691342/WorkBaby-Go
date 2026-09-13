// Package browser 提供 browser_* 工具族：Agent 操控托管浏览器的入口。
//
// 工具与右侧浏览器面板共用同一 BrowserService 实例；snapshot 是模型感知页面的
// 首选（结构化文本 + 可交互元素坐标），screenshot 供多模态核对与前端渲染。
package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	svcbrowser "WorkBaby/internal/service"
	"WorkBaby/internal/tool"
)

// Suite 托管浏览器工具集（全部共享一个 service）。
type Suite struct{ svc *svcbrowser.BrowserService }

// New 构造工具集。
func New(svc *svcbrowser.BrowserService) *Suite { return &Suite{svc: svc} }

// All 返回全部 browser_* 工具。
func (s *Suite) All() []tool.Tool {
	return []tool.Tool{
		&navigateTool{s.svc}, &snapshotTool{s.svc}, &screenshotTool{s.svc},
		&clickTool{s.svc}, &typeTool{s.svc}, &keyTool{s.svc}, &scrollTool{s.svc},
	}
}

// ---- navigate ----

type navigateTool struct{ svc *svcbrowser.BrowserService }

func (t *navigateTool) Name() string              { return "browser_navigate" }
func (t *navigateTool) RiskLevel() tool.RiskLevel { return tool.RiskNetwork }

func (t *navigateTool) Description() string {
	return "打开托管浏览器并导航到指定 URL（未启动会自动拉起本机 Edge/Chrome）。无 scheme 时自动补 https://，含空格时按搜索词处理。"
}

func (t *navigateTool) Schema() tool.ToolSchema {
	return tool.ToolSchema{
		Name: t.Name(), Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"required": ["url"],
			"properties": {
				"url": {"type": "string", "description": "目标地址（完整 URL / 域名 / 搜索词）"}
			}
		}`),
	}
}

func (t *navigateTool) Meta() tool.ToolMeta {
	return tool.ToolMeta{TimeoutSec: 45, MaxResultChars: 2000, Category: tool.CategoryNetwork, Group: tool.GroupExec, ActivityDesc: "正在导航浏览器"}
}

func (t *navigateTool) Execute(ctx context.Context, args json.RawMessage) tool.ToolResult {
	var req struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &req); err != nil || strings.TrimSpace(req.URL) == "" {
		return tool.ToolResult{Err: pkg.New(domain.ErrBrowserInvalidArg.Code, "browser_navigate 需要 url", "")}
	}
	if err := t.svc.Navigate(ctx, req.URL); err != nil {
		return tool.ToolResult{Err: err}
	}
	snap, err := t.svc.Snapshot(ctx)
	if err != nil {
		return tool.ToolResult{Content: "已发起导航（页面快照暂不可用）", Err: nil}
	}
	return tool.ToolResult{
		Content: fmt.Sprintf("已打开：%s\n标题：%s\n\n%s", snap.URL, snap.Title, clip(snap.Text, 1500)),
		Data:    map[string]any{"url": snap.URL, "title": snap.Title},
	}
}

// ---- snapshot ----

type snapshotTool struct{ svc *svcbrowser.BrowserService }

func (t *snapshotTool) Name() string              { return "browser_snapshot" }
func (t *snapshotTool) RiskLevel() tool.RiskLevel { return tool.RiskReadOnly }

func (t *snapshotTool) Description() string {
	return "读取当前页面：URL、标题、可见正文与可交互元素清单（带坐标索引）。点击前先调用它选取元素 index。"
}

func (t *snapshotTool) Schema() tool.ToolSchema {
	return tool.ToolSchema{Name: t.Name(), Description: t.Description(), Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}
}

func (t *snapshotTool) Meta() tool.ToolMeta {
	return tool.ToolMeta{ReadOnly: true, TimeoutSec: 20, MaxResultChars: 6000, Category: tool.CategoryInfo, Group: tool.GroupExec, ActivityDesc: "正在读取页面"}
}

func (t *snapshotTool) Execute(ctx context.Context, args json.RawMessage) tool.ToolResult {
	snap, err := t.svc.Snapshot(ctx)
	if err != nil {
		return tool.ToolResult{Err: err}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "URL：%s\n标题：%s\n\n可见正文：\n%s\n\n可交互元素：\n", snap.URL, snap.Title, clip(snap.Text, 3000))
	for _, el := range snap.Elements {
		fmt.Fprintf(&b, "[%d] %s %q @(%d,%d %dx%d)\n", el.Index, el.Tag, el.Text, el.X, el.Y, el.W, el.H)
	}
	return tool.ToolResult{
		Content: b.String(),
		Data:    map[string]any{"url": snap.URL, "title": snap.Title, "elements": snap.Elements},
	}
}

// ---- screenshot ----

type screenshotTool struct{ svc *svcbrowser.BrowserService }

func (t *screenshotTool) Name() string              { return "browser_screenshot" }
func (t *screenshotTool) RiskLevel() tool.RiskLevel { return tool.RiskReadOnly }

func (t *screenshotTool) Description() string {
	return "截取当前页面视口（JPEG）。截图在浏览器面板可视化；需要读取页面内容时优先用 browser_snapshot。"
}

func (t *screenshotTool) Schema() tool.ToolSchema {
	return tool.ToolSchema{Name: t.Name(), Description: t.Description(), Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}
}

func (t *screenshotTool) Meta() tool.ToolMeta {
	return tool.ToolMeta{ReadOnly: true, TimeoutSec: 20, MaxResultChars: 500, Category: tool.CategoryInfo, Group: tool.GroupExec, ActivityDesc: "正在截取页面"}
}

func (t *screenshotTool) Execute(ctx context.Context, args json.RawMessage) tool.ToolResult {
	shot, err := t.svc.Screenshot(ctx)
	if err != nil {
		return tool.ToolResult{Err: err}
	}
	return tool.ToolResult{
		Content: fmt.Sprintf("已截取视口截图（%dx%d CSS 像素），见浏览器面板。", shot.W, shot.H),
		Data:    map[string]any{"image": shot.Image, "w": shot.W, "h": shot.H},
	}
}

// ---- click ----

type clickTool struct{ svc *svcbrowser.BrowserService }

func (t *clickTool) Name() string              { return "browser_click" }
func (t *clickTool) RiskLevel() tool.RiskLevel { return tool.RiskNetwork }

func (t *clickTool) Description() string {
	return "点击页面元素：优先传 snapshot 给出的 index；也可传视口坐标 x/y（CSS 像素）。"
}

func (t *clickTool) Schema() tool.ToolSchema {
	return tool.ToolSchema{
		Name: t.Name(), Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"index": {"type": "integer", "description": "browser_snapshot 元素序号（推荐）"},
				"x": {"type": "integer", "description": "视口横坐标（CSS 像素，index 缺省时用）"},
				"y": {"type": "integer", "description": "视口纵坐标"}
			}
		}`),
	}
}

func (t *clickTool) Meta() tool.ToolMeta {
	return tool.ToolMeta{TimeoutSec: 35, MaxResultChars: 1500, Category: tool.CategoryExec, Group: tool.GroupExec, ActivityDesc: "正在点击页面"}
}

func (t *clickTool) Execute(ctx context.Context, args json.RawMessage) tool.ToolResult {
	var req struct {
		Index *int `json:"index"`
		X     *int `json:"x"`
		Y     *int `json:"y"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return tool.ToolResult{Err: pkg.New(domain.ErrBrowserInvalidArg.Code, "browser_click args", err.Error())}
	}
	if req.Index != nil {
		snap, err := t.svc.Snapshot(ctx)
		if err != nil {
			return tool.ToolResult{Err: err}
		}
		for _, el := range snap.Elements {
			if el.Index == *req.Index {
				if err := t.svc.Click(ctx, el.X+el.W/2, el.Y+el.H/2); err != nil {
					return tool.ToolResult{Err: err}
				}
				return t.after(ctx, el.Tag, el.Text)
			}
		}
		return tool.ToolResult{Err: pkg.New(domain.ErrBrowserInvalidArg.Code, "元素序号不存在，请重新 browser_snapshot", fmt.Sprint(*req.Index))}
	}
	if req.X != nil && req.Y != nil {
		if err := t.svc.Click(ctx, *req.X, *req.Y); err != nil {
			return tool.ToolResult{Err: err}
		}
		return t.after(ctx, "point", "")
	}
	return tool.ToolResult{Err: pkg.New(domain.ErrBrowserInvalidArg.Code, "browser_click 需要 index 或 x/y", "")}
}

// after 点击后的页面状态回执（供模型确认动作生效）。
func (t *clickTool) after(ctx context.Context, tag, text string) tool.ToolResult {
	snap, err := t.svc.Snapshot(ctx)
	if err != nil {
		return tool.ToolResult{Content: "已点击 " + tag + "（页面快照暂不可用）"}
	}
	return tool.ToolResult{
		Content: fmt.Sprintf("已点击 %s %q。当前页：%s\n标题：%s\n\n%s", tag, text, snap.URL, snap.Title, clip(snap.Text, 800)),
		Data:    map[string]any{"url": snap.URL, "title": snap.Title},
	}
}

// ---- type ----

type typeTool struct{ svc *svcbrowser.BrowserService }

func (t *typeTool) Name() string              { return "browser_type" }
func (t *typeTool) RiskLevel() tool.RiskLevel { return tool.RiskNetwork }

func (t *typeTool) Description() string {
	return "向当前焦点输入框输入文本（先用 browser_click 聚焦输入框）；submit=true 时输入后回车。"
}

func (t *typeTool) Schema() tool.ToolSchema {
	return tool.ToolSchema{
		Name: t.Name(), Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"required": ["text"],
			"properties": {
				"text": {"type": "string", "description": "要输入的文本"},
				"submit": {"type": "boolean", "description": "输入后是否回车"}
			}
		}`),
	}
}

func (t *typeTool) Meta() tool.ToolMeta {
	return tool.ToolMeta{TimeoutSec: 35, MaxResultChars: 1500, Category: tool.CategoryExec, Group: tool.GroupExec, ActivityDesc: "正在输入文本"}
}

func (t *typeTool) Execute(ctx context.Context, args json.RawMessage) tool.ToolResult {
	var req struct {
		Text   string `json:"text"`
		Submit bool   `json:"submit"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return tool.ToolResult{Err: pkg.New(domain.ErrBrowserInvalidArg.Code, "browser_type args", err.Error())}
	}
	if err := t.svc.Type(ctx, req.Text, req.Submit); err != nil {
		return tool.ToolResult{Err: err}
	}
	return tool.ToolResult{Content: "已输入文本" + map[bool]string{true: "（已回车）", false: ""}[req.Submit]}
}

// ---- key ----

type keyTool struct{ svc *svcbrowser.BrowserService }

func (t *keyTool) Name() string              { return "browser_key" }
func (t *keyTool) RiskLevel() tool.RiskLevel { return tool.RiskNetwork }

func (t *keyTool) Description() string {
	return "发送单个按键：Enter / Tab / Escape / Backspace / Delete / ArrowUp/Down/Left/Right / PageUp / PageDown / Home / End。"
}

func (t *keyTool) Schema() tool.ToolSchema {
	return tool.ToolSchema{
		Name: t.Name(), Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"required": ["key"],
			"properties": {"key": {"type": "string", "description": "按键名（如 Enter / ArrowDown）"}
			}
		}`),
	}
}

func (t *keyTool) Meta() tool.ToolMeta {
	return tool.ToolMeta{TimeoutSec: 35, MaxResultChars: 1500, Category: tool.CategoryExec, Group: tool.GroupExec, ActivityDesc: "正在发送按键"}
}

func (t *keyTool) Execute(ctx context.Context, args json.RawMessage) tool.ToolResult {
	var req struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(args, &req); err != nil || strings.TrimSpace(req.Key) == "" {
		return tool.ToolResult{Err: pkg.New(domain.ErrBrowserInvalidArg.Code, "browser_key 需要 key", "")}
	}
	if err := t.svc.Key(ctx, req.Key); err != nil {
		return tool.ToolResult{Err: err}
	}
	return tool.ToolResult{Content: "已发送按键 " + req.Key}
}

// ---- scroll ----

type scrollTool struct{ svc *svcbrowser.BrowserService }

func (t *scrollTool) Name() string              { return "browser_scroll" }
func (t *scrollTool) RiskLevel() tool.RiskLevel { return tool.RiskReadOnly }

func (t *scrollTool) Description() string {
	return "滚动页面：direction=up/down/left/right，amount 为像素（缺省 600）。"
}

func (t *scrollTool) Schema() tool.ToolSchema {
	return tool.ToolSchema{
		Name: t.Name(), Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"direction": {"type": "string", "enum": ["up", "down", "left", "right"]},
				"amount": {"type": "integer", "description": "滚动像素，缺省 600"}
			}
		}`),
	}
}

func (t *scrollTool) Meta() tool.ToolMeta {
	return tool.ToolMeta{ReadOnly: true, TimeoutSec: 20, MaxResultChars: 500, Category: tool.CategoryInfo, Group: tool.GroupExec, ActivityDesc: "正在滚动页面"}
}

func (t *scrollTool) Execute(ctx context.Context, args json.RawMessage) tool.ToolResult {
	var req struct {
		Direction string `json:"direction"`
		Amount    int    `json:"amount"`
	}
	_ = json.Unmarshal(args, &req)
	if req.Amount <= 0 {
		req.Amount = 600
	}
	var dx, dy int
	switch req.Direction {
	case "up":
		dy = -req.Amount
	case "left":
		dx = -req.Amount
	case "right":
		dx = req.Amount
	default:
		dy = req.Amount
	}
	if err := t.svc.Scroll(ctx, dx, dy); err != nil {
		return tool.ToolResult{Err: err}
	}
	return tool.ToolResult{Content: "已滚动 " + req.Direction + " " + fmt.Sprint(req.Amount) + "px"}
}

// clip 按 rune 截断文本。
func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…(截断)"
}
