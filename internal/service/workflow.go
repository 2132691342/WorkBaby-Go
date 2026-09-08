package service

import (
	"context"
	"encoding/json"
	"fmt"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/repo"
	"WorkBaby/internal/workflow"
	"WorkBaby/internal/workflow/nodes"
)

// WorkflowService 工作流编排：定义 CRUD + 运行触发 + 执行查询 + HumanInput 回填。
//
// 边界：service 不持有 event.Bus、不持有 LLM/Tool — 这些都通过 workflow.Executor 注入。
type WorkflowService struct {
	wfRepo   *repo.WorkflowRepo
	execRepo *repo.WorkflowExecutionRepo
	nodeRepo *repo.WorkflowNodeExecutionRepo
	exec     *workflow.Executor
	resolver *workflow.DefaultHumanInputResolver
}

// NewWorkflowService 注入仓储 + executor + resolver。
func NewWorkflowService(
	wfRepo *repo.WorkflowRepo,
	execRepo *repo.WorkflowExecutionRepo,
	nodeRepo *repo.WorkflowNodeExecutionRepo,
	exec *workflow.Executor,
	resolver *workflow.DefaultHumanInputResolver,
) *WorkflowService {
	return &WorkflowService{
		wfRepo:   wfRepo,
		execRepo: execRepo,
		nodeRepo: nodeRepo,
		exec:     exec,
		resolver: resolver,
	}
}

// Save 保存或更新（按 Name 唯一性由调用方保证；id 留空 → Create；非空 → Update）。
//
// 保存前对 Graph JSON 做完整校验；校验失败返回 ErrWorkflowInvalidGraph。
func (s *WorkflowService) Save(ctx context.Context, id string, req *domain.WorkflowGraphREQ) (*domain.WorkflowRESP, error) {
	if req.Name == "" {
		return nil, pkg.New(9102, "工作流名称必填", "")
	}
	g, err := workflow.ParseGraph(req.Graph)
	if err != nil {
		return nil, err
	}
	if err := workflow.Validate(g); err != nil {
		return nil, err
	}

	if id == "" {
		row := &domain.WorkflowDO{
			Name:        req.Name,
			Description: req.Description,
			Graph:       req.Graph,
			Enabled:     req.Enabled,
		}
		if err := s.wfRepo.Create(ctx, row); err != nil {
			return nil, err
		}
		return toWorkflowRESP(row), nil
	}
	row, err := s.wfRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	row.Name = req.Name
	row.Description = req.Description
	row.Graph = req.Graph
	row.Enabled = req.Enabled
	if err := s.wfRepo.Update(ctx, row); err != nil {
		return nil, err
	}
	return toWorkflowRESP(row), nil
}

// List 列出全部工作流（含 disabled）。
func (s *WorkflowService) List(ctx context.Context) ([]domain.WorkflowRESP, error) {
	rows, err := s.wfRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.WorkflowRESP, 0, len(rows))
	for i := range rows {
		out = append(out, *toWorkflowRESP(&rows[i]))
	}
	return out, nil
}

// Get 按 ID 取工作流定义。
func (s *WorkflowService) Get(ctx context.Context, id string) (*domain.WorkflowRESP, error) {
	row, err := s.wfRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return toWorkflowRESP(row), nil
}

// Delete 软删。
func (s *WorkflowService) Delete(ctx context.Context, id string) error {
	return s.wfRepo.Delete(ctx, id)
}

// Run 触发一次执行；返回 executionID。
func (s *WorkflowService) Run(ctx context.Context, id string, inputs map[string]any) (string, error) {
	execID, err := s.exec.Run(ctx, id, inputs)
	if err != nil {
		// GetByID / ParseGraph / Validate / Run 阶段都可能失败；executor 内部已 MarkFinished
		return execID, err
	}
	return execID, nil
}

// ListExecutions 按 workflowId 列历史执行（limit 默认 100）。
func (s *WorkflowService) ListExecutions(ctx context.Context, workflowID string, limit int) ([]domain.WorkflowExecutionRESP, error) {
	rows, err := s.execRepo.ListByWorkflow(ctx, workflowID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]domain.WorkflowExecutionRESP, 0, len(rows))
	for i := range rows {
		out = append(out, toExecutionRESP(&rows[i]))
	}
	return out, nil
}

// GetExecution 拉一次执行 + 它的所有节点执行。
func (s *WorkflowService) GetExecution(ctx context.Context, executionID string) (*domain.ExecutionDetailRESP, error) {
	execRow, err := s.execRepo.GetByID(ctx, executionID)
	if err != nil {
		return nil, err
	}
	nodeRows, err := s.nodeRepo.ListByExecution(ctx, executionID)
	if err != nil {
		return nil, err
	}
	nodes := make([]domain.WorkflowNodeExecutionRESP, 0, len(nodeRows))
	for i := range nodeRows {
		nodes = append(nodes, toNodeExecutionRESP(&nodeRows[i]))
	}
	return &domain.ExecutionDetailRESP{
		Execution: toExecutionRespFromDB(execRow),
		Nodes:     nodes,
	}, nil
}

