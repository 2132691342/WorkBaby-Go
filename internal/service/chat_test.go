package service

import (
	"WorkBaby/internal/db"
	"WorkBaby/internal/domain"
	"WorkBaby/internal/event"
	"WorkBaby/internal/harness"
	"WorkBaby/internal/llm"
	"WorkBaby/internal/llm/registry"
	"WorkBaby/internal/memory"
	"WorkBaby/internal/repo"
	"WorkBaby/internal/skill"
	"WorkBaby/internal/tool"
	"context"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newChatOpsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "test.db") + "?_pragma=journal_mode(MEMORY)&_pragma=busy_timeout(5000)"
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Migrate(gdb))
	require.NoError(t, db.CreateFTS5(gdb))
	t.Cleanup(func() {
		sqlDB, err := gdb.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return gdb
}

func newChatOpsService(t *testing.T) (*ChatService, *repo.MessageRepo) {
	t.Helper()
	gdb := newChatOpsTestDB(t)
	sessRepo := repo.NewChatSessionRepo(gdb)
	msgRepo := repo.NewMessageRepo(gdb)
	setRepo := repo.NewSystemSettingRepo(gdb)
	mem := memory.NewService(msgRepo, repo.NewMemoryEpisodeRepo(gdb),
		repo.NewMemoryFactRepo(gdb), repo.NewMemoryProcedureRepo(gdb), t.TempDir())
	svc := NewChatService(sessRepo, msgRepo, repo.NewAiProviderRepo(gdb), setRepo, repo.NewTokenUsageRepo(gdb),
		event.New(), registry.New(), NewToolService(tool.NewRegistry(), setRepo), mem,
		NewSkillService(repo.NewSkillRepo(gdb), skill.NewRegistry()))
	return svc, msgRepo
}

func seedSession(t *testing.T, svc *ChatService, msgRepo *repo.MessageRepo, ctx context.Context) (*domain.ChatSessionRESP, int64) {
	t.Helper()
	ses, err := svc.CreateSession(ctx, &domain.ChatSessionREQ{Name: "ops"})
	require.NoError(t, err)
	for i := 1; i <= 5; i++ {
		require.NoError(t, msgRepo.Insert(ctx, &domain.MessageDO{
			ID: "M_" + itoa(i), SessionID: ses.ID, Seq: int64(i),
			Role: domain.MessageRoleUser, Content: "msg" + itoa(i), Status: domain.MessageStatusCompleted,
		}))
	}
	return ses, 5
}

// ===== 会话消息操作 =====


// TestForkSession 从指定消息分叉：新会话复制前段，原会话不变。
func TestForkSession(t *testing.T) {
	svc, msgRepo := newChatOpsService(t)
	ctx := context.Background()
	ses, _ := seedSession(t, svc, msgRepo, ctx)

	forked, err := svc.ForkSession(ctx, ses.ID, "M_3", "分叉会话")
	require.NoError(t, err)
	if forked.ID == ses.ID {
		t.Fatalf("forked session must have new id")
	}
	forkedRows, err := msgRepo.ListBySession(ctx, forked.ID, 0, 100)
	require.NoError(t, err)
	if len(forkedRows) != 3 || forkedRows[2].Content != "msg3" {
		t.Fatalf("want 3 copied messages, got %d last=%v", len(forkedRows), forkedRows[2].Content)
	}
	origRows, err := msgRepo.ListBySession(ctx, ses.ID, 0, 100)
	require.NoError(t, err)
	if len(origRows) != 5 {
		t.Fatalf("original session must keep 5 messages, got %d", len(origRows))
	}
}

// TestMemoryFactProcedural 语义/程序记忆写读删闭环（含同 key 覆盖）。
func TestMemoryFactProcedural(t *testing.T) {
	mem := memory.NewService(repo.NewMessageRepo(newChatOpsTestDB(t)), repo.NewMemoryEpisodeRepo(newChatOpsTestDB(t)),
		repo.NewMemoryFactRepo(newChatOpsTestDB(t)), repo.NewMemoryProcedureRepo(newChatOpsTestDB(t)), t.TempDir())
	svc := NewMemoryService(mem)
	ctx := context.Background()

	// 语义：写入 → 列出 → 覆盖 → 删除
	fact, err := svc.WriteFact(ctx, "user", "preference", "喜欢简洁回答", 0.7)
	require.NoError(t, err)
	require.NotEmpty(t, fact.ID)
	facts, err := svc.ListFacts(ctx, 50)
	require.NoError(t, err)
	if len(facts) != 1 || facts[0].Key != "preference" {
		t.Fatalf("want 1 fact, got %d", len(facts))
	}
	// 同 subject+key 覆盖，不新增
	_, err = svc.WriteFact(ctx, "user", "preference", "喜欢简洁且带示例", 0.8)
	require.NoError(t, err)
	facts, _ = svc.ListFacts(ctx, 50)
	if len(facts) != 1 || facts[0].Value != "喜欢简洁且带示例" {
		t.Fatalf("fact should be upserted, got %+v", facts)
	}
	require.NoError(t, svc.DeleteFact(ctx, facts[0].ID))
	if facts, _ = svc.ListFacts(ctx, 50); len(facts) != 0 {
		t.Fatalf("want 0 facts after delete")
	}

	// 程序：写入 → 列出 → 删除
	proc, err := svc.WriteProcedure(ctx, "tool-flow-websearch", []string{"websearch", "webfetch", "file_write"})
	require.NoError(t, err)
	procs, err := svc.ListProcedures(ctx, 50)
	require.NoError(t, err)
	if len(procs) != 1 || len(procs[0].Steps) != 3 {
		t.Fatalf("want 1 procedure with 3 steps, got %+v", procs)
	}
	require.NoError(t, svc.DeleteProcedure(ctx, proc.ID))
	if procs, _ = svc.ListProcedures(ctx, 50); len(procs) != 0 {
		t.Fatalf("want 0 procedures after delete")
	}
}

// ===== 审批 =====

// TestApprovalApproveAndDecide 审批全链路 + 已批准免审。
func TestApprovalApproveAndDecide(t *testing.T) {
	bus := event.New()
	svc := NewApprovalService(bus)

	var gotID string
	var mu sync.Mutex
	done := make(chan struct{})
	bus.Subscribe(event.MatchPrefix("chat:"), func(name string, payload any) {
		if name != "chat:approval" {
			return
		}
		m, _ := payload.(map[string]any)
		mu.Lock()
		gotID, _ = m["id"].(string)
		mu.Unlock()
		close(done)
	})

	ctx := harness.WithRunContext(context.Background(), "run-1", "ses-1")
	result := make(chan bool, 1)
	go func() { result <- svc.Approve(ctx, "go build ./...", tool.RiskApprovalNeeds) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("approval event not published")
	}

	mu.Lock()
	id := gotID
	mu.Unlock()
	if err := svc.Decide(id, true); err != nil {
		t.Fatalf("decide failed: %v", err)
	}
	select {
	case ok := <-result:
		if !ok {
			t.Fatalf("approved decision should return true")
		}
	case <-time.After(time.Second):
		t.Fatalf("Approve did not return")
	}

	// 已批准命令免审：第二次调用立即 true，不再发事件
	events := 0
	bus.Subscribe(event.MatchExact("chat:approval"), func(string, any) { events++ })
	if !svc.Approve(ctx, "go build ./...", tool.RiskApprovalNeeds) {
		t.Fatalf("previously approved command should pass without approval")
	}
	if events != 0 {
		t.Fatalf("exempt command should not publish approval event")
	}
}


func itoa(n int) string {
	return string(rune('0' + n))
}






// TestQueueSteerPersistsAndQueues 有活动 run 时：消息立即落库（前端可见）+ 进入注入队列。
func TestQueueSteerPersistsAndQueues(t *testing.T) {
	svc, msgRepo := newChatOpsService(t)
	ctx := context.Background()
	ses, err := svc.CreateSession(ctx, &domain.ChatSessionREQ{Name: "steer2"})
	require.NoError(t, err)

	// 伪造活动 run（真实 run 由 SendStream 注册；此处只验证注入侧契约）
	svc.runs.set(ses.ID, "RUN_STEER", func() {})

	res, err := svc.QueueSteer(ctx, ses.ID, "改成用 Go 写")
	require.NoError(t, err)
	assert.Equal(t, "RUN_STEER", res.RunID)
	assert.True(t, res.Queued)

	queued := svc.steers.drain(ses.ID)
	require.Len(t, queued, 1)
	assert.Equal(t, "改成用 Go 写", queued[0].Content)

	rows, err := msgRepo.ListBySession(ctx, ses.ID, 0, 0)
	require.NoError(t, err)
	require.Len(t, rows, 1, "注入消息应立即落库")
	assert.Equal(t, domain.MessageRoleUser, rows[0].Role)
	assert.Equal(t, "RUN_STEER", rows[0].RunID)
	assert.Equal(t, domain.MessageStatusCompleted, rows[0].Status)
}


type fakeRunner struct {
	calls     atomic.Int32
	result    *AgentRunOutcome
	err       error
	startedAt chan struct{}
	hold      time.Duration
	holdCh    chan struct{}
}

func newFakeRunner(out *AgentRunOutcome, err error) *fakeRunner {
	return &fakeRunner{
		result:    out,
		err:       err,
		startedAt: make(chan struct{}, 1),
		holdCh:    make(chan struct{}),
	}
}

func (f *fakeRunner) RunAgent(ctx context.Context, sessionID, userInput, agent string) (*AgentRunOutcome, error) {
	f.calls.Add(1)
	select {
	case f.startedAt <- struct{}{}:
	default:
	}
	if f.hold > 0 {
		// 非取消测试用：到达 hold 时间后自动完成。
		select {
		case <-f.holdCh:
		case <-time.After(f.hold):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	} else {
		// 取消测试用：阻塞直到 holdCh 关闭或 ctx 取消。
		// 关键：ctx 取消时返回 nil error，让 runOne 走 ctx 分支标 cancelled
		// （而不是 err 分支标 failed）。
		select {
		case <-f.holdCh:
		case <-ctx.Done():
			return &AgentRunOutcome{RunID: "cancelled"}, nil
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.result == nil {
		return &AgentRunOutcome{Content: "ok", RunID: "fake"}, nil
	}
	return f.result, nil
}

// newTaskServiceWithFake 装配 TaskService + 单线程 worker（降低测试等待）。
// workers 默认 2；这里压到 1 让测试可观察顺序。
func newTaskServiceWithFake(t *testing.T, runner agentRunner, workers int) *TaskService {
	t.Helper()
	svc := NewTaskService(runner, event.New())
	if workers > 0 {
		// 替换默认 channel 启动 worker 数量 = workers。
		svc.queue = make(chan *domain.TaskDO, taskQueueSize)
	}
	for i := 0; i < workers; i++ {
		go svc.loop()
	}
	t.Cleanup(func() { svc.Stop() })
	return svc
}

// TestTaskSubmitAndComplete 提交任务 → worker 执行 → 终态 completed。
func TestTaskSubmitAndComplete(t *testing.T) {
	fake := newFakeRunner(&AgentRunOutcome{Content: "done", RunID: "RUN_FAKE"}, nil)
	fake.hold = 50 * time.Millisecond
	svc := newTaskServiceWithFake(t, fake, 1)

	tk, err := svc.Submit(domain.TaskSubmitREQ{SessionID: "SES_1", Agent: "default", Prompt: "do"})
	require.NoError(t, err)
	require.NotEmpty(t, tk.ID)

	// 等任务跑完（轮询限 5 秒）
	require.Eventually(t, func() bool {
		got, err := svc.Get(tk.ID)
		return err == nil && got.State == domain.TaskCompleted
	}, 5*time.Second, 20*time.Millisecond, "任务应进入 completed 终态")

	got, _ := svc.Get(tk.ID)
	assert.Equal(t, "done", got.Result)
	assert.Equal(t, "RUN_FAKE", got.RunID)
	assert.False(t, got.StartedAt == 0, "StartedAt 应被记录")
	assert.False(t, got.FinishedAt == 0, "FinishedAt 应被记录")
}

// TestTaskCancelRunning 取消运行中的任务 → worker ctx 取消 → 任务转 cancelled。
func TestTaskCancelRunning(t *testing.T) {
	fake := newFakeRunner(nil, nil) // hold=0 → 阻塞直到 holdCh 关闭或 ctx 取消
	svc := newTaskServiceWithFake(t, fake, 1)

	tk, err := svc.Submit(domain.TaskSubmitREQ{SessionID: "SES_2", Agent: "default", Prompt: "long"})
	require.NoError(t, err)

	// 等 worker 真正开始（fake 在收到 started 信号后才阻塞）
	select {
	case <-fake.startedAt:
	case <-time.After(2 * time.Second):
		t.Fatal("worker 未启动")
	}

	// 取消：worker 通过 ctx 收到信号后返回 ctx.Err，TaskService 标 cancelled
	require.NoError(t, svc.Cancel(tk.ID))

	require.Eventually(t, func() bool {
		got, err := svc.Get(tk.ID)
		return err == nil && got.State == domain.TaskCancelled
	}, 3*time.Second, 20*time.Millisecond, "长任务取消后应转 cancelled")
}




// 编译期断言 harness 包仍能 import（避免误删导致 task_test 编译仍过但行为退化）
var _ = harness.WithRunContext
var _ = llm.RoleUser
var _ = context.Background
