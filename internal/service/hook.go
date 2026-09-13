// Package service UserHookService：用户钩子 CRUD + 子进程执行器。
// 协议：JSON 载荷进 stdin，JSON 决策出 stdout；before_tool 的 deny 拦截工具调用，其余事件只观察。
// 子进程失败 / 超时 / 非法输出均放行（钩子故障不阻塞主流程）。
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/repo"
)

// hookTimeoutBounds 钩子超时上下限（毫秒）。
const (
	hookTimeoutDefault = 10000
	hookTimeoutMax     = 60000
	hookTimeoutMin     = 1000
)

// UserHookService 钩子管理（设置页 CRUD + 运行时执行器一体）。
type UserHookService struct {
	repo *repo.UserHookRepo

	mu    sync.Mutex
	cache []domain.UserHookDO
	valid bool
}

// NewUserHookService 构造。
func NewUserHookService(repo *repo.UserHookRepo) *UserHookService {
	return &UserHookService{repo: repo}
}

// toHookRESP DO → RESP。
func toHookRESP(row *domain.UserHookDO) domain.UserHookRESP {
	return domain.UserHookRESP{
		ID: row.ID, Name: row.Name, Event: row.Event, Matcher: row.Matcher,
		Command: row.Command, TimeoutMs: row.TimeoutMs, Enabled: row.Enabled, Sort: row.Sort,
	}
}

// List 全量钩子。
func (s *UserHookService) List(ctx context.Context) ([]domain.UserHookRESP, error) {
	rows, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.UserHookRESP, 0, len(rows))
	for i := range rows {
		out = append(out, toHookRESP(&rows[i]))
	}
	return out, nil
}

// Upsert 创建/更新（校验事件与命令；timeout 收敛到合法区间）。
func (s *UserHookService) Upsert(ctx context.Context, req *domain.UserHookREQ) (*domain.UserHookRESP, error) {
	name := strings.TrimSpace(req.Name)
	event := strings.TrimSpace(req.Event)
	command := strings.TrimSpace(req.Command)
	if name == "" {
		return nil, pkg.New(domain.ErrHookInvalid.Code, "钩子名称不能为空", "")
	}
	if event != domain.HookEventRunStart && event != domain.HookEventBeforeTool &&
		event != domain.HookEventAfterTool && event != domain.HookEventRunEnd {
		return nil, pkg.New(domain.ErrHookInvalid.Code, "非法的钩子事件", event)
	}
	if command == "" {
		return nil, pkg.New(domain.ErrHookInvalid.Code, "钩子命令不能为空", "")
	}
	timeout := req.TimeoutMs
	if timeout == 0 {
		timeout = hookTimeoutDefault
	}
	if timeout < hookTimeoutMin {
		timeout = hookTimeoutMin
	}
	if timeout > hookTimeoutMax {
		timeout = hookTimeoutMax
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	sort := 0
	if req.Sort != nil {
		sort = *req.Sort
	}

	row := &domain.UserHookDO{
		ID: req.ID, Name: name, Event: event, Matcher: strings.TrimSpace(req.Matcher),
		Command: command, TimeoutMs: timeout, Enabled: enabled, Sort: sort,
	}
	if row.ID != "" {
		// 更新：目标必须存在，且保留创建时间
		existing, gerr := s.repo.GetByID(ctx, row.ID)
		if gerr != nil {
			return nil, gerr
		}
		row.CreatedAt = existing.CreatedAt
	} else {
		row.ID = pkg.NewID("HOOK")
	}
	if err := s.repo.Upsert(ctx, row); err != nil {
		return nil, err
	}
	s.invalidate()
	out := toHookRESP(row)
	return &out, nil
}

// Delete 删除钩子。
func (s *UserHookService) Delete(ctx context.Context, id string) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.invalidate()
	return nil
}

