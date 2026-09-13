// Package service 面板终端服务：每会话一个持久 cmd.exe（cd / 环境变量跨命令保留），
// stdin 写命令、stdout/stderr 合流读出；中文 Windows 下按 GBK 解码与回写，避免乱码。
package service

import (
	"io"
	"os/exec"
	"strings"
	"sync"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// terminalBufLimit 单终端输出缓冲上限（字节）：超限从头截断，Seq 继续推进。
const terminalBufLimit = 256 * 1024

// terminalMaxCount 全局并发终端上限（每会话一个，够用且防句柄泄漏）。
const terminalMaxCount = 8

// terminal 单个持久 shell 会话。
type terminal struct {
	id        string
	sessionID string
	cwd       string
	cmd       *exec.Cmd
	stdin     io.WriteCloser

	mu     sync.Mutex
	buf    string // 已解码输出（UTF-8，环形截断）
	seq    int64  // 累计产出字节数（含被截断丢弃的部分）
	closed bool
	done   chan struct{}
}

// TerminalService 终端管理（进程态，随应用退出回收）。
type TerminalService struct {
	mu    sync.Mutex
	terms map[string]*terminal
}

// NewTerminalService 构造。
func NewTerminalService() *TerminalService {
	return &TerminalService{terms: map[string]*terminal{}}
}

// Open 打开（或复用）会话终端：持久 cmd.exe，cwd 落在会话工作区根。
func (s *TerminalService) Open(sessionID, cwd string) (*domain.TerminalRESP, error) {
	s.mu.Lock()
	for _, t := range s.terms {
		if t.sessionID == sessionID && !t.isClosed() {
			s.mu.Unlock()
			return &domain.TerminalRESP{ID: t.id, SessionID: t.sessionID, Cwd: t.cwd}, nil
		}
	}
	if len(s.terms) >= terminalMaxCount {
		for id, t := range s.terms {
			if t.isClosed() {
				delete(s.terms, id)
			}
		}
		if len(s.terms) >= terminalMaxCount {
			s.mu.Unlock()
			return nil, domain.ErrTerminalLimit
		}
	}
	s.mu.Unlock()

	cmd := exec.Command("cmd.exe", "/Q", "/K", "prompt", "$P$G")
	cmd.Dir = cwd
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, pkg.Wrap(8501, "terminal stdin", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, pkg.Wrap(8501, "terminal stdout", err)
	}
	cmd.Stderr = cmd.Stdout // stderr 合流，保持时间序
	if err := cmd.Start(); err != nil {
		return nil, pkg.Wrap(8501, "terminal start", err)
	}

	t := &terminal{
		id:        pkg.NewID("TERM"),
		sessionID: sessionID,
		cwd:       cwd,
		cmd:       cmd,
		stdin:     stdin,
		done:      make(chan struct{}),
	}
	go t.pump(stdout)
	s.mu.Lock()
	s.terms[t.id] = t
	s.mu.Unlock()
	return &domain.TerminalRESP{ID: t.id, SessionID: sessionID, Cwd: cwd}, nil
}

// Run 向终端写入一行命令（编码 GBK；cmd 交互回显即自然誊录）。
func (s *TerminalService) Run(id, command string) error {
	t := s.get(id)
	if t == nil {
		return domain.ErrTerminalNotFound
	}
	if t.isClosed() {
		return domain.ErrTerminalClosed
	}
	line := strings.TrimRight(command, "\r\n") + "\r\n"
	encoded, err := simplifiedchinese.GBK.NewEncoder().String(line)
	if err != nil {
		encoded = line // 含不可编码字符时退原文（ASCII 命令不受影响）
	}
	_, werr := t.stdin.Write([]byte(encoded))
	return werr
}

// Output 拉取 since 游标后的输出增量；客户端持 Seq 循环轮询。
func (s *TerminalService) Output(id string, since int64) (*domain.TerminalOutputRESP, error) {
	t := s.get(id)
	if t == nil {
		return nil, domain.ErrTerminalNotFound
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	bufStart := t.seq - int64(len(t.buf))
	from := since - bufStart
	if from < 0 {
		from = 0
	}
	if from > int64(len(t.buf)) {
		from = int64(len(t.buf))
	}
	return &domain.TerminalOutputRESP{
		Seq:    t.seq,
		Output: t.buf[from:],
		Closed: t.closed,
	}, nil
}

// Stop 终止终端进程。
func (s *TerminalService) Stop(id string) error {
	s.mu.Lock()
	t := s.terms[id]
	delete(s.terms, id)
	s.mu.Unlock()
	if t == nil {
		return domain.ErrTerminalNotFound
	}
	t.close()
	return nil
}

// Shutdown 应用退出时回收全部终端进程。
func (s *TerminalService) Shutdown() {
	s.mu.Lock()
	all := make([]*terminal, 0, len(s.terms))
	for _, t := range s.terms {
		all = append(all, t)
	}
	s.terms = map[string]*terminal{}
	s.mu.Unlock()
	for _, t := range all {
		t.close()
	}
}

// get 按号取终端。
func (s *TerminalService) get(id string) *terminal {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.terms[id]
}

// pump 持续读子进程输出：GBK 解码后追加进缓冲（环形截断）；进程退出即标记关闭。
func (t *terminal) pump(r io.Reader) {
	defer close(t.done)
	dec := simplifiedchinese.GBK.NewDecoder()
	chunk := make([]byte, 4096)
	for {
		n, err := r.Read(chunk)
		if n > 0 {
			text, derr := dec.Bytes(chunk[:n])
			if derr != nil {
				text = chunk[:n]
			}
			t.mu.Lock()
			t.buf += string(text)
			t.seq += int64(n)
			if over := len(t.buf) - terminalBufLimit; over > 0 {
				t.buf = t.buf[over:]
			}
			t.mu.Unlock()
		}
		if err != nil {
			t.mu.Lock()
			t.closed = true
			t.mu.Unlock()
			return
		}
	}
}

// isClosed 终端是否已退出。
func (t *terminal) isClosed() bool {
	select {
	case <-t.done:
		return true
	default:
		return false
	}
}

// close 终止进程并标记关闭（幂等）。
func (t *terminal) close() {
	t.mu.Lock()
	already := t.closed
	t.closed = true
	t.mu.Unlock()
	if already {
		return
	}
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}
}