// ResolveHumanInput 前端回填：把 value 投递到等待中的 resolver；返回 bool 表示是否成功。
func (s *WorkflowService) ResolveHumanInput(executionID, nodeID, value string) error {
	if !s.resolver.Deliver(executionID, nodeID, value) {
		return pkg.New(9106, "no waiting human input for this node", executionID+"/"+nodeID)
	}
	return nil
}

// Cancel 取消正在跑的 workflow。
func (s *WorkflowService) Cancel(ctx context.Context, executionID string) error {
	return s.exec.Cancel(ctx, executionID)
}

// Pause 暂停正在跑的 workflow（层边界挂起）。
func (s *WorkflowService) Pause(ctx context.Context, executionID string) error {
	return s.exec.Pause(ctx, executionID)
}

// Resume 恢复已暂停的 workflow。
func (s *WorkflowService) Resume(ctx context.Context, executionID string) error {
	return s.exec.Resume(ctx, executionID)
}

// GraphToDAG 把后端 Graph JSON 转换为前端 Vue Flow 可编辑格式。
//
// 双向语义：
//   - NodeDef.Pos → DAGNode.Pos（画布坐标）
//   - NodeDef.Branch → 指向 condition 上游的边的 SourceHandle（分支出口）
//   - NodeDef.Inputs / Graph.Inputs → DAGNode.Inputs / WorkflowDAGRESP.Inputs
func (s *WorkflowService) GraphToDAG(ctx context.Context, id string) (*domain.WorkflowDAGRESP, error) {
	wf, err := s.wfRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	g, err := workflow.ParseGraph(wf.Graph)
	if err != nil {
		return nil, err
	}
	condOf := map[string]bool{} // 节点 ID → 是否 condition
	for _, n := range g.Nodes {
		condOf[n.ID] = n.Type == domain.WorkflowNodeCondition
	}
	out := &domain.WorkflowDAGRESP{Name: g.Name, Inputs: g.Inputs, Outputs: g.Outputs}
	for _, n := range g.Nodes {
		node := domain.WorkflowDAGNode{
			ID:     n.ID,
			Type:   string(n.Type),
			Params: n.Config,
			Branch: n.Branch,
			Inputs: n.Inputs,
		}
		if n.Pos != nil {
			node.Pos = &domain.WorkflowPos{X: n.Pos.X, Y: n.Pos.Y}
		}
		out.Nodes = append(out.Nodes, node)
	}
	// deps → edges（from=dependency, to=current）；condition 上游且目标声明分支 → 该边带 source_handle
	seen := map[string]bool{}
	for _, n := range g.Nodes {
		for _, d := range n.Deps {
			key := d + "->" + n.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			e := domain.WorkflowDAGEdge{From: d, To: n.ID}
			if condOf[d] && n.Branch != "" {
				e.SourceHandle = n.Branch
			}
			out.Edges = append(out.Edges, e)
		}
	}
	return out, nil
}

// SaveGraph 把前端 Vue Flow 格式保存为后端 Graph JSON（校验后落库）。
//
// 保存语义：
//   - DAGNode.Pos → NodeDef.Pos（画布坐标持久化）
//   - 来自 condition 源的边带 source_handle → 目标 NodeDef.Branch
//   - 其余边 → NodeDef.Deps
func (s *WorkflowService) SaveGraph(ctx context.Context, id string, req *domain.WorkflowDAGREQ) (*domain.WorkflowDAGRESP, error) {
	if req.Name == "" {
		return nil, pkg.New(9102, "工作流名称必填", "")
	}
	byID := map[string]domain.WorkflowDAGNode{}
	nodeTypes := map[string]domain.WorkflowNodeType{}
	for _, n := range req.Nodes {
		byID[n.ID] = n
		nodeTypes[n.ID] = domain.WorkflowNodeType(n.Type)
	}

	g := &workflow.Graph{Name: req.Name, Inputs: req.Inputs, Nodes: []*workflow.NodeDef{}, Outputs: req.Outputs}
	for _, n := range req.Nodes {
		nd := &workflow.NodeDef{
			ID:     n.ID,
			Type:   workflow.NodeType(n.Type),
			Config: n.Params,
			Branch: n.Branch,
			Inputs: n.Inputs,
		}
		if n.Pos != nil {
			nd.Pos = &workflow.Pos{X: n.Pos.X, Y: n.Pos.Y}
		}
		g.Nodes = append(g.Nodes, nd)
	}
	// edges：普通 → deps；condition 源 → 目标 branch + deps
	for _, e := range req.Edges {
		if _, ok := byID[e.From]; !ok {
			continue
		}
		if _, ok := byID[e.To]; !ok {
			continue
		}
		if nodeTypes[e.From] == domain.WorkflowNodeCondition && e.SourceHandle != "" {
			for _, n := range g.Nodes {
				if n.ID == e.To {
					n.Branch = e.SourceHandle
				}
			}
		}
		for _, n := range g.Nodes {
			if n.ID == e.To {
				n.Deps = appendUnique(n.Deps, e.From)
			}
		}
	}
	if err := workflow.Validate(g); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(g)
	if err != nil {
		return nil, pkg.Wrap(9102, "marshal workflow graph failed", err)
	}
	wf, err := s.wfRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	wf.Graph = string(raw)
	if err := s.wfRepo.Update(ctx, wf); err != nil {
		return nil, err
	}
	return s.GraphToDAG(ctx, id)
}

