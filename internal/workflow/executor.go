// Package workflow 实现 DAG 工作流引擎：图定义、校验、拓扑分层并行执行、节点落库与事件。
//
// 本文件实现执行器：拓扑分层（Kahn）→ 逐层执行 → 层内节点并行 → condition 分支过滤。
package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/event"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/repo"
	"WorkBaby/internal/workflow/nodes"
	"gorm.io/gorm"
)

// ChannelSenderType 只是为了 executor 编译期不依赖 internal/channel；运行时仍由 service 注入。
//
// alias 已避免循环；实际接口签名在 nodes.ChannelSender。
type ChannelSenderType = nodes.ChannelSender

// Executor 持有依赖 + 节点注册表 + HumanInputResolver；运行入口 Run()。
type Executor struct {
	db       *gorm.DB
	wfRepo   *repo.WorkflowRepo
	execRepo *repo.WorkflowExecutionRepo
	nodeRepo *repo.WorkflowNodeExecutionRepo
	bus      *event.Bus
	resolver nodes.HumanInputResolver
	sender   ChannelSenderType
	nodes    map[domain.WorkflowNodeType]nodes.Node
	runs     sync.Map // executionID → context.CancelFunc；存活 run 注册表，Cancel 据此打断
	pauses   sync.Map // executionID → chan struct{}；暂停信号（nil 表示已恢复），Pause/Resume 据此挂起/续跑
}

// ExecutorConfig 启动期一次性配置。
type ExecutorConfig struct {
	DB       *gorm.DB
	WFRepo   *repo.WorkflowRepo
	ExecRepo *repo.WorkflowExecutionRepo
	NodeRepo *repo.WorkflowNodeExecutionRepo
	Bus      *event.Bus
	Resolver nodes.HumanInputResolver
	Sender   ChannelSenderType // 可选；nil 时 ChannelNode 报错
	Nodes    []nodes.Node
}

// New 构造执行器；同时把 7 个节点注册到 nodes.registry。
func New(cfg ExecutorConfig) *Executor {
	m := map[domain.WorkflowNodeType]nodes.Node{}
	for _, n := range cfg.Nodes {
		m[n.Type()] = n
	}
	if cfg.Resolver != nil {
		nodes.RegisterAll(cfg.Nodes...)
	}
	return &Executor{
		db:       cfg.DB,
		wfRepo:   cfg.WFRepo,
		execRepo: cfg.ExecRepo,
		nodeRepo: cfg.NodeRepo,
		bus:      cfg.Bus,
		resolver: cfg.Resolver,
		sender:   cfg.Sender,
		nodes:    m,
	}
}

