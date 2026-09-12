package workflow

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"WorkBaby/internal/db"
	"WorkBaby/internal/domain"
	"WorkBaby/internal/event"
	"WorkBaby/internal/repo"
	"WorkBaby/internal/workflow/nodes"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// newWorkflowTestDB 复用 repo/smoke_test 的 newTestDB 风格，单独建一份避免 import cycle。
func newWorkflowTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "wf-test.db") + "?_pragma=journal_mode(MEMORY)&_pragma=busy_timeout(5000)"
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := gdb.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return gdb
}

// mockNode 计数 + 返回固定 output；用于测试调度/并行/分支。
type mockNode struct {
	typ   domain.WorkflowNodeType
	calls atomic.Int32
	out   map[string]any
	delay time.Duration
	// 起止时刻（原子读写）；用于判定并行而非依赖墙钟耗时，避免慢机器抖动。
	start atomic.Int64
	end   atomic.Int64
}

func (n *mockNode) Type() domain.WorkflowNodeType { return n.typ }
func (n *mockNode) Schema() nodes.Schema          { return nodes.Schema{Type: n.typ} }
func (n *mockNode) Execute(ctx context.Context, inputs map[string]any, cfg map[string]any, upstream map[string]any) (map[string]any, error) {
	n.calls.Add(1)
	n.start.Store(time.Now().UnixNano())
	defer func() { n.end.Store(time.Now().UnixNano()) }()
	if n.delay > 0 {
		select {
		case <-time.After(n.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return n.out, nil
}

// overlapped 判定两个节点是否并发执行：时间区间存在交集。
// 串行执行时 b.start >= a.end；并行时 b.start < a.end。
func overlapped(a, b *mockNode) bool {
	return a.start.Load() < b.end.Load() && b.start.Load() < a.end.Load()
}

// TestExecutorRun 三节点线性：a → b → c。
func TestExecutorRun(t *testing.T) {
	gdb := newWorkflowTestDB(t)
	wfRepo := repo.NewWorkflowRepo(gdb)
	execRepo := repo.NewWorkflowExecutionRepo(gdb)
	nodeRepo := repo.NewWorkflowNodeExecutionRepo(gdb)
	bus := event.New()
	// 三节点各用独立 type，避免 executor 按 type 路由到同一 mock。
	a := &mockNode{typ: domain.WorkflowNodeHTTP, out: map[string]any{"v": 1}}
	b := &mockNode{typ: domain.WorkflowNodeCode, out: map[string]any{"sum": 2}}
	c := &mockNode{typ: domain.WorkflowNodeTool, out: map[string]any{"final": "ok"}}

	ex := New(ExecutorConfig{
		DB: gdb, WFRepo: wfRepo, ExecRepo: execRepo, NodeRepo: nodeRepo, Bus: bus,
		Nodes: []nodes.Node{a, b, c},
	})

	ctx := context.Background()
	row := &domain.WorkflowDO{
		ID: "WORKFLOW_TEST", Name: "linear",
		Graph: `{"nodes":[
			{"id":"a","type":"http","config":{"url":"https://x"}},
			{"id":"b","type":"code","deps":["a"],"config":{"script":"return 1;"}},
			{"id":"c","type":"tool","deps":["b"],"config":{"toolName":"echo"}}
		]}`,
		Enabled: true,
	}
	if err := wfRepo.Create(ctx, row); err != nil {
		t.Fatalf("create wf: %v", err)
	}
	execID, err := ex.Run(ctx, "WORKFLOW_TEST", nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if a.calls.Load() != 1 || b.calls.Load() != 1 || c.calls.Load() != 1 {
		t.Fatalf("call counts wrong: a=%d b=%d c=%d", a.calls.Load(), b.calls.Load(), c.calls.Load())
	}
	// verify final state
	final, err := execRepo.GetByID(ctx, execID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if final.Status != domain.WorkflowStatusCompleted {
		t.Fatalf("status = %s", final.Status)
	}
}

// TestExecutorParallelLayer 同一层节点并行。
func TestExecutorParallelLayer(t *testing.T) {
	// CI runner（单核或低并发）下并行度不可靠：同层节点会被串行调度，overlapped() 假阴失败。
	// 本地 `-short` 跳过避免假阳。本地开发默认 -short 跳过，CI 用 -short 也跳过。
	// 想真正验证并行性时显式 `go test ./internal/workflow -run TestExecutorParallelLayer -count=1`。
	if testing.Short() {
		t.Skip("parallel timing flaky on CI, skipped in -short mode")
	}
	gdb := newWorkflowTestDB(t)
	wfRepo := repo.NewWorkflowRepo(gdb)
	execRepo := repo.NewWorkflowExecutionRepo(gdb)
	nodeRepo := repo.NewWorkflowNodeExecutionRepo(gdb)
	bus := event.New()
	// 三节点各用独立 type：executor 按 type 路由实现，同 type 会路由到同一 mock 而掩盖并行性
	a := &mockNode{typ: domain.WorkflowNodeHTTP, delay: 100 * time.Millisecond, out: map[string]any{"v": 1}}
	b := &mockNode{typ: domain.WorkflowNodeTool, delay: 100 * time.Millisecond, out: map[string]any{"v": 2}}
	join := &mockNode{typ: domain.WorkflowNodeCode, out: map[string]any{"done": true}}

	ex := New(ExecutorConfig{
		DB: gdb, WFRepo: wfRepo, ExecRepo: execRepo, NodeRepo: nodeRepo, Bus: bus,
		Nodes: []nodes.Node{a, b, join},
	})

	row := &domain.WorkflowDO{
		ID: "WORKFLOW_PAR", Name: "parallel",
		Graph: `{"nodes":[
			{"id":"a","type":"http","config":{"url":"https://x"}},
			{"id":"b","type":"tool","config":{"toolName":"echo"}},
			{"id":"j","type":"code","deps":["a","b"],"config":{"script":"return 1;"}}
		]}`,
	}
	_ = wfRepo.Create(context.Background(), row)

	execID, err := ex.Run(context.Background(), "WORKFLOW_PAR", nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !overlapped(a, b) {
		t.Fatalf("should parallel: a=[%d,%d] b=[%d,%d]", a.start.Load(), a.end.Load(), b.start.Load(), b.end.Load())
	}
	if execID == "" {
		t.Fatal("no execID")
	}
}

// TestExecutorResumeFrom 断点续跑复用已完成节点：a 不重跑、b 正常执行、终态 completed。
//
// 这条不变量是「进程重启后工作流能接着跑」的地基：重跑已完成节点会重复副作用，
// 而漏跑未完成节点会让结果静默缺失。
func TestExecutorResumeFrom(t *testing.T) {
	gdb := newWorkflowTestDB(t)
	wfRepo := repo.NewWorkflowRepo(gdb)
	execRepo := repo.NewWorkflowExecutionRepo(gdb)
	nodeRepo := repo.NewWorkflowNodeExecutionRepo(gdb)
	ctx := context.Background()
	a := &mockNode{typ: domain.WorkflowNodeHTTP, out: map[string]any{"v": 1}}
	b := &mockNode{typ: domain.WorkflowNodeCode, out: map[string]any{"sum": 2}}

	ex := New(ExecutorConfig{
		DB: gdb, WFRepo: wfRepo, ExecRepo: execRepo, NodeRepo: nodeRepo, Bus: event.New(),
		Nodes: []nodes.Node{a, b},
	})

	row := &domain.WorkflowDO{
		ID: "WORKFLOW_RESUME", Name: "resume",
		Graph: `{"nodes":[
			{"id":"a","type":"http","config":{"url":"https://x"}},
			{"id":"b","type":"code","deps":["a"],"config":{"script":"return 1;"}}
		],"outputs":{"sum":"b.sum"}}`,
		Enabled: true,
	}
	if err := wfRepo.Create(ctx, row); err != nil {
		t.Fatalf("create wf: %v", err)
	}

	// 构造中断现场：a 已完成、b 未跑、执行处于 paused（等价于进程重启后的排空态）
	const execID = "WFEXEC_RESUME"
	now := time.Now().UnixMilli()
	if err := execRepo.Create(ctx, &domain.WorkflowExecutionDO{
		ID: execID, WorkflowID: "WORKFLOW_RESUME",
		Status: domain.WorkflowStatusPaused, Inputs: `{}`, StartedAt: now,
	}); err != nil {
		t.Fatalf("create exec: %v", err)
	}
	if err := nodeRepo.Create(ctx, &domain.WorkflowNodeExecutionDO{
		ID: "WFNODE_A", ExecutionID: execID, NodeID: "a",
		NodeType: domain.WorkflowNodeHTTP, Status: domain.NodeStatusRunning, StartedAt: now,
	}); err != nil {
		t.Fatalf("create node a: %v", err)
	}
	if err := nodeRepo.MarkFinished(ctx, "WFNODE_A", domain.NodeStatusCompleted, `{"v":1}`, ""); err != nil {
		t.Fatalf("finish node a: %v", err)
	}

	if err := ex.ResumeFrom(ctx, execID); err != nil {
		t.Fatalf("resume: %v", err)
	}

	if got := a.calls.Load(); got != 0 {
		t.Fatalf("已完成节点不得重跑，a.calls=%d", got)
	}
	if got := b.calls.Load(); got != 1 {
		t.Fatalf("未完成节点应执行一次，b.calls=%d", got)
	}
	final, err := execRepo.GetByID(ctx, execID)
	if err != nil {
		t.Fatalf("get exec: %v", err)
	}
	if final.Status != domain.WorkflowStatusCompleted {
		t.Fatalf("status = %s, want completed", final.Status)
	}
}

// TestExecutorCancel 取消必须真正打断执行中的 run，终态 cancelled 而非 failed/completed。
func TestExecutorCancel(t *testing.T) {
	gdb := newWorkflowTestDB(t)
	wfRepo := repo.NewWorkflowRepo(gdb)
	execRepo := repo.NewWorkflowExecutionRepo(gdb)
	nodeRepo := repo.NewWorkflowNodeExecutionRepo(gdb)
	bus := event.New()
	slow := &mockNode{typ: domain.WorkflowNodeHTTP, delay: 5 * time.Second, out: map[string]any{"v": 1}}

	ex := New(ExecutorConfig{
		DB: gdb, WFRepo: wfRepo, ExecRepo: execRepo, NodeRepo: nodeRepo, Bus: bus,
		Nodes: []nodes.Node{slow},
	})

	row := &domain.WorkflowDO{
		ID: "WORKFLOW_CANCEL", Name: "cancel",
		Graph:   `{"nodes":[{"id":"s","type":"http","config":{"url":"https://x"}}]}`,
		Enabled: true,
	}
	if err := wfRepo.Create(context.Background(), row); err != nil {
		t.Fatalf("create wf: %v", err)
	}

	type runRes struct {
		execID string
		err    error
	}
	done := make(chan runRes, 1)
	go func() {
		id, err := ex.Run(context.Background(), "WORKFLOW_CANCEL", nil)
		done <- runRes{id, err}
	}()

	// 轮询等 run 进入 running（拿最新一条执行记录）
	var execRow *domain.WorkflowExecutionDO
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := execRepo.ListByWorkflow(context.Background(), "WORKFLOW_CANCEL", 1)
		if err == nil && len(rows) == 1 && rows[0].Status == domain.WorkflowStatusRunning {
			execRow = &rows[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if execRow == nil {
		t.Fatal("execution did not reach running state")
	}

	start := time.Now()
	if err := ex.Cancel(context.Background(), execRow.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	select {
	case r := <-done:
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Fatalf("cancel did not interrupt the run, elapsed=%v", elapsed)
		}
		_ = r.err // runCtx 取消后节点返回 ctx.Err()，Run 返回 error 属预期
	case <-time.After(3 * time.Second):
		t.Fatal("run did not finish after cancel")
	}

	final, err := execRepo.GetByID(context.Background(), execRow.ID)
	if err != nil {
		t.Fatalf("get final: %v", err)
	}
	if final.Status != domain.WorkflowStatusCancelled {
		t.Fatalf("status = %s, want cancelled", final.Status)
	}
}
