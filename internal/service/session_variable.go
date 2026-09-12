package service

import (
	"context"
	"sort"
	"strings"
	"sync"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/repo"
)

// SessionVarService 会话变量读写（三层 State：user 跨会话 / session 会话内 / temp run 内）。
//
// 方法签名对齐工具与能力注入接口（都消费 []SessionVarItem），RESP 包装留在 api 层。
// 变量会作为 system 段注入，因此写入路径必须做规模校验——不设限等于给上下文预算开后门。
type SessionVarService struct {
	repo *repo.SessionVariableRepo

	tmu  sync.Mutex
	temp map[string]map[string]string // runID → key → value（run 级临时态，不落库）
}

// NewSessionVarService 构造；repo 为 nil 时读写退化为空操作（temp 仍可用）。
func NewSessionVarService(r *repo.SessionVariableRepo) *SessionVarService {
	return &SessionVarService{repo: r, temp: map[string]map[string]string{}}
}

// List 返回会话级 + 用户级合并清单（各条目带 scope），按 scope 再按 key 升序。
func (s *SessionVarService) List(ctx context.Context, sessionID string) ([]domain.SessionVarItem, error) {
	if s == nil || s.repo == nil {
		return []domain.SessionVarItem{}, nil
	}
	sessionRows, err := s.repo.List(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	userRows, err := s.repo.List(ctx, domain.SessionVarUserScope)
	if err != nil {
		return nil, err
	}
	out := make([]domain.SessionVarItem, 0, len(sessionRows)+len(userRows))
	for _, r := range userRows {
		out = append(out, domain.SessionVarItem{Key: r.Key, Value: r.Value, Scope: domain.SessionVarScopeUser})
	}
	for _, r := range sessionRows {
		out = append(out, domain.SessionVarItem{Key: r.Key, Value: r.Value, Scope: domain.SessionVarScopeSession})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// Set 写入/覆盖一个变量并返回最新清单（key 空或超长、值超长、总量超限均拒绝）。
// scope=temp 由 SetTemp 承担（需要 runID），此处拒绝。
func (s *SessionVarService) Set(ctx context.Context, sessionID string, scope domain.SessionVarScope, key, value string) ([]domain.SessionVarItem, error) {
	if err := validateVar(key, value); err != nil {
		return nil, err
	}
	if scope == domain.SessionVarScopeTemp {
		return nil, pkg.New(domain.ErrSessionVarInvalid.Code, "temp 变量需经 run 上下文写入", key)
	}
	owner := ownerSessionID(sessionID, scope)
	if s == nil || s.repo == nil {
		return []domain.SessionVarItem{}, nil
	}
	if err := s.checkCount(ctx, owner, key); err != nil {
		return nil, err
	}
	if err := s.repo.Set(ctx, owner, key, value); err != nil {
		return nil, err
	}
	return s.List(ctx, sessionID)
}

// Delete 删除一个变量并返回最新清单；不存在返回 ErrSessionVarNotFound。
func (s *SessionVarService) Delete(ctx context.Context, sessionID string, scope domain.SessionVarScope, key string) ([]domain.SessionVarItem, error) {
	if s == nil || s.repo == nil {
		return []domain.SessionVarItem{}, nil
	}
	if scope == domain.SessionVarScopeTemp {
		return []domain.SessionVarItem{}, nil // temp 由 ClearTemp 统一清理
	}
	ok, err := s.repo.Delete(ctx, ownerSessionID(sessionID, scope), key)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, domain.ErrSessionVarNotFound
	}
	return s.List(ctx, sessionID)
}

// SetTemp 写入 run 级临时变量并返回该 run 的最新清单。
func (s *SessionVarService) SetTemp(runID, key, value string) ([]domain.SessionVarItem, error) {
	if err := validateVar(key, value); err != nil {
		return nil, err
	}
	if runID == "" {
		return nil, pkg.New(domain.ErrSessionVarInvalid.Code, "缺少 run 上下文，无法写入临时变量", key)
	}
	s.tmu.Lock()
	defer s.tmu.Unlock()
	if s.temp[runID] == nil {
		s.temp[runID] = map[string]string{}
	}
	if _, ok := s.temp[runID][key]; !ok && len(s.temp[runID]) >= domain.SessionVarCountMax {
		return nil, pkg.New(domain.ErrSessionVarInvalid.Code, "临时变量数量已达上限", key)
	}
	s.temp[runID][key] = value
	return tempItems(s.temp[runID]), nil
}

// ListTemp 返回某 run 的临时变量清单（run 不存在返回空）。
func (s *SessionVarService) ListTemp(runID string) []domain.SessionVarItem {
	if s == nil || runID == "" {
		return nil
	}
	s.tmu.Lock()
	defer s.tmu.Unlock()
	return tempItems(s.temp[runID])
}

// ClearTemp 清理某 run 的临时态（run 结束时调用）。
func (s *SessionVarService) ClearTemp(runID string) {
	if s == nil || runID == "" {
		return
	}
	s.tmu.Lock()
	delete(s.temp, runID)
	s.tmu.Unlock()
}

// checkCount 新增变量前校验该作用域数量上限；已存在（覆盖）直接放行。
func (s *SessionVarService) checkCount(ctx context.Context, owner, key string) error {
	rows, err := s.repo.List(ctx, owner)
	if err != nil {
		return err
	}
	if len(rows) >= domain.SessionVarCountMax {
		for _, r := range rows {
			if r.Key == key {
				return nil
			}
		}
		return pkg.New(domain.ErrSessionVarInvalid.Code, "会话变量数量已达上限", key)
	}
	return nil
}

// ownerSessionID 作用域 → 持久化落点的 session_id。
func ownerSessionID(sessionID string, scope domain.SessionVarScope) string {
	if scope == domain.SessionVarScopeUser {
		return domain.SessionVarUserScope
	}
	return sessionID
}

// validateVar 变量 key/value 规模校验。
func validateVar(key, value string) error {
	key = strings.TrimSpace(key)
	if key == "" || len(key) > domain.SessionVarKeyMaxLen {
		return pkg.New(domain.ErrSessionVarInvalid.Code, "变量名不能为空且不超过 128 字符", key)
	}
	if len([]rune(value)) > domain.SessionVarValueMaxLen {
		return pkg.New(domain.ErrSessionVarInvalid.Code, "变量值超长", key)
	}
	return nil
}

// tempItems map → 按 key 升序的视图列表。
func tempItems(m map[string]string) []domain.SessionVarItem {
	out := make([]domain.SessionVarItem, 0, len(m))
	for k, v := range m {
		out = append(out, domain.SessionVarItem{Key: k, Value: v, Scope: domain.SessionVarScopeTemp})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