// Run 启动一次工作流执行（同步等待全部完成）；返回 executionID（出错也返回，便于查询）。
//
// 事件流：workflow:started → workflow:node-start/node-done × N → workflow:completed / workflow:failed。
func (e *Executor) Run(ctx context.Context, workflowID string, inputs map[string]any) (string, error) {
	row, err := e.wfRepo.GetByID(ctx, workflowID)
	if err != nil {
		return "", err
	}

	g, err := ParseGraph(row.Graph)
	if err != nil {
		return "", err
	}
	if err := Validate(g); err != nil {
		return "", err
	}

	inputsJSON, _ := json.Marshal(inputs)
	execID := pkg.NewID(domain.IDWorkflowExec)
	now := time.Now().UnixMilli()
	pkg.L.Info("workflow run start", "executionID", execID, "workflowID", workflowID, "nodes", len(g.Nodes))
	startedAt := time.Now()
	if err := e.execRepo.Create(ctx, &domain.WorkflowExecutionDO{
		ID:         execID,
		WorkflowID: workflowID,
		Status:     domain.WorkflowStatusRunning,
		Inputs:     string(inputsJSON),
		StartedAt:  now,
	}); err != nil {
		return "", err
	}

	e.bus.Publish("workflow:started", map[string]any{
		"workflowID":  workflowID,
		"executionID": execID,
		"inputs":      inputs,
	})

	// 可取消的 runCtx：终态唯一所有者是本 Run 流程（Cancel 只发取消信号，不写状态）
	runCtx, cancel := context.WithCancel(ctx)
	e.runs.Store(execID, cancel)
	defer func() { cancel(); e.runs.Delete(execID) }()

	outputs := map[string]map[string]any{}  // nodeID → 输出
	conditionOutputs := map[string]string{} // condition 节点 ID → branch 标签
	skipped := map[string]bool{}            // nodeID → 已跳过（分支跳过传播依据）

	layers, err := TopologicalLayers(g)
	if err != nil {
		e.failExec(ctx, execID, err)
		return execID, err
	}

	for _, layer := range layers {
		if err := e.waitIfPaused(runCtx, execID); err != nil {
			e.abortExec(ctx, execID, err, true)
			return execID, err
		}
		if err := e.runLayer(runCtx, g, execID, layer, inputs, outputs, conditionOutputs, skipped); err != nil {
			e.abortExec(ctx, execID, err, runCtx.Err() != nil)
			return execID, err
		}
	}

	final := map[string]any{}
	for k, ref := range g.Outputs {
		if v, ok := resolveRef(outputs, ref); ok {
			final[k] = v
		}
	}
	outJSON, _ := json.Marshal(final)
	if err := e.execRepo.MarkFinished(ctx, execID, domain.WorkflowStatusCompleted, string(outJSON), ""); err != nil {
		return execID, err
	}
	pkg.L.Info("workflow run done", "executionID", execID, "workflowID", workflowID,
		"latencyMs", time.Since(startedAt).Milliseconds(), "outputKeys", len(final))
	e.bus.Publish("workflow:completed", map[string]any{
		"workflowID":  workflowID,
		"executionID": execID,
		"outputs":     final,
	})
	return execID, nil
}

// runLayer 同一层节点并行执行；首个错误快速失败。
// skipped 记录本层之前已被跳过的节点（分支跳过传播）。
func (e *Executor) runLayer(
	ctx context.Context,
	g *Graph,
	execID string,
	layer []string,
	inputs map[string]any,
	outputs map[string]map[string]any,
	conditionOutputs map[string]string,
	skipped map[string]bool,
) error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for _, nodeID := range layer {
		nd := FindNode(g, nodeID)
		if nd == nil {
			continue
		}
		// 分支跳过判定也读 conditionOutputs / skipped，与兄弟 goroutine 写同一组 map——持锁。
		mu.Lock()
		runnable := NodeRunnable(nd, conditionOutputs, skipped)
		mu.Unlock()
		if !runnable {
			e.recordSkipped(execID, nd)
			mu.Lock()
			skipped[nd.ID] = true
			mu.Unlock()
			continue
		}

		wg.Add(1)
		go func(nodeDef *NodeDef) {
			defer wg.Done()

			// 读 outputs / conditionOutputs 必须持锁：层内兄弟 goroutine 并发写。
			mu.Lock()
			upstream := ResolveUpstream(nodeDef, outputs)
			renderedCfg := RenderConfigMap(nodeDef.Config, outputs)
			mu.Unlock()

			// 给节点注入执行上下文：HumanInput 节点据此拼 waiter key（识别 execution/node）。
			// 上游覆盖在「执行上下文」之前，保留上下文优先。
			ctxInputs := map[string]any{
				"__executionId__": execID,
				"__nodeId__":      nodeDef.ID,
			}
			for k, v := range upstream {
				ctxInputs[k] = v
			}

			nodeExecID := e.startNodeExec(ctx, execID, nodeDef, upstream)

			impl, ok := e.nodes[nodeDef.Type]
			if !ok {
				e.finishNodeFailed(ctx, nodeExecID, pkg.New(9105, "no impl for "+string(nodeDef.Type), ""))
				mu.Lock()
				if firstErr == nil {
					firstErr = pkg.New(9105, "no impl for "+string(nodeDef.Type), "")
				}
				mu.Unlock()
				return
			}

			out, err := impl.Execute(ctx, ctxInputs, renderedCfg, upstream)
			if err != nil {
				e.finishNodeFailed(ctx, nodeExecID, err)
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}

			e.finishNodeOK(ctx, nodeExecID, out)

			mu.Lock()
			outputs[nodeDef.ID] = out
			// condition 节点的 branch 输出缓存（NodeActive 用）
			if nodeDef.Type == domain.WorkflowNodeCondition {
				if b, ok := out["branch"].(string); ok {
					conditionOutputs[nodeDef.ID] = b
				}
			}
			mu.Unlock()
		}(nd)
	}
	wg.Wait()
	return firstErr
}

