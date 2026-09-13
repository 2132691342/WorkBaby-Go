// Package domain 本文件：终端聚合（无独立表；进程态在内存，随应用生命周期）。
package domain

import "WorkBaby/internal/pkg"

// TerminalRESP 终端实例（open 幂等：同会话重复 open 复用同一终端）。
type TerminalRESP struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
}

// TerminalOutputRESP 输出增量：Seq 为缓冲区总字节游标，客户端带 Seq 拉增量。
type TerminalOutputRESP struct {
	Seq    int64  `json:"seq"`
	Output string `json:"output"`
	Closed bool   `json:"closed"`
}

// 包级错误变量；错误码段位 8500（终端面板）。
var (
	ErrTerminalNotFound = pkg.New(8501, "terminal not found", "")
	ErrTerminalLimit    = pkg.New(8502, "terminal limit reached", "")
	ErrTerminalClosed   = pkg.New(8503, "terminal already closed", "")
)
