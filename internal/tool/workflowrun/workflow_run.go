// Package workflowrun 提供 run_workflow 工具：Agent 在对话中触发已编排的工作流并取回输出。
//
// 执行是同步的（Executor.Run 跑完整图才返回），因此用 Meta.TimeoutSec 限制等待；
// 含 human_input 节点的工作流会挂起等待人工输入，不适合由 Agent 触发。
package workflowrun

import (
	"context"
	"encoding/json"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/tool"
)

// Runner 工作流触发与查询契约（service.WorkflowService 实现）。
type Runner interface {
	Run(ctx context.Context, id string, inputs map[string]any) (string, error)
	GetExecution(ctx context.Context, executionID string) (*domain.ExecutionDetailRESP, error)
}

// Tool 工作流触发工具。
type Tool struct{ r Runner }

// New 构造工作流触发工具。
func New(r Runner) *Tool { return &Tool{r: r} }

func (t *Tool) Name() string              { return "run_workflow" }
func (t *Tool) RiskLevel() tool.RiskLevel { return tool.RiskExec }

func (t *Tool) Meta() tool.ToolMeta {
	return tool.ToolMeta{TimeoutSec: 120, MaxResultChars: 8000, UIHint: "workflow"}
}

func (t *Tool) Description() string {
	return "按 ID 运行一条已编排好的工作流并取回它的输出。适合需要固定多步流程、人工复核或定时复用的任务。"
}

func (t *Tool) Schema() tool.ToolSchema {
	return tool.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"required": ["workflow_id"],
			"properties": {
				"workflow_id": {"type": "string", "description": "工作流 ID（见上下文中「可用工作流」清单）"},
				"inputs": {"type": "object", "description": "传给工作流的入参；键为图内引用的输入名"}
			}
		}`),
	}
}

type runReq struct {
	WorkflowID string         `json:"workflow_id"`
	Inputs     map[string]any `json:"inputs"`
}

func (t *Tool) Execute(ctx context.Context, args json.RawMessage) tool.ToolResult {
	var req runReq
	if err := json.Unmarshal(args, &req); err != nil {
		return tool.ToolResult{Err: pkg.Wrap(4013, "run_workflow 参数解析失败", err)}
	}
	if req.WorkflowID == "" {
		return tool.ToolResult{Err: pkg.New(4014, "run_workflow 缺少 workflow_id", "")}
	}
	if req.Inputs == nil {
		req.Inputs = map[string]any{}
	}
	execID, err := t.r.Run(ctx, req.WorkflowID, req.Inputs)
	if err != nil {
		return tool.ToolResult{
			Err:     pkg.Wrap(4015, "工作流执行失败", err),
			Content: "工作流执行失败（execution_id=" + execID + "）。",
			Data:    map[string]any{"execution_id": execID, "workflow_id": req.WorkflowID},
		}
	}
	detail, derr := t.r.GetExecution(ctx, execID)
	if derr != nil || detail == nil {
		return tool.ToolResult{
			Content: "工作流已执行完成，但读取结果失败（execution_id=" + execID + "）。",
			Err:     derr,
			Data:    map[string]any{"execution_id": execID},
		}
	}
	exec := detail.Execution
	content := "工作流执行" + humanStatus(exec.Status) + "，execution_id=" + execID
	if len(exec.Outputs) > 0 {
		if bs, merr := json.Marshal(exec.Outputs); merr == nil {
			content += "\n输出：" + pkg.TruncateRunes(string(bs), 6000)
		}
	}
	if exec.ErrorMsg != "" {
		content += "\n错误：" + exec.ErrorMsg
	}
	return tool.ToolResult{
		Content: content,
		Data: map[string]any{
			"execution_id": execID,
			"workflow_id":  req.WorkflowID,
			"status":       exec.Status,
			"outputs":      exec.Outputs,
		},
	}
}

// humanStatus 执行状态中文化。
func humanStatus(s domain.WorkflowExecutionStatus) string {
	switch s {
	case domain.WorkflowStatusCompleted:
		return "成功"
	case domain.WorkflowStatusFailed:
		return "失败"
	case domain.WorkflowStatusCancelled:
		return "已取消"
	case domain.WorkflowStatusPaused:
		return "暂停（等待人工输入）"
	default:
		return "结束（" + string(s) + "）"
	}
}
