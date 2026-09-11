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