// startNodeExec 创建节点执行记录 + emit workflow:node-start。
func (e *Executor) startNodeExec(ctx context.Context, execID string, nd *NodeDef, upstream map[string]any) string {
	id := pkg.NewID(domain.IDWorkflowNode)
	inputs := map[string]any{"upstream": upstream, "config": nd.Config}
	b, _ := json.Marshal(inputs)
	_ = e.nodeRepo.Create(ctx, &domain.WorkflowNodeExecutionDO{
		ID:          id,
		ExecutionID: execID,
		NodeID:      nd.ID,
		NodeType:    nd.Type,
		Status:      domain.NodeStatusRunning,
		Inputs:      string(b),
		StartedAt:   time.Now().UnixMilli(),
	})
	e.bus.Publish("workflow:node-start", map[string]any{
		"executionID": execID,
		"nodeID":      nd.ID,
		"nodeType":    string(nd.Type),
		"nodeExecID":  id,
	})
	return id
}

// finishNodeOK 节点完成 → emit + 落库。
func (e *Executor) finishNodeOK(ctx context.Context, nodeExecID string, out map[string]any) {
	b, _ := json.Marshal(out)
	_ = e.nodeRepo.MarkFinished(ctx, nodeExecID, domain.NodeStatusCompleted, string(b), "")
	e.bus.Publish("workflow:node-done", map[string]any{
		"nodeExecID": nodeExecID,
		"outputs":    out,
		"status":     string(domain.NodeStatusCompleted),
	})
}

// finishNodeFailed 节点失败。
func (e *Executor) finishNodeFailed(ctx context.Context, nodeExecID string, err error) {
	pkg.L.Warn("workflow node failed", "nodeExecID", nodeExecID, "err", err.Error())
	_ = e.nodeRepo.MarkFinished(ctx, nodeExecID, domain.NodeStatusFailed, "", err.Error())
	e.bus.Publish("workflow:node-done", map[string]any{
		"nodeExecID": nodeExecID,
		"error":      err.Error(),
		"status":     string(domain.NodeStatusFailed),
	})
}

// recordSkipped 记录被 condition 分支跳过的节点（不落 inputs/outputs，仅记状态）。
func (e *Executor) recordSkipped(execID string, nd *NodeDef) {
	id := pkg.NewID(domain.IDWorkflowNode)
	_ = e.nodeRepo.Create(context.Background(), &domain.WorkflowNodeExecutionDO{
		ID:          id,
		ExecutionID: execID,
		NodeID:      nd.ID,
		NodeType:    nd.Type,
		Status:      domain.NodeStatusSkipped,
		StartedAt:   time.Now().UnixMilli(),
	})
	e.bus.Publish("workflow:node-done", map[string]any{
		"nodeExecID": id,
		"nodeID":     nd.ID,
		"status":     string(domain.NodeStatusSkipped),
	})
}

// failExec 工作流失败终态。
func (e *Executor) failExec(ctx context.Context, execID string, err error) {
	pkg.L.Error("workflow run failed", "executionID", execID, "err", err.Error())
	_ = e.execRepo.MarkFinished(ctx, execID, domain.WorkflowStatusFailed, "", err.Error())
	e.bus.Publish("workflow:failed", map[string]any{
		"executionID": execID,
		"error":       err.Error(),
	})
}

