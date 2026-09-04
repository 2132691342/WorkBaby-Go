package service

import (
	"context"
	"sync"
)

// runRegistry 记录当前正在跑的 run（按 sessionID），提供 Cancel 接口中断后端 Agent 循环。
//
// 设计要点：
//   - 单用户桌面应用：同一 sessionID 同时只可能有一个 run（SendStream 由 ChatService.mu 串行化）；
//     set 重复注册同一 sessionID 时，cancel 旧 ctx（防 ctx 泄漏），覆盖为新 ctx。
//   - cancel 后立即 delete：保证 CancelStream 是"一次性"语义，再次调幂等（找不到就 nil）。
//   - 记录 runID：steering 注入需要知道当前活动 run 的身份。
//   - 极小：单独成文件便于单测（不需要 repo/bus/reg 全套依赖）。
type runRegistry struct {
	mu   sync.Mutex
	runs map[string]runEntry
}

// runEntry 活动 run：cancel 函数 + runID（注入/事件路由需要）。
type runEntry struct {
	runID  string
	cancel context.CancelFunc
}

func newRunRegistry() *runRegistry { return &runRegistry{runs: map[string]runEntry{}} }

// set 注册 sessionID 的 run；覆盖前会 cancel 旧 ctx（兜底防泄漏）。
func (r *runRegistry) set(sessionID, runID string, cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if prev, ok := r.runs[sessionID]; ok {
		prev.cancel()
	}
	r.runs[sessionID] = runEntry{runID: runID, cancel: cancel}
}

// delete 注销（run 自然结束时调用）。
func (r *runRegistry) delete(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.runs, sessionID)
}

// lookup 返回该会话当前活动 run 的 runID；无活动 run 返回 false。
func (r *runRegistry) lookup(sessionID string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.runs[sessionID]
	if !ok || e.runID == "" {
		return "", false
	}
	return e.runID, true
}

// cancel 取消指定 session 的 run；返回是否找到。找不到（已结束/不存在）不报错。
func (r *runRegistry) cancel(sessionID string) bool {
	r.mu.Lock()
	e, ok := r.runs[sessionID]
	if ok {
		delete(r.runs, sessionID)
	}
	r.mu.Unlock()
	if !ok {
		return false
	}
	e.cancel()
	return true
}
