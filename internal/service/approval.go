package service

import (
	"context"
	"sync"
	"time"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/event"
	"WorkBaby/internal/harness"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/repo"
	"WorkBaby/internal/tool"
)

// 审批等待用户决策的超时；超时视为拒绝（LLM 收到拒绝结果会改道，run 不会永久挂起）。
const approvalWaitTimeout = 2 * time.Minute

// inputReason 补充输入请求的统一说明文案。
const inputReason = "我需要你补充一点信息才能继续"

// ApprovalService 工具审批门：
//   - Approve：发布 chat:approval 事件给前端 → 阻塞等待 Decide 回填；
//   - needs_approval 类命令批准过一次后本进程内免审（单用户桌面应用，无会话隔离诉求）；
//   - irreversible（危险正则命中）每次都问，永不免审；
//   - 决策路径（Approve/Deny/超时/取消）通过 chat:approval-decided 事件回传，前端可据此清理；
//   - Pending() 暴露未决审批（含 run/session/过期时间），前端刷新页面后可重拉恢复；
//   - 持久化：未决请求落 approval_records，进程重启后 DrainStale 排空；
//   - RequestInput：模型主动向用户要信息，复用同一暂停通道。
type ApprovalService struct {
	bus      *event.Bus
	events   *event.RunEventLog // run 事件日志（与 ChatService 共享，保证 seq 连续）
	records  *repo.ApprovalRecordRepo
	mu       sync.Mutex
	pending  map[string]*pendingApproval
	inputs   map[string]chan inputAnswer
	inflight map[string]*pendingInput
	approved map[string]bool // 已批准的命令（needs_approval 免审表；进程内存，重启清空更安全）
}

// pendingApproval 单条未决审批。
type pendingApproval struct {
	ch        chan bool
	command   string
	risk      string
	runID     string
	sessionID string
	createdAt int64
	expiresAt int64
}

// pendingInput 单条未决补充输入（Pending 快照用）。
type pendingInput struct {
	question  string
	runID     string
	sessionID string
	expiresAt int64
}

// inputAnswer 补充输入的回填值；ok=false 表示用户跳过。
type inputAnswer struct {
	text string
	ok   bool
}

// NewApprovalService 构造审批服务。
func NewApprovalService(bus *event.Bus) *ApprovalService {
	return &ApprovalService{
		bus:      bus,
		pending:  map[string]*pendingApproval{},
		inputs:   map[string]chan inputAnswer{},
		inflight: map[string]*pendingInput{},
		approved: map[string]bool{},
	}
}

// WithEventLog 启用 run 事件日志：审批事件与 chat 事件共享同一序号空间，断线重放不缺帧。
func (s *ApprovalService) WithEventLog(log *event.RunEventLog) *ApprovalService {
	s.events = log
	return s
}

// WithRecords 启用审批持久化（暂停态跨重启可见/可排空）。
func (s *ApprovalService) WithRecords(r *repo.ApprovalRecordRepo) *ApprovalService {
	s.records = r
	return s
}

// emit 发布审批事件：注入 run_id/session_id、经事件日志分配 seq，再广播到总线。
// ctx 取消后可能读不到 runID（回退空串），此时事件不入重放缓冲，仅广播。
func (s *ApprovalService) emit(ctx context.Context, name string, payload map[string]any) {
	runID := harness.RunIDFromCtx(ctx)
	if payload == nil {
		payload = map[string]any{}
	}
	payload["run_id"] = runID
	payload["session_id"] = harness.SessionIDFromCtx(ctx)
	if s.events != nil {
		s.events.Append(runID, name, payload)
	}
	s.bus.Publish(name, payload)
}