// ListWorkflowNodeTypes 返回内置节点类型目录（schema 字段描述），驱动前端调色板与属性面板。
func (s *WorkflowService) ListWorkflowNodeTypes() []domain.WorkflowNodeTypeRESP {
	out := make([]domain.WorkflowNodeTypeRESP, 0, len(domain.AllWorkflowNodeTypes))
	for _, t := range domain.AllWorkflowNodeTypes {
		sch := nodes.SchemaFor(t)
		fields := make([]domain.WorkflowNodeFieldRESP, 0, len(sch.Fields))
		for _, f := range sch.Fields {
			fields = append(fields, domain.WorkflowNodeFieldRESP{
				Name:        f.Name,
				Label:       f.Label,
				Type:        string(f.Type),
				Required:    f.Required,
				Description: f.Description,
				Options:     f.Options,
				Default:     f.Default,
			})
		}
		out = append(out, domain.WorkflowNodeTypeRESP{Type: string(t), Label: nodeTypeLabel(t), Fields: fields})
	}
	return out
}

// nodeTypeLabel 节点类型的展示名（目录端点返回；前端可被 i18n 覆盖）。
func nodeTypeLabel(t domain.WorkflowNodeType) string {
	switch t {
	case domain.WorkflowNodeLLM:
		return "LLM 调用"
	case domain.WorkflowNodeTool:
		return "工具调用"
	case domain.WorkflowNodeCode:
		return "代码执行"
	case domain.WorkflowNodeCondition:
		return "条件分支"
	case domain.WorkflowNodeHTTP:
		return "HTTP 请求"
	case domain.WorkflowNodeHumanInput:
		return "人工输入"
	case domain.WorkflowNodeChannel:
		return "通知通道"
	}
	return string(t)
}

// appendUnique 去重追加。
func appendUnique(slice []string, v string) []string {
	for _, x := range slice {
		if x == v {
			return slice
		}
	}
	return append(slice, v)
}

// ValidateGraph 仅做图定义校验，不落库（前端编辑器用）。
func (s *WorkflowService) ValidateGraph(raw string) error {
	g, err := workflow.ParseGraph(raw)
	if err != nil {
		return err
	}
	return workflow.Validate(g)
}

// toWorkflowRESP DO → RESP。
func toWorkflowRESP(wf *domain.WorkflowDO) *domain.WorkflowRESP {
	return &domain.WorkflowRESP{
		ID:          wf.ID,
		Name:        wf.Name,
		Description: wf.Description,
		Graph:       wf.Graph,
		Enabled:     wf.Enabled,
		CreatedAt:   wf.CreatedAt,
		UpdatedAt:   wf.UpdatedAt,
	}
}

func toExecutionRESP(row *domain.WorkflowExecutionDO) domain.WorkflowExecutionRESP {
	return toExecutionRespFromDB(row)
}

func toExecutionRespFromDB(row *domain.WorkflowExecutionDO) domain.WorkflowExecutionRESP {
	var ins, outs map[string]any
	if row.Inputs != "" {
		_ = json.Unmarshal([]byte(row.Inputs), &ins)
	}
	if row.Outputs != "" {
		_ = json.Unmarshal([]byte(row.Outputs), &outs)
	}
	return domain.WorkflowExecutionRESP{
		ID:         row.ID,
		WorkflowID: row.WorkflowID,
		Status:     row.Status,
		Inputs:     ins,
		Outputs:    outs,
		ErrorMsg:   row.ErrorMsg,
		StartedAt:  row.StartedAt,
		FinishedAt: row.FinishedAt,
		CreatedAt:  row.CreatedAt,
	}
}

func toNodeExecutionRESP(row *domain.WorkflowNodeExecutionDO) domain.WorkflowNodeExecutionRESP {
	var ins, outs map[string]any
	if row.Inputs != "" {
		_ = json.Unmarshal([]byte(row.Inputs), &ins)
	}
	if row.Outputs != "" {
		_ = json.Unmarshal([]byte(row.Outputs), &outs)
	}
	return domain.WorkflowNodeExecutionRESP{
		ID:          row.ID,
		ExecutionID: row.ExecutionID,
		NodeID:      row.NodeID,
		NodeType:    row.NodeType,
		Status:      row.Status,
		Inputs:      ins,
		Outputs:     outs,
		ErrorMsg:    row.ErrorMsg,
		StartedAt:   row.StartedAt,
		FinishedAt:  row.FinishedAt,
	}
}

// unused — 防止 fmt 误删引用
var _ = fmt.Sprintf
