package capability

import (
	"context"
	"strings"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/harness"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/tool"
	"WorkBaby/internal/tool/workflowrun"
)

// WorkflowRunner 工作流清单与触发契约（service.WorkflowService 实现）。
type WorkflowRunner interface {
	workflowrun.Runner
	List(ctx context.Context) ([]domain.WorkflowRESP, error)
}

// workflowCap 让 Agent 看见并触发已编排的工作流。
type workflowCap struct {
	runner   WorkflowRunner
	maxItems int
	tools    []tool.Tool
}

// NewWorkflow 构造工作流能力；runner 为 nil 时能力整体降级为空。
func NewWorkflow(runner WorkflowRunner) Capability {
	c := &workflowCap{runner: runner, maxItems: 20}
	if runner != nil {
		c.tools = []tool.Tool{workflowrun.New(runner)}
	}
	return c
}

func (c *workflowCap) ID() string { return "workflow" }

func (c *workflowCap) Tools() []tool.Tool { return c.tools }

// Preload 注入可用工作流清单，让「跑一下那个流程」类指令有明确落点。
// 清单为空时不注入，避免每轮都占上下文。
func (c *workflowCap) Preload(ctx context.Context, p *PreloadCtx) ([]harness.ContextPiece, error) {
	if c.runner == nil {
		return nil, nil
	}
	rows, err := c.runner.List(ctx)
	if err != nil {
		return nil, err
	}
	var sb strings.Builder
	n := 0
	for i := range rows {
		if !rows[i].Enabled {
			continue
		}
		sb.WriteString("- ")
		sb.WriteString(rows[i].ID)
		sb.WriteString(" · ")
		sb.WriteString(rows[i].Name)
		if d := strings.TrimSpace(rows[i].Description); d != "" {
			sb.WriteString("：")
			sb.WriteString(pkg.TruncateRunes(d, 80))
		}
		sb.WriteString("\n")
		n++
		if n >= c.maxItems {
			break
		}
	}
	if n == 0 {
		return nil, nil
	}
	return []harness.ContextPiece{{
		Key:   "workflows",
		Title: "可用工作流（用 run_workflow 按 ID 触发）",
		Body:  strings.TrimSpace(sb.String()),
	}}, nil
}

func (c *workflowCap) Capture(_ context.Context, _ *CaptureCtx) error { return nil }