// Test 设置页试跑：以样例载荷执行一次，返回决策与耗时（不落库）。
func (s *UserHookService) Test(ctx context.Context, id string) (*domain.HookTestRESP, error) {
	row, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !row.Enabled {
		return nil, pkg.New(domain.ErrHookTestFailed.Code, "钩子未启用", row.Name)
	}
	start := time.Now()
	decision, err := s.runOne(ctx, row, domain.HookPayload{
		Event: row.Event, SessionID: "test", Tool: "exec", Params: json.RawMessage(`{"command":"echo hook-test"}`),
	})
	return &domain.HookTestRESP{
		Decision:   decision.Decision,
		Reason:     decision.Reason,
		Err:        errString(err),
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}

// ---- 运行时执行器 ----

// enabledByEvent 启用且事件匹配的钩子（按 sort 升序）。
func (s *UserHookService) enabledByEvent(ctx context.Context, event string) []domain.UserHookDO {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.valid {
		rows, err := s.repo.List(ctx)
		if err != nil {
			pkg.L.Warn("hook cache load failed", "err", err.Error())
			return nil
		}
		s.cache = rows
		s.valid = true
	}
	out := make([]domain.UserHookDO, 0, 4)
	for _, h := range s.cache {
		if h.Enabled && h.Event == event {
			out = append(out, h)
		}
	}
	return out
}

// invalidate 配置变更后失效缓存（下次触发重载）。
func (s *UserHookService) invalidate() {
	s.mu.Lock()
	s.valid = false
	s.mu.Unlock()
}

// hookMatches 工具名匹配：matcher 空 = 全部；逗号分隔多值精确匹配。
func hookMatches(matcher, toolName string) bool {
	m := strings.TrimSpace(matcher)
	if m == "" {
		return true
	}
	for _, name := range strings.Split(m, ",") {
		if strings.TrimSpace(name) == toolName {
			return true
		}
	}
	return false
}

// runOne 执行单个钩子子进程：stdin 载荷 → stdout 决策。
func (s *UserHookService) runOne(ctx context.Context, h *domain.UserHookDO, payload domain.HookPayload) (domain.HookDecision, error) {
	timeout := time.Duration(h.TimeoutMs) * time.Millisecond
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := newShellCmd(cctx, h.Command)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return domain.HookDecision{}, err
	}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stdout
	if err := cmd.Start(); err != nil {
		return domain.HookDecision{}, err
	}
	bs, _ := json.Marshal(payload)
	_, _ = stdin.Write(bs)
	_ = stdin.Close()

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	var errProc error
	select {
	case errProc = <-waitErr:
	case <-cctx.Done():
		errProc = cctx.Err()
	}
	decision := domain.HookDecision{}
	out := strings.TrimSpace(stdout.String())
	if out != "" {
		// 取最后一行非空 JSON（容忍前导日志）
		for i := len(out) - 1; i >= 0; i-- {
			if out[i] == '\n' {
				out = out[i+1:]
				break
			}
		}
		_ = json.Unmarshal([]byte(out), &decision)
	}
	if errProc != nil && out != "" {
		// 带上子进程输出：「exit status 1」本身不可诊断
		errProc = fmt.Errorf("%w: %s", errProc, clipRunes(out, 200))
	}
	return decision, errProc
}

// clipRunes 按字符截断（避免截断出半个中文字符）。
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// BeforeTool 工具执行前钩子（harness 层调用）：任一 deny 即拦截。
func (s *UserHookService) BeforeTool(ctx context.Context, sessionID, runID, toolName string, params json.RawMessage) (bool, string) {
	for _, h := range s.enabledByEvent(ctx, domain.HookEventBeforeTool) {
		if !hookMatches(h.Matcher, toolName) {
			continue
		}
		hh := h
		decision, err := s.runOne(ctx, &hh, domain.HookPayload{
			Event: domain.HookEventBeforeTool, SessionID: sessionID, RunID: runID,
			Tool: toolName, Params: params,
		})
		if err != nil {
			pkg.L.Warn("before_tool hook failed (allowed)", "hook", hh.Name, "err", err.Error())
			continue
		}
		if decision.Decision == "deny" {
			reason := decision.Reason
			if reason == "" {
				reason = "denied by user hook: " + hh.Name
			}
			pkg.L.Info("before_tool hook denied tool call", "hook", hh.Name, "tool", toolName)
			return false, reason
		}
	}
	return true, ""
}

// AfterTool 工具执行后钩子（观察不阻断，异步执行）。
func (s *UserHookService) AfterTool(sessionID, runID, toolName, outcome string) {
	s.fireAsync(domain.HookPayload{
		Event: domain.HookEventAfterTool, SessionID: sessionID, RunID: runID,
		Tool: toolName, Outcome: outcome,
	}, domain.HookEventAfterTool)
}

// RunStart run 起始钩子（观察不阻断，异步执行）。
func (s *UserHookService) RunStart(sessionID, runID string) {
	s.fireAsync(domain.HookPayload{
		Event: domain.HookEventRunStart, SessionID: sessionID, RunID: runID,
	}, domain.HookEventRunStart)
}

// RunEnd run 收尾钩子（观察不阻断，异步执行）。
func (s *UserHookService) RunEnd(sessionID, runID, reason string) {
	s.fireAsync(domain.HookPayload{
		Event: domain.HookEventRunEnd, SessionID: sessionID, RunID: runID, Reason: reason,
	}, domain.HookEventRunEnd)
}

// fireAsync 后台触发：逐个跑匹配钩子，失败仅记日志。
func (s *UserHookService) fireAsync(payload domain.HookPayload, event string) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	go func() {
		defer cancel()
		for _, h := range s.enabledByEvent(ctx, event) {
			if h.Event == domain.HookEventAfterTool && !hookMatches(h.Matcher, payload.Tool) {
				continue
			}
			hh := h
			if _, err := s.runOne(ctx, &hh, payload); err != nil {
				pkg.L.Warn("hook fired with error", "hook", hh.Name, "event", event, "err", err.Error())
			}
		}
	}()
}
