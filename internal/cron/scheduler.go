// Package cron 提供定时任务调度：robfig/cron 表达式调度 + 动作 handler + 持久化。
// 不依赖 Wails runtime；动作处理器由装配方注册。
package cron

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/repo"
	cronlib "github.com/robfig/cron/v3"
)

// ActionHandler 执行一次 cron 动作（args 为 actionArgs JSON）。
type ActionHandler func(ctx context.Context, args json.RawMessage) error

// Scheduler 定时任务调度器：启动加载启用任务 + 增删改热更新 + 手动触发。
type Scheduler struct {
	cron      *cronlib.Cron
	repo      *repo.CronJobRepo
	logs      *repo.CronJobRunLogRepo
	keepAwake KeepAwaker
	handlers  map[domain.CronJobAction]ActionHandler
	entries   map[string]cronlib.EntryID
	mu        sync.Mutex
}

// NewScheduler 构造；动作 handler 经 RegisterHandler 注入。logs 可为 nil（不写执行日志）。
func NewScheduler(r *repo.CronJobRepo, logs *repo.CronJobRunLogRepo) *Scheduler {
	return &Scheduler{
		cron:     cronlib.New(),
		repo:     r,
		logs:     logs,
		handlers: make(map[domain.CronJobAction]ActionHandler),
		entries:  make(map[string]cronlib.EntryID),
	}
}

// KeepAwaker 无人值守防系统空闲休眠（cron/workflow 长任务运行中置 true，结束归位）。
// 由装配方注入：nil 表示禁用（避免单测与不需要该能力的部署）。
type KeepAwaker interface {
	Acquire(ctx context.Context, reason string) (release func(), err error)
}

// WithKeepAwaker 注入保持唤醒策略（nil = 禁用）。
func (s *Scheduler) WithKeepAwaker(k KeepAwaker) *Scheduler { s.keepAwake = k; return s }

// RegisterHandler 注册动作处理器（装配方注入 run_workflow → workflow service）。
func (s *Scheduler) RegisterHandler(action domain.CronJobAction, h ActionHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[action] = h
}

// Start 加载启用任务并启动调度；单个任务失败不阻断。
func (s *Scheduler) Start(ctx context.Context) error {
	jobs, err := s.repo.ListEnabled(ctx)
	if err != nil {
		return err
	}
	for i := range jobs {
		if err := s.schedule(ctx, &jobs[i]); err != nil {
			pkg.L.Warn("cron schedule failed", "id", jobs[i].ID, "err", err)
		}
	}
	s.cron.Start()
	return nil
}

// Stop 停止调度器（保留任务与 entries 状态）。
func (s *Scheduler) Stop() { s.cron.Stop() }

// List 全部任务。
func (s *Scheduler) List(ctx context.Context) ([]domain.CronJobRESP, error) {
	rows, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.CronJobRESP, 0, len(rows))
	for i := range rows {
		out = append(out, toRESP(&rows[i]))
	}
	return out, nil
}

// Create 新建任务并调度（表达式无效拒绝落库）。
func (s *Scheduler) Create(ctx context.Context, req domain.CronJobREQ) (domain.CronJobRESP, error) {
	if req.Name == "" || req.Schedule == "" {
		return domain.CronJobRESP{}, pkg.New(9201, "name and schedule required", "")
	}
	if _, err := cronlib.ParseStandard(req.Schedule); err != nil {
		return domain.CronJobRESP{}, pkg.Wrap(9201, "cron expression invalid", err)
	}
	if req.WorkflowID == "" {
		return domain.CronJobRESP{}, pkg.New(9202, "workflowId required for run_workflow action", "")
	}
	row := &domain.CronJobDO{
		Name:       req.Name,
		Spec:       req.Schedule,
		Action:     domain.ActionRunWorkflow,
		ActionArgs: marshalArgs(map[string]any{"workflowID": req.WorkflowID}),
		Enabled:    enabledOf(req.Enabled),
	}
	if err := s.repo.Create(ctx, row); err != nil {
		return domain.CronJobRESP{}, err
	}
	if row.Enabled {
		if err := s.schedule(ctx, row); err != nil {
			return domain.CronJobRESP{}, err
		}
	}
	return toRESP(row), nil
}

