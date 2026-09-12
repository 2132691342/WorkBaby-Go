package service

import (
	"context"
	"sort"
	"sync"
	"time"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/event"
	"WorkBaby/internal/pkg"
)

// 编译期断言：ChatService 满足后台任务的执行入口。
var _ agentRunner = (*ChatService)(nil)

// 后台任务队列默认值：单用户桌面端，并发克制优先。
const (
	taskWorkers    = 2
	taskQueueSize  = 32
	taskKeepRecent = 50 // 内存保留的最近任务数
	taskResultMax  = 2000
)

// agentRunner 后台任务的 Agent 执行入口（ChatService 实现）。
//
// 接口化让任务队列不依赖 ChatService 的具体构造（repo/registry/tool 一整套），
// 单测可直接注入假实现验证排队与取消语义。
type agentRunner interface {
	RunAgent(ctx context.Context, sessionID, userInput, agentName string) (*AgentRunOutcome, error)
}

// TaskService 后台任务队列：有界 worker pool + 优先级无关的 FIFO + context 取消传播。
//
// 长任务（调研、批量处理）提交后不阻塞聊天：任务中心看状态，结果落成会话消息。
// v1 单优先级：本地单用户场景，优先级队列是过早设计。
type TaskService struct {
	runner agentRunner
	bus    *event.Bus
	events *event.RunEventLog

	mu      sync.Mutex
	tasks   map[string]*domain.TaskDO
	cancels map[string]context.CancelFunc
	queue   chan *domain.TaskDO
	stopped bool
}

// NewTaskService 构造任务服务（未启动 worker）。
func NewTaskService(runner agentRunner, bus *event.Bus) *TaskService {
	return &TaskService{
		runner:  runner,
		bus:     bus,
		tasks:   map[string]*domain.TaskDO{},
		cancels: map[string]context.CancelFunc{},
		queue:   make(chan *domain.TaskDO, taskQueueSize),
	}
}

// WithEventLog 启用 run 事件日志：任务事件与 chat 事件共享序号空间。
func (s *TaskService) WithEventLog(log *event.RunEventLog) *TaskService {
	s.events = log
	return s
}

// Start 启动 worker pool；重复调用只启动一次。
func (s *TaskService) Start() {
	for i := 0; i < taskWorkers; i++ {
		go s.loop()
	}
}

// Stop 停止 worker（关闭队列通道；在跑的任务靠其自身 ctx 结束）。
func (s *TaskService) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	s.mu.Unlock()
	close(s.queue)
}

// Submit 提交任务；队列满返回 5009（不阻塞调用方，前端可提示稍后重试）。
func (s *TaskService) Submit(req domain.TaskSubmitREQ) (*domain.TaskDO, error) {
	if req.SessionID == "" || req.Prompt == "" {
		return nil, domain.ErrTaskInvalid
	}
	if s.runner == nil {
		return nil, domain.ErrTaskNoDelegator
	}
	if req.Agent == "" {
		req.Agent = defaultAgentName
	}
	now := time.Now().UnixMilli()
	t := &domain.TaskDO{
		ID:        pkg.NewID("TASK"),
		SessionID: req.SessionID,
		Agent:     req.Agent,
		Prompt:    req.Prompt,
		State:     domain.TaskPending,
		CreatedAt: now,
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil, domain.ErrTaskNoDelegator
	}
	s.tasks[t.ID] = t
	s.pruneLocked()
	queued := len(s.queue)
	s.mu.Unlock()
	t.QueueLength = queued

	select {
	case s.queue <- t:
	default:
		s.mu.Lock()
		delete(s.tasks, t.ID)
		s.mu.Unlock()
		return nil, domain.ErrTaskQueueFull
	}
	s.emit(t, "task:created")
	return t, nil
}

// List 任务列表（最新在前）；sessionID 为空返回全部。
func (s *TaskService) List(sessionID string, limit int) domain.TaskListRESP {
	if limit <= 0 {
		limit = taskKeepRecent
	}
	s.mu.Lock()
	rows := make([]*domain.TaskDO, 0, len(s.tasks))
	for _, t := range s.tasks {
		if sessionID == "" || t.SessionID == sessionID {
			rows = append(rows, t)
		}
	}
	s.mu.Unlock()
	sort.Slice(rows, func(i, j int) bool { return rows[i].CreatedAt > rows[j].CreatedAt })
	if len(rows) > limit {
		rows = rows[:limit]
	}
	out := domain.TaskListRESP{Items: make([]domain.TaskRESP, 0, len(rows)), Total: len(rows)}
	for _, t := range rows {
		out.Items = append(out.Items, toTaskRESP(t))
	}
	return out
}

