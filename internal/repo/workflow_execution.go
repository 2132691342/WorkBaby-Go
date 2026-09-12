package repo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"gorm.io/gorm"
)

// WorkflowExecutionRepo workflow_executions 表 CRUD。
type WorkflowExecutionRepo struct{ db *gorm.DB }

// NewWorkflowExecutionRepo 构造仓储。
func NewWorkflowExecutionRepo(db *gorm.DB) *WorkflowExecutionRepo {
	return &WorkflowExecutionRepo{db: db}
}

// Create 新建执行记录（status=running、startedAt=now）。
func (r *WorkflowExecutionRepo) Create(ctx context.Context, row *domain.WorkflowExecutionDO) error {
	if row.ID == "" {
		row.ID = pkg.NewID(domain.IDWorkflowExec)
	}
	if row.StartedAt == 0 {
		row.StartedAt = time.Now().UnixMilli()
	}
	if row.Status == "" {
		row.Status = domain.WorkflowStatusRunning
	}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return pkg.Wrap(2093, "create workflow execution failed", err)
	}
	return nil
}

// GetByID 查找。
func (r *WorkflowExecutionRepo) GetByID(ctx context.Context, id string) (*domain.WorkflowExecutionDO, error) {
	var row domain.WorkflowExecutionDO
	if err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrWorkflowNotFound
		}
		return nil, pkg.Wrap(2093, "get workflow execution failed", err)
	}
	return &row, nil
}

// ListByWorkflow 按 workflowId 全量倒序。
func (r *WorkflowExecutionRepo) ListByWorkflow(ctx context.Context, workflowID string, limit int) ([]domain.WorkflowExecutionDO, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows []domain.WorkflowExecutionDO
	if err := r.db.WithContext(ctx).
		Where("workflow_id = ?", workflowID).
		Order("started_at DESC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, pkg.Wrap(2093, "list workflow executions failed", err)
	}
	return rows, nil
}

// UpdateStatus 非终态状态写入（running/paused 等，不落 finished_at）。
func (r *WorkflowExecutionRepo) UpdateStatus(ctx context.Context, id string, status domain.WorkflowExecutionStatus) error {
	if err := r.db.WithContext(ctx).
		Model(&domain.WorkflowExecutionDO{}).
		Where("id = ?", id).
		Update("status", status).Error; err != nil {
		return pkg.Wrap(2093, "update workflow execution status failed", err)
	}
	return nil
}

// MarkFinished 终态写入（completed/failed/cancelled）+ outputs + errorMsg + finishedAt。
func (r *WorkflowExecutionRepo) MarkFinished(ctx context.Context, id string, status domain.WorkflowExecutionStatus, outputsJSON, errorMsg string) error {
	now := time.Now().UnixMilli()
	updates := map[string]any{
		"status":      status,
		"finished_at": now,
	}
	if outputsJSON != "" {
		updates["outputs"] = outputsJSON
	}
	if errorMsg != "" {
		updates["error_msg"] = errorMsg
	}
	if err := r.db.WithContext(ctx).
		Model(&domain.WorkflowExecutionDO{}).
		Where("id = ?", id).
		Updates(updates).Error; err != nil {
		return pkg.Wrap(2093, "finish workflow execution failed", err)
	}
	return nil
}

// ReapRunning 启动排空：把上次进程遗留的 running 执行标为 paused（可断点续跑）。
//
// 进程崩溃后这些执行没有任何 goroutine 在跑，保持 running 会让界面永远转圈；
// 标 paused 既如实表达「未完成」，也给了前端一个可恢复入口。
func (r *WorkflowExecutionRepo) ReapRunning(ctx context.Context) (int64, error) {
	res := r.db.WithContext(ctx).Model(&domain.WorkflowExecutionDO{}).
		Where("status = ?", domain.WorkflowStatusRunning).
		Update("status", domain.WorkflowStatusPaused)
	if res.Error != nil {
		return 0, pkg.Wrap(2093, "reap running workflow executions failed", res.Error)
	}
	return res.RowsAffected, nil
}

// WorkflowNodeExecutionRepo workflow_node_executions 表 CRUD。
type WorkflowNodeExecutionRepo struct{ db *gorm.DB }

// NewWorkflowNodeExecutionRepo 构造。
func NewWorkflowNodeExecutionRepo(db *gorm.DB) *WorkflowNodeExecutionRepo {
	return &WorkflowNodeExecutionRepo{db: db}
}