// abortExec run 错误收尾：用户取消 → cancelled；真实节点错误 → failed。
func (e *Executor) abortExec(ctx context.Context, execID string, runErr error, cancelled bool) {
	if cancelled {
		_ = e.execRepo.MarkFinished(ctx, execID, domain.WorkflowStatusCancelled, "", "cancelled by user")
		e.bus.Publish("workflow:cancelled", map[string]any{"executionID": execID})
		return
	}
	e.failExec(ctx, execID, runErr)
}

// Cancel 取消正在跑的 workflow：向 runCtx 发取消信号，终态由 Run 流程收尾（单一写入者）。
// 孤儿 running 行（上次进程未正常收尾，无存活 run）直接落 cancelled 终态。
func (e *Executor) Cancel(ctx context.Context, executionID string) error {
	row, err := e.execRepo.GetByID(ctx, executionID)
	if err != nil {
		return err
	}
	if row.Status != domain.WorkflowStatusRunning {
		return pkg.New(9100, fmt.Sprintf("workflow execution already %s", row.Status), "")
	}
	if c, ok := e.runs.Load(executionID); ok {
		c.(context.CancelFunc)()
		return nil
	}
	return e.execRepo.MarkFinished(ctx, executionID, domain.WorkflowStatusCancelled, "", "cancelled by user")
}

// Pause 暂停正在跑的 workflow：在下一个层边界挂起，等待 Resume 或 Cancel。
// 幂等：已暂停 / 已终态返回错误。
func (e *Executor) Pause(ctx context.Context, executionID string) error {
	row, err := e.execRepo.GetByID(ctx, executionID)
	if err != nil {
		return err
	}
	if row.Status == domain.WorkflowStatusPaused {
		return pkg.New(9108, "workflow execution already paused", "")
	}
	if row.Status != domain.WorkflowStatusRunning {
		return pkg.New(9100, fmt.Sprintf("workflow execution already %s", row.Status), "")
	}
	if _, ok := e.runs.Load(executionID); !ok {
		return pkg.New(9109, "workflow execution not active", "")
	}
	// 注册暂停信号（chan 非 nil = 需等待恢复）
	ch := make(chan struct{})
	e.pauses.Store(executionID, ch)
	if err := e.execRepo.UpdateStatus(ctx, executionID, domain.WorkflowStatusPaused); err != nil {
		e.pauses.Delete(executionID)
		return err
	}
	e.bus.Publish("workflow:paused", map[string]any{"executionID": executionID})
	return nil
}

// Resume 恢复已暂停的 workflow。
func (e *Executor) Resume(ctx context.Context, executionID string) error {
	v, ok := e.pauses.Load(executionID)
	if !ok {
		return pkg.New(9110, "workflow execution not paused", "")
	}
	ch := v.(chan struct{})
	e.pauses.Delete(executionID)
	close(ch) // 唤醒等待中的 runLayer 检查点
	if err := e.execRepo.UpdateStatus(ctx, executionID, domain.WorkflowStatusRunning); err != nil {
		return err
	}
	e.bus.Publish("workflow:resumed", map[string]any{"executionID": executionID})
	return nil
}

// waitIfPaused 在每个层边界检查暂停信号；暂停时阻塞到 Resume / Cancel。
func (e *Executor) waitIfPaused(ctx context.Context, execID string) error {
	v, ok := e.pauses.Load(execID)
	if !ok {
		return nil
	}
	ch := v.(chan struct{})
	select {
	case <-ch:
		return nil // 已恢复，继续下一层
	case <-ctx.Done():
		return ctx.Err() // 取消 → 终态由 abortExec 收尾
	}
}
func resolveRef(outputs map[string]map[string]any, ref string) (any, bool) {
	head := ref
	for i := 0; i < len(ref); i++ {
		if ref[i] == '.' {
			head = ref[:i]
			break
		}
	}
	nodeOut, ok := outputs[head]
	if !ok {
		return nil, false
	}
	tail := ref[len(head):]
	if tail == "" {
		return nodeOut, true
	}
	return digPath(nodeOut, tail[1:]) // 去掉点
}