// Get 单条任务。
func (s *TaskService) Get(id string) (domain.TaskRESP, error) {
	s.mu.Lock()
	t, ok := s.tasks[id]
	s.mu.Unlock()
	if !ok {
		return domain.TaskRESP{}, domain.ErrTaskNotFound
	}
	return toTaskRESP(t), nil
}

// Cancel 取消排队或运行中的任务；已终态返回 5011。
func (s *TaskService) Cancel(id string) error {
	s.mu.Lock()
	t, ok := s.tasks[id]
	if !ok {
		s.mu.Unlock()
		return domain.ErrTaskNotFound
	}
	if t.State == domain.TaskCompleted || t.State == domain.TaskFailed || t.State == domain.TaskCancelled {
		s.mu.Unlock()
		return domain.ErrTaskNotRunning
	}
	cancel, has := s.cancels[id]
	s.mu.Unlock()
	if has {
		cancel() // 运行中的任务：取消沿 ctx 传播到 harness run
		return nil
	}
	// 排队中：标记取消，worker 取到时跳过
	s.mu.Lock()
	t.State = domain.TaskCancelled
	t.FinishedAt = time.Now().UnixMilli()
	s.mu.Unlock()
	s.emit(t, "task:done")
	return nil
}

// loop worker 主循环：取任务 → 跑 Agent → 落终态。
func (s *TaskService) loop() {
	for t := range s.queue {
		s.runOne(t)
	}
}

// runOne 执行单个任务（阻塞）；已取消的排队任务直接跳过。
func (s *TaskService) runOne(t *domain.TaskDO) {
	s.mu.Lock()
	if t.State == domain.TaskCancelled {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancels[t.ID] = cancel
	t.State = domain.TaskRunning
	t.StartedAt = time.Now().UnixMilli()
	s.mu.Unlock()
	s.emit(t, "task:started")

	out, err := s.runner.RunAgent(ctx, t.SessionID, t.Prompt, t.Agent)

	s.mu.Lock()
	delete(s.cancels, t.ID)
	t.FinishedAt = time.Now().UnixMilli()
	switch {
	case err != nil:
		t.State = domain.TaskFailed
		t.Error = err.Error()
	case ctx.Err() != nil:
		t.State = domain.TaskCancelled
	default:
		t.State = domain.TaskCompleted
		if out != nil {
			t.RunID = out.RunID
			t.Result = pkg.TruncateRunes(out.Content, taskResultMax)
			if out.Err != nil {
				t.Error = out.Err.Error()
			}
		}
	}
	s.mu.Unlock()
	cancel()
	s.emit(t, "task:done")
}

// pruneLocked 保留最近 taskKeepRecent 条已终态任务，避免无限增长。
func (s *TaskService) pruneLocked() {
	done := make([]*domain.TaskDO, 0, len(s.tasks))
	for _, t := range s.tasks {
		if t.State == domain.TaskCompleted || t.State == domain.TaskFailed || t.State == domain.TaskCancelled {
			done = append(done, t)
		}
	}
	if len(done) <= taskKeepRecent {
		return
	}
	sort.Slice(done, func(i, j int) bool { return done[i].CreatedAt < done[j].CreatedAt })
	for _, t := range done[:len(done)-taskKeepRecent] {
		delete(s.tasks, t.ID)
	}
}

// emit 发布任务事件（不在 run 序号空间内，仅广播给前端任务中心）。
func (s *TaskService) emit(t *domain.TaskDO, name string) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(name, map[string]any{"task": toTaskRESP(t)})
}

// toTaskRESP DO → 出参。
func toTaskRESP(t *domain.TaskDO) domain.TaskRESP {
	return domain.TaskRESP{
		ID:         t.ID,
		SessionID:  t.SessionID,
		Agent:      t.Agent,
		Prompt:     t.Prompt,
		State:      t.State,
		RunID:      t.RunID,
		Result:     t.Result,
		Error:      t.Error,
		CreatedAt:  t.CreatedAt,
		StartedAt:  t.StartedAt,
		FinishedAt: t.FinishedAt,
	}
}
