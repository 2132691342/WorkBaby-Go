package api

import (
	"WorkBaby/internal/domain"
)

// ListWorkflows 全部工作流（含 disabled）。
func (h *Handler) ListWorkflows() ([]domain.WorkflowRESP, error) {
	return h.workflowSvc.List(h.ctx)
}

// GetWorkflow 按 ID。
func (h *Handler) GetWorkflow(id string) (domain.WorkflowRESP, error) {
	r, err := h.workflowSvc.Get(h.ctx, id)
	if err != nil {
		return domain.WorkflowRESP{}, err
	}
	return *r, nil
}

// SaveWorkflow 创建或更新；id 为空 → Create，否则 Update。
func (h *Handler) SaveWorkflow(id string, req domain.WorkflowGraphREQ) (domain.WorkflowRESP, error) {
	r, err := h.workflowSvc.Save(h.ctx, id, &req)
	if err != nil {
		return domain.WorkflowRESP{}, err
	}
	return *r, nil
}

// DeleteWorkflow 软删。
func (h *Handler) DeleteWorkflow(id string) error {
	return h.workflowSvc.Delete(h.ctx, id)
}

// ValidateWorkflowGraph 仅校验，不落库（前端编辑器实时反馈）。
func (h *Handler) ValidateWorkflowGraph(graph string) error {
	return h.workflowSvc.ValidateGraph(graph)
}

// RunWorkflow 手动触发；返回 execution_id 供前端轮询详情。
func (h *Handler) RunWorkflow(id string, req domain.WorkflowRunREQ) (domain.WorkflowRunRESP, error) {
	execID, err := h.workflowSvc.Run(h.ctx, id, req.Inputs)
	if err != nil {
		return domain.WorkflowRunRESP{}, err
	}
	return domain.WorkflowRunRESP{ExecutionID: execID}, nil
}

// ListWorkflowExecutions 列工作流的执行历史。
func (h *Handler) ListWorkflowExecutions(workflowID string, limit int) ([]domain.WorkflowExecutionRESP, error) {
	return h.workflowSvc.ListExecutions(h.ctx, workflowID, limit)
}

// GetWorkflowExecution 单次执行详情 + 节点执行列表。
func (h *Handler) GetWorkflowExecution(executionID string) (domain.ExecutionDetailRESP, error) {
	d, err := h.workflowSvc.GetExecution(h.ctx, executionID)
	if err != nil {
		return domain.ExecutionDetailRESP{}, err
	}
	return *d, nil
}

// ResolveHumanInput 前端回填工作流 HumanInput 节点的等待。
func (h *Handler) ResolveHumanInput(executionID string, req domain.ResolveHumanInputREQ) error {
	return h.workflowSvc.ResolveHumanInput(executionID, req.NodeID, req.Value)
}

// CancelWorkflowExecution 取消正在跑的工作流。
func (h *Handler) CancelWorkflowExecution(executionID string) error {
	return h.workflowSvc.Cancel(h.ctx, executionID)
}

// PauseWorkflowExecution 暂停正在跑的工作流。
func (h *Handler) PauseWorkflowExecution(executionID string) error {
	return h.workflowSvc.Pause(h.ctx, executionID)
}

// ResumeWorkflowExecution 恢复已暂停的工作流。
func (h *Handler) ResumeWorkflowExecution(executionID string) error {
	return h.workflowSvc.Resume(h.ctx, executionID)
}

// GetWorkflowGraph 返回前端 Vue Flow 可编辑的图（Graph JSON → DAG）。
func (h *Handler) GetWorkflowGraph(id string) (domain.WorkflowDAGRESP, error) {
	d, err := h.workflowSvc.GraphToDAG(h.ctx, id)
	if err != nil {
		return domain.WorkflowDAGRESP{}, err
	}
	return *d, nil
}

// UpdateWorkflowGraph 保存前端 Vue Flow 格式的图（DAG → Graph JSON）。
func (h *Handler) UpdateWorkflowGraph(id string, req domain.WorkflowDAGREQ) (domain.WorkflowDAGRESP, error) {
	d, err := h.workflowSvc.SaveGraph(h.ctx, id, &req)
	if err != nil {
		return domain.WorkflowDAGRESP{}, err
	}
	return *d, nil
}

// ListWorkflowNodeTypes 返回内置节点类型目录（schema 字段描述）。
func (h *Handler) ListWorkflowNodeTypes() []domain.WorkflowNodeTypeRESP {
	return h.workflowSvc.ListWorkflowNodeTypes()
}