// Update 更新任务并热更新调度（表达式无效拒绝）。
func (s *Scheduler) Update(ctx context.Context, id string, req domain.CronJobREQ) (domain.CronJobRESP, error) {
	row, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return domain.CronJobRESP{}, err
	}
	if req.Name != "" {
		row.Name = req.Name
	}
	if req.Schedule != "" {
		if _, err := cronlib.ParseStandard(req.Schedule); err != nil {
			return domain.CronJobRESP{}, pkg.Wrap(9201, "cron expression invalid", err)
		}
		row.Spec = req.Schedule
	}
	if req.WorkflowID != "" {
		row.ActionArgs = marshalArgs(map[string]any{"workflowID": req.WorkflowID})
	}
	if req.Enabled != nil {
		row.Enabled = *req.Enabled
	}
	if err := s.repo.Update(ctx, row); err != nil {
		return domain.CronJobRESP{}, err
	}
	if row.Enabled {
		if err := s.schedule(ctx, row); err != nil {
			return domain.CronJobRESP{}, err
		}
	} else {
		s.remove(row.ID)
	}
	return toRESP(row), nil
}

// Delete 删除任务并移除调度。
func (s *Scheduler) Delete(ctx context.Context, id string) error {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.remove(id)
	return nil
}

// Trigger 立即执行一次（异步；状态经 lastRunAt/lastStatus 回写）。
func (s *Scheduler) Trigger(ctx context.Context, id string) error {
	row, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	go s.execute(context.Background(), row)
	return nil
}

func (s *Scheduler) schedule(ctx context.Context, job *domain.CronJobDO) error {
	if entryID, ok := s.entries[job.ID]; ok {
		s.cron.Remove(entryID)
	}
	snap := *job // 闭包捕获快照，避免后续更新影响执行参数
	entryID, err := s.cron.AddFunc(job.Spec, func() {
		s.execute(context.Background(), &snap)
	})
	if err != nil {
		return pkg.Wrap(9201, "cron expression invalid", err)
	}
	s.entries[job.ID] = entryID
	// robfig Entry.Next 由 run goroutine 懒计算（未 Start 时为零值）；此处直接用 Schedule 计算
	if sch, err := cronlib.ParseStandard(job.Spec); err == nil {
		if next := sch.Next(time.Now()); !next.IsZero() {
			_ = s.repo.UpdateNextRunAt(ctx, job.ID, next.UnixMilli())
		}
	}
	return nil
}

func (s *Scheduler) remove(jobID string) {
	if entryID, ok := s.entries[jobID]; ok {
		s.cron.Remove(entryID)
		delete(s.entries, jobID)
	}
}

