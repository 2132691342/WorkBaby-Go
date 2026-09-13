// Package api 终端面板 HTTP 接口：开终端（幂等）/ 跑命令 / 拉输出 / 关终端。
package api

import (
	"WorkBaby/internal/domain"
)

// TerminalOpenReq 开启终端入参。
type TerminalOpenReq struct {
	SessionID string `json:"session_id"`
}

// TerminalOpen 打开（复用）会话终端，cwd = 会话工作区根。
func (h *Handler) TerminalOpen(req TerminalOpenReq) (*domain.TerminalRESP, error) {
	return h.termSvc.Open(req.SessionID, h.workspaceSvc.Dir(req.SessionID))
}

// TerminalRunReq 运行命令入参。
type TerminalRunReq struct {
	ID      string `json:"id"`
	Command string `json:"command"`
}

// TerminalRun 向终端写入一行命令。
func (h *Handler) TerminalRun(req TerminalRunReq) error {
	return h.termSvc.Run(req.ID, req.Command)
}

// TerminalOutput 增量输出（since = 上次返回的 seq）。
func (h *Handler) TerminalOutput(id string, since int64) (*domain.TerminalOutputRESP, error) {
	return h.termSvc.Output(id, since)
}

// TerminalStop 关闭终端。
func (h *Handler) TerminalStop(id string) error {
	return h.termSvc.Stop(id)
}
