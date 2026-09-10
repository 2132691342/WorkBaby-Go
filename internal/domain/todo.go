package domain

import "WorkBaby/internal/pkg"

// TodoItem 会话内计划项（todo 工具的唯一有状态数据； Todo 计划工具）。
// v1 以会话内存存储；跨进程恢复/历史归档留待持久化迭代。
type TodoItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

// TodoStateRESP 计划状态快照（供前端进度卡 / 后续 API 复用）。
type TodoStateRESP struct {
	SessionID string     `json:"session_id"`
	Items     []TodoItem `json:"items"`
	DoneCount int        `json:"done_count"`
	Total     int        `json:"total"`
}

// ErrTodoItemNotFound 勾选不存在的计划项（前端面板可能持有过期快照）。
var ErrTodoItemNotFound = pkg.New(5009, "todo item not found", "")