// execute 执行一次任务并记录结果（lastRunAt/lastStatus）；失败按 MaxRetries 指数退避重试。
//
// runID 是本 run 共享的唯一标识（首次 attempt 创建一条 log，后续 attempt 各自一条）；
// runCount 与 failCount 在本 run 失败时累加。
func (s *Scheduler) execute(ctx context.Context, job *domain.CronJobDO) {
	var releaseKeepAwake func()
	if s.keepAwake != nil {
		// 任务运行全程防止系统休眠；release 必须在 defer 链首部。
		if r, err := s.keepAwake.Acquire(ctx, "cron:"+job.ID); err == nil {
			releaseKeepAwake = r
			defer func() { releaseKeepAwake() }()
		} else {
			pkg.L.Warn("cron keep-awake acquire failed", "jobID", job.ID, "err", err.Error())
		}
	}
	s.mu.Lock()
	h := s.handlers[job.Action]
	s.mu.Unlock()
	maxAttempts := job.MaxRetries + 1
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	delayBase := job.RetryDelaySec
	if delayBase <= 0 {
		delayBase = 30
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		logRow := &domain.CronJobRunLogDO{
			ID: pkg.NewID("CLK"), JobID: job.ID, Attempt: attempt,
			Status: "running", StartedAt: time.Now().UnixMilli(),
		}
		if s.logs != nil {
			if err := s.logs.Create(ctx, logRow); err != nil {
				pkg.L.Warn("cron create run log failed", "jobID", job.ID, "err", err.Error())
			}
		}
		attemptCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		attemptStart := time.Now()
		err := func() error {
			defer cancel()
			if h == nil {
				return pkg.New(9202, "cron action handler not registered", string(job.Action))
			}
			return h(attemptCtx, json.RawMessage(job.ActionArgs))
		}()
		durMs := time.Since(attemptStart).Milliseconds()
		finishAt := time.Now().UnixMilli()
		status := "success"
		errMsg := ""
		if err != nil {
			status = "failed"
			errMsg = truncateErr(err.Error(), 2000)
			lastErr = err
		}
		if s.logs != nil {
			if uerr := s.logs.Update(ctx, logRow.ID, status, finishAt, durMs, errMsg); uerr != nil {
				pkg.L.Warn("cron update run log failed", "jobID", job.ID, "err", uerr.Error())
			}
		}
		if err == nil {
			_ = s.repo.UpdateRunState(ctx, job.ID, finishAt, "success")
			_ = s.repo.IncrementRunCount(ctx, job.ID, false)
			return
		}
		if attempt < maxAttempts {
			delay := time.Duration(delayBase) * time.Second * (1 << (attempt - 1)) // 30s, 60s, 120s, ...
			if delay > 10*time.Minute {
				delay = 10 * time.Minute
			}
			pkg.L.Warn("cron attempt failed, will retry",
				"jobID", job.ID, "attempt", attempt, "nextDelay", delay.String(), "err", err.Error())
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		}
	}
	// 所有尝试都失败
	_ = s.repo.UpdateRunState(ctx, job.ID, time.Now().UnixMilli(), "failed")
	_ = s.repo.IncrementRunCount(ctx, job.ID, true)
	lastErrMsg := ""; if lastErr != nil { lastErrMsg = truncateErr(lastErr.Error(), 2000) }; pkg.L.Warn("cron run exhausted retries", "jobID", job.ID, "maxAttempts", maxAttempts, "lastErr", lastErrMsg)
}

// toRESP 映射；run_workflow 的 workflowId 从 actionArgs 提取。
func toRESP(d *domain.CronJobDO) domain.CronJobRESP {
	resp := domain.CronJobRESP{
		ID: d.ID, Name: d.Name, Schedule: d.Spec,
		Enabled: d.Enabled, LastRunAt: d.LastRunAt, LastStatus: d.LastStatus,
		NextRunAt: d.NextRunAt, Action: d.Action,
		CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
	if d.Action == domain.ActionRunWorkflow && d.ActionArgs != "" {
		var m map[string]string
		if json.Unmarshal([]byte(d.ActionArgs), &m) == nil {
			resp.WorkflowID = m["workflowID"]
		}
	}
	return resp
}

func marshalArgs(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func enabledOf(v *bool) bool {
	if v == nil {
		return true
	}
	return *v
}

// truncateErr 错误文案截断（避免日志/DB 字段撑爆）。
func truncateErr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// DeleteByJob 级联清理任务日志（调用方在删 job 前先调用）。
func (s *Scheduler) DeleteByJob(ctx context.Context, jobID string) {
	if s.logs != nil {
		if err := s.logs.DeleteByJob(ctx, jobID); err != nil {
			pkg.L.Warn("delete cron run logs failed", "jobID", jobID, "err", err.Error())
		}
	}
}

// ListRunLogs 任务执行历史（前端任务中心/审计面板）。
func (s *Scheduler) ListRunLogs(ctx context.Context, jobID string, limit int) ([]domain.CronJobRunLogRESP, error) {
	if s.logs == nil {
		return []domain.CronJobRunLogRESP{}, nil
	}
	rows, err := s.logs.ListByJob(ctx, jobID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]domain.CronJobRunLogRESP, 0, len(rows))
	for i := range rows {
		out = append(out, domain.CronJobRunLogRESP{
			ID: rows[i].ID, JobID: rows[i].JobID, Attempt: rows[i].Attempt,
			Status: rows[i].Status, StartedAt: rows[i].StartedAt,
			FinishedAt: rows[i].FinishedAt, DurationMs: rows[i].DurationMs,
			Error: rows[i].Error,
		})
	}
	return out, nil
}