// Approve 实现 tool.Approver：阻塞等待用户决策（批准 true / 拒绝、超时、取消 false）。
func (s *ApprovalService) Approve(ctx context.Context, command string, risk string) bool {
	if risk == tool.RiskApprovalNeeds {
		s.mu.Lock()
		ok := s.approved[command]
		s.mu.Unlock()
		if ok {
			return true // 免审：同命令已批准过
		}
	}

	id := pkg.NewID("APR")
	now := time.Now()
	item := &pendingApproval{
		ch:        make(chan bool, 1),
		command:   command,
		risk:      risk,
		runID:     harness.RunIDFromCtx(ctx),
		sessionID: harness.SessionIDFromCtx(ctx),
		createdAt: now.UnixMilli(),
		expiresAt: now.Add(approvalWaitTimeout).UnixMilli(),
	}
	s.mu.Lock()
	s.pending[id] = item
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
	}()
	s.persistPending(ctx, id, domain.ApprovalKindApproval, command, risk, item.expiresAt)

	s.emit(ctx, "chat:approval", map[string]any{
		"id":      id,
		"command": command,
		"reason":  approvalReason(risk),
		"risk":    risk,
	})

	select {
	case ok := <-item.ch:
		if ok && risk == tool.RiskApprovalNeeds {
			s.mu.Lock()
			s.approved[command] = true
			s.mu.Unlock()
		}
		s.settleRecord(id, map[bool]string{true: domain.ApprovalStatusApproved, false: domain.ApprovalStatusDenied}[ok], "")
		s.emitDecided(ctx, id, command, map[bool]string{true: "approved", false: "denied"}[ok])
		return ok
	case <-time.After(approvalWaitTimeout):
		pkg.L.Warn("approval timed out, deny", "id", id)
		s.settleRecord(id, domain.ApprovalStatusTimeout, "")
		s.emitDecided(ctx, id, command, "timeout")
		return false
	case <-ctx.Done():
		s.settleRecord(id, domain.ApprovalStatusCancelled, "")
		s.emitDecided(ctx, id, command, "cancelled")
		return false // run 被取消/超时，工具栈随之终止
	}
}

// persistPending 未决请求落库（records 未注入时静默跳过）。
func (s *ApprovalService) persistPending(ctx context.Context, id, kind, command, risk string, expiresAt int64) {
	if s.records == nil {
		return
	}
	row := &domain.ApprovalRecordDO{
		ID: id, RunID: harness.RunIDFromCtx(ctx), SessionID: harness.SessionIDFromCtx(ctx),
		Kind: kind, Command: command, Risk: risk, Status: domain.ApprovalStatusPending, ExpiresAt: expiresAt,
	}
	if err := s.records.Create(ctx, row); err != nil {
		pkg.L.Warn("persist pending approval failed", "id", id, "err", err.Error())
	}
}

// settleRecord 终态更新（忽略未启用持久化）。
func (s *ApprovalService) settleRecord(id, status, answer string) {
	if s.records == nil {
		return
	}
	if err := s.records.SetStatus(context.Background(), id, status, answer); err != nil {
		pkg.L.Warn("settle approval record failed", "id", id, "err", err.Error())
	}
}

// RequestInput 模型主动向用户要信息（risk=input_required）：阻塞等待 Answer 回填文本。
// 返回 (回答, true)；超时/取消返回 ("", false)。
func (s *ApprovalService) RequestInput(ctx context.Context, question string) (string, bool) {
	id := pkg.NewID("INP")
	now := time.Now()
	ch := make(chan inputAnswer, 1)
	s.mu.Lock()
	s.inputs[id] = ch
	s.inflight[id] = &pendingInput{
		question:  question,
		runID:     harness.RunIDFromCtx(ctx),
		sessionID: harness.SessionIDFromCtx(ctx),
		expiresAt: now.Add(approvalWaitTimeout).UnixMilli(),
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.inputs, id)
		delete(s.inflight, id)
		s.mu.Unlock()
	}()
	s.persistPending(ctx, id, domain.ApprovalKindInput, question, "input_required", now.Add(approvalWaitTimeout).UnixMilli())

	s.emit(ctx, "chat:approval", map[string]any{
		"id":      id,
		"command": question,
		"reason":  inputReason,
		"risk":    tool.RiskApprovalInput,
	})

	select {
	case ans := <-ch:
		if !ans.ok {
			s.settleRecord(id, domain.ApprovalStatusSkipped, "")
			s.emitDecided(ctx, id, question, "skipped")
			return "", false
		}
		s.settleRecord(id, domain.ApprovalStatusAnswered, ans.text)
		s.emitDecided(ctx, id, question, "answered")
		return ans.text, true
	case <-time.After(approvalWaitTimeout):
		s.settleRecord(id, domain.ApprovalStatusTimeout, "")
		s.emitDecided(ctx, id, question, "timeout")
		return "", false
	case <-ctx.Done():
		s.settleRecord(id, domain.ApprovalStatusCancelled, "")
		s.emitDecided(ctx, id, question, "cancelled")
		return "", false
	}
}

// Answer 用户回复补充输入（api 层绑定）。
func (s *ApprovalService) Answer(id, text string) error {
	s.mu.Lock()
	ch, ok := s.inputs[id]
	if ok {
		delete(s.inputs, id)
		delete(s.inflight, id)
	}
	s.mu.Unlock()
	if !ok {
		return pkg.New(4003, "补充输入请求不存在或已过期", id)
	}
	ch <- inputAnswer{text: text, ok: true}
	return nil
}

