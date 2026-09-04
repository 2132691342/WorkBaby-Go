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
	items, _ := s.Load(sessionID)
	done := 0
	for _, it := range items {
		if it.Done {
			done++
		}
	}
	return domain.TodoStateRESP{SessionID: sessionID, Items: items, DoneCount: done, Total: len(items)}
}
