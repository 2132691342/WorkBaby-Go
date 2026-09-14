// Package api Git 面板 HTTP 接口：概览 / 分支 / diff / 暂存 / 提交。
// 工作区跟随会话绑定（workspaceSvc.Dir），不落库、不改仓库数据。
package api

import (
	"WorkBaby/internal/domain"
	"WorkBaby/internal/service"
)

// gitSvc 构造指向当前会话工作区的 Git 服务（无状态，按需新建）。
func (h *Handler) gitSvc(sessionID string) *service.GitService {
	return service.NewGitService(func() string { return h.workspaceSvc.Dir(sessionID) })
}

// GitOverview 面板首屏聚合：分支 + 变更 + 本地分支 + 最近提交。
func (h *Handler) GitOverview(sessionID string) (*domain.GitOverviewRESP, error) {
	return h.gitSvc(sessionID).Overview(h.ctx)
}

// GitSwitchReq 带 session 的分支切换入参（HTTP body）。
type GitSwitchReq struct {
	SessionID string `json:"session_id"`
	Branch    string `json:"branch"`
	Create    bool   `json:"create"`
}

// GitSwitch 切换/新建分支。
func (h *Handler) GitSwitch(req GitSwitchReq) error {
	return h.gitSvc(req.SessionID).Switch(h.ctx, &domain.GitSwitchREQ{Branch: req.Branch, Create: req.Create})
}

// GitDiff 单文件或全仓 unified diff。
func (h *Handler) GitDiff(sessionID, path string, staged bool) (*domain.GitDiffRESP, error) {
	out, err := h.gitSvc(sessionID).Diff(h.ctx, &domain.GitDiffREQ{Path: path, Staged: staged})
	if err != nil {
		return nil, err
	}
	return &domain.GitDiffRESP{Diff: out}, nil
}

// GitWriteReq 通用写操作入参（stage/unstage/discard 共用）。
type GitWriteReq struct {
	SessionID string `json:"session_id"`
	Path      string `json:"path"` // 空 = 全部（仅 stage）
}

// GitStage 暂存（path 空 = add -A）。
func (h *Handler) GitStage(req GitWriteReq) error {
	return h.gitSvc(req.SessionID).Stage(h.ctx, req.Path)
}

// GitUnstage 取消暂存。
func (h *Handler) GitUnstage(req GitWriteReq) error {
	return h.gitSvc(req.SessionID).Unstage(h.ctx, req.Path)
}

// GitDiscard 丢弃工作区改动。
func (h *Handler) GitDiscard(req GitWriteReq) error {
	return h.gitSvc(req.SessionID).Discard(h.ctx, req.Path)
}

// GitCommitReq 提交入参（HTTP body）。
type GitCommitReq struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
	StageAll  bool   `json:"stage_all"`
}

// GitCommit 提交暂存区（StageAll 先 add -A）。
func (h *Handler) GitCommit(req GitCommitReq) error {
	return h.gitSvc(req.SessionID).Commit(h.ctx, &domain.GitCommitREQ{Message: req.Message, StageAll: req.StageAll})
}

// GitLog 提交历史分页（limit / skip / all）。
func (h *Handler) GitLog(sessionID string, limit, skip int, all bool) ([]domain.GitCommitRESP, error) {
	return h.gitSvc(sessionID).Log(h.ctx, &domain.GitLogREQ{Limit: limit, Skip: skip, All: all}), nil
}

// GitCommitDetail 单次提交的元信息与变更文件清单。
func (h *Handler) GitCommitDetail(sessionID, hash string) (*domain.GitCommitDetailRESP, error) {
	return h.gitSvc(sessionID).CommitDetail(h.ctx, hash)
}

// GitCommitFileDiff 提交内单文件 diff（path 空 = 全量）。
func (h *Handler) GitCommitFileDiff(sessionID, hash, path string) (*domain.GitDiffRESP, error) {
	out, err := h.gitSvc(sessionID).CommitFileDiff(h.ctx, hash, path)
	if err != nil {
		return nil, err
	}
	return &domain.GitDiffRESP{Diff: out}, nil
}

// GitGraph 提交图谱（只读文本）。
func (h *Handler) GitGraph(sessionID string, limit int) (*domain.GitGraphRESP, error) {
	out, err := h.gitSvc(sessionID).Graph(h.ctx, limit)
	if err != nil {
		return nil, err
	}
	return &domain.GitGraphRESP{Graph: out}, nil
}

// GitBranchReq 分支操作入参（删除）。
type GitBranchReq struct {
	SessionID string `json:"session_id"`
	Branch    string `json:"branch"`
	Force     bool   `json:"force"`
}

// GitDeleteBranch 删除本地分支（当前分支被拒）。
func (h *Handler) GitDeleteBranch(req GitBranchReq) error {
	return h.gitSvc(req.SessionID).DeleteBranch(h.ctx, req.Branch, req.Force)
}
