// Package cron 提供定时任务调度：robfig/cron 表达式调度 + 动作 handler + 持久化。
// 不依赖 Wails runtime；动作处理器由装配方注册。
package cron

import (
	"context"
	"encoding/json"
	"log/slog"
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
	cron     *cronlib.Cron
	repo     *repo.CronJobRepo
	handlers map[domain.CronJobAction]ActionHandler
	entries  map[string]cronlib.EntryID
	mu       sync.Mutex
}

// NewScheduler 构造；动作 handler 经 RegisterHandler 注入。
func NewScheduler(r *repo.CronJobRepo) *Scheduler {
	return &Scheduler{
		cron:     cronlib.New(),
		repo:     r,
		handlers: make(map[domain.CronJobAction]ActionHandler),
		entries:  make(map[string]cronlib.EntryID),
	}
}

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
			slog.Warn("cron schedule failed", "id", jobs[i].ID, "err", err)
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

// execute 执行一次任务并记录结果（lastRunAt/lastStatus）。
func (s *Scheduler) execute(ctx context.Context, job *domain.CronJobDO) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	s.mu.Lock()
	h := s.handlers[job.Action]
	s.mu.Unlock()
	now := time.Now().UnixMilli()
	status := "success"
	if h == nil {
		status = "failed"
		slog.Warn("cron unknown action", "id", job.ID, "action", job.Action)
	} else if err := h(ctx, json.RawMessage(job.ActionArgs)); err != nil {
		status = "failed"
		slog.Warn("cron run failed", "id", job.ID, "err", err)
	}
	_ = s.repo.UpdateRunState(ctx, job.ID, now, status)
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
