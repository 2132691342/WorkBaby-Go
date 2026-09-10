package service

import (
	"sync"

	"WorkBaby/internal/domain"
)

// SessionTodoStore 内存版会话计划存储（实现 internal/tool/todo.Store）。
//
// todo 工具与后续前端进度卡共享同一实例；v1 内存语义：会话关闭即清空，
// 持久化（跨重启/归档）留待迭代。
type SessionTodoStore struct {
	mu    sync.Mutex
	items map[string][]domain.TodoItem
}

// NewSessionTodoStore 构造。
func NewSessionTodoStore() *SessionTodoStore {
	return &SessionTodoStore{items: map[string][]domain.TodoItem{}}
}

// Load 返回该会话当前计划（副本，避免调用方改内部切片）。
func (s *SessionTodoStore) Load(sessionID string) ([]domain.TodoItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.TodoItem(nil), s.items[sessionID]...), nil
}

// Save 覆盖该会话计划。
func (s *SessionTodoStore) Save(sessionID string, items []domain.TodoItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[sessionID] = append([]domain.TodoItem(nil), items...)
	return nil
}

// State 返回会话计划快照（done/total 已统计）。
func (s *SessionTodoStore) State(sessionID string) domain.TodoStateRESP {
	return stateOf(sessionID, s.load(sessionID))
}

// Toggle 用户手动勾选/取消某条待办（会话计划面板交互）。
// 与模型侧的 todo 工具写同一份状态，下一轮 system 注入会带上最新进度。
func (s *SessionTodoStore) Toggle(sessionID, itemID string) (domain.TodoStateRESP, error) {
	s.mu.Lock()
	items := s.items[sessionID]
	found := false
	for i := range items {
		if items[i].ID == itemID {
			items[i].Done = !items[i].Done
			found = true
			break
		}
	}
	if found {
		s.items[sessionID] = items
	}
	s.mu.Unlock()
	if !found {
		return domain.TodoStateRESP{SessionID: sessionID}, domain.ErrTodoItemNotFound
	}
	return s.State(sessionID), nil
}

// load 内部读取（已在锁内调用时不要用；公开 Load 走副本语义）。
func (s *SessionTodoStore) load(sessionID string) []domain.TodoItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.TodoItem(nil), s.items[sessionID]...)
}

// stateOf 由 items 组装快照。
func stateOf(sessionID string, items []domain.TodoItem) domain.TodoStateRESP {
	done := 0
	for _, it := range items {
		if it.Done {
			done++
		}
	}
	return domain.TodoStateRESP{SessionID: sessionID, Items: items, DoneCount: done, Total: len(items)}
}