// DrainStale 启动排空：重启后无等待 goroutine 的旧 pending 一律标 cancelled
// （run 已随进程死亡；用户可经 Resume 端点续跑，续跑后审批会重新发起）。
func (s *ApprovalService) DrainStale(ctx context.Context) {
	if s.records == nil {
		return
	}
	rows, err := s.records.ListPending(ctx)
	if err != nil {
		pkg.L.Warn("drain stale approvals list failed", "err", err.Error())
		return
	}
	for i := range rows {
		if err := s.records.SetStatus(ctx, rows[i].ID, domain.ApprovalStatusCancelled, ""); err != nil {
			pkg.L.Warn("drain stale approval failed", "id", rows[i].ID, "err", err.Error())
		}
	}
	if len(rows) > 0 {
		pkg.L.Info("drained stale approvals", "count", len(rows))
	}
}

// Decide 用户决策回填（api 层 DecideApproval 绑定调用）。
func (s *ApprovalService) Decide(id string, approved bool) error {
	s.mu.Lock()
	item, ok := s.pending[id]
	if ok {
		delete(s.pending, id)
	}
	s.mu.Unlock()
	if !ok {
		if _, isInput := s.inputs[id]; isInput {
			return pkg.New(4003, "这是补充输入请求，请用 skip 或 answer 处理", id)
		}
		return pkg.New(4003, "审批请求不存在或已过期", id)
	}
	item.ch <- approved
	return nil
}

// Skip 用户跳过：审批按拒绝处理，补充输入按「未回复」处理（模型据此自行假设并继续）。
// 两类请求共用同一入口，前端无需按 risk 分流打不同端点——分流正是 4003 的根因。
func (s *ApprovalService) Skip(id string) error {
	s.mu.Lock()
	item, isApproval := s.pending[id]
	if isApproval {
		delete(s.pending, id)
	}
	ch, isInput := s.inputs[id]
	if isInput {
		delete(s.inputs, id)
		delete(s.inflight, id)
	}
	s.mu.Unlock()
	switch {
	case isApproval:
		item.ch <- false
		return nil
	case isInput:
		ch <- inputAnswer{ok: false}
		return nil
	default:
		return pkg.New(4003, "审批请求不存在或已过期", id)
	}
}

// Pending 返回未决审批快照（供前端刷新后恢复； 审批恢复）。
// 顺带清理已过期的审批（Approve 侧超时路径会拒绝，这里只做视图裁剪）。
func (s *ApprovalService) Pending() []domain.ApprovalPendingRESP {
	now := time.Now().UnixMilli()
	s.mu.Lock()
	out := make([]domain.ApprovalPendingRESP, 0, len(s.pending)+len(s.inflight))
	for id, it := range s.pending {
		if now >= it.expiresAt {
			delete(s.pending, id)
			continue
		}
		out = append(out, domain.ApprovalPendingRESP{
			ID:        id,
			RunID:     it.runID,
			SessionID: it.sessionID,
			Command:   it.command,
			Reason:    approvalReason(it.risk),
			Risk:      it.risk,
			ExpiresAt: it.expiresAt,
		})
	}
	// 补充输入与审批同属「暂停等用户」，必须一并回传，否则刷新后提问卡片永久丢失
	for id, it := range s.inflight {
		if now >= it.expiresAt {
			delete(s.inputs, id)
			delete(s.inflight, id)
			continue
		}
		out = append(out, domain.ApprovalPendingRESP{
			ID:        id,
			RunID:     it.runID,
			SessionID: it.sessionID,
			Command:   it.question,
			Reason:    inputReason,
			Risk:      tool.RiskApprovalInput,
			ExpiresAt: it.expiresAt,
		})
	}
	s.mu.Unlock()
	return out
}

// emitDecided 发反向事件，让前端清 pendingApproval。
// ctx 可能已取消（Approve 的 ctx.Done 分支），从 ctx 读不到 runID 时回退为空字符串。
func (s *ApprovalService) emitDecided(ctx context.Context, id, command, decision string) {
	s.emit(ctx, "chat:approval-decided", map[string]any{
		"id":       id,
		"command":  command,
		"decision": decision,
	})
}

func approvalReason(risk string) string {
	if risk == tool.RiskApprovalIrrev {
		return "命令命中危险操作模式（不可逆），需要你确认"
	}
	return "命令不在白名单内，需要你确认后执行"
}