// Create 新建节点执行（status=running、startedAt=now）。
func (r *WorkflowNodeExecutionRepo) Create(ctx context.Context, row *domain.WorkflowNodeExecutionDO) error {
	if row.ID == "" {
		row.ID = pkg.NewID(domain.IDWorkflowNode)
	}
	if row.Status == "" {
		row.Status = domain.NodeStatusRunning
	}
	if row.StartedAt == 0 {
		row.StartedAt = time.Now().UnixMilli()
	}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return pkg.Wrap(2093, "create workflow node execution failed", err)
	}
	return nil
}

// MarkFinished 节点终态（completed/failed/skipped）+ outputs/inputs（JSON）+ errorMsg + finishedAt。
func (r *WorkflowNodeExecutionRepo) MarkFinished(ctx context.Context, id string, status domain.WorkflowNodeStatus, outputsJSON, errorMsg string) error {
	now := time.Now().UnixMilli()
	updates := map[string]any{
		"status":      status,
		"finished_at": now,
	}
	if outputsJSON != "" {
		updates["outputs"] = outputsJSON
	}
	if errorMsg != "" {
		updates["error_msg"] = errorMsg
	}
	if err := r.db.WithContext(ctx).
		Model(&domain.WorkflowNodeExecutionDO{}).
		Where("id = ?", id).
		Updates(updates).Error; err != nil {
		return pkg.Wrap(2093, "finish workflow node execution failed", err)
	}
	return nil
}

// SetInputs 节点起始输入（一般 renderCfgTemplates 后写回）。
func (r *WorkflowNodeExecutionRepo) SetInputs(ctx context.Context, id string, inputsJSON string) error {
	if err := r.db.WithContext(ctx).
		Model(&domain.WorkflowNodeExecutionDO{}).
		Where("id = ?", id).
		Update("inputs", inputsJSON).Error; err != nil {
		return pkg.Wrap(2093, "set workflow node inputs failed", err)
	}
	return nil
}

// ListByExecution 按 executionId 全量（按 startedAt 升序）。
func (r *WorkflowNodeExecutionRepo) ListByExecution(ctx context.Context, executionID string) ([]domain.WorkflowNodeExecutionDO, error) {
	var rows []domain.WorkflowNodeExecutionDO
	if err := r.db.WithContext(ctx).
		Where("execution_id = ?", executionID).
		Order("started_at ASC").
		Find(&rows).Error; err != nil {
		return nil, pkg.Wrap(2093, "list workflow node executions failed", err)
	}
	return rows, nil
}

// FindLatestByNode 取某执行下指定节点的最新一条执行记录（人工输入回填定位用）。
func (r *WorkflowNodeExecutionRepo) FindLatestByNode(ctx context.Context, executionID, nodeID string) (*domain.WorkflowNodeExecutionDO, error) {
	var row domain.WorkflowNodeExecutionDO
	if err := r.db.WithContext(ctx).
		Where("execution_id = ? AND node_id = ?", executionID, nodeID).
		Order("started_at DESC").
		First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrWorkflowNotFound
		}
		return nil, pkg.Wrap(2093, "find workflow node execution failed", err)
	}
	return &row, nil
}

// SetWaitingInput 把节点置为等待人工输入，并把提问文案写入 outputs（跨重启后 UI 仍可回放）。
func (r *WorkflowNodeExecutionRepo) SetWaitingInput(ctx context.Context, id, prompt string) error {
	b, _ := json.Marshal(map[string]any{"prompt": prompt})
	if err := r.db.WithContext(ctx).
		Model(&domain.WorkflowNodeExecutionDO{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": string(domain.NodeStatusWaiting), "outputs": string(b)}).Error; err != nil {
		return pkg.Wrap(2093, "set node waiting input failed", err)
	}
	return nil
}

// SetHumanInput 记录人工输入到 outputs.user_input，供断点续跑取用。
//
// 场景：进程重启后原等待 goroutine 已消失，输入无处投递；先落库，再由续跑注入节点。
// 人工输入节点在等待期间不写其他 outputs，整体覆盖是安全的。
func (r *WorkflowNodeExecutionRepo) SetHumanInput(ctx context.Context, id, value string) error {
	b, _ := json.Marshal(map[string]any{"user_input": value})
	if err := r.db.WithContext(ctx).
		Model(&domain.WorkflowNodeExecutionDO{}).
		Where("id = ?", id).
		Update("outputs", string(b)).Error; err != nil {
		return pkg.Wrap(2093, "set human input failed", err)
	}
	return nil
}
