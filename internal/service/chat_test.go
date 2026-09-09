package service

// 聊天服务长链路测试：会话操作 / 审批 / 插话队列 / 后台任务 / 工作区绑定 / 消息清洗。
// 场景实现为私有函数（testXxx），由文件末尾 6 个按能力域划分的父测试以 t.Run 聚合。

import (
	"WorkBaby/internal/db"
	"WorkBaby/internal/domain"
	"WorkBaby/internal/event"
	"WorkBaby/internal/harness"
	"WorkBaby/internal/llm"
	"WorkBaby/internal/llm/registry"
	"WorkBaby/internal/memory"
	"WorkBaby/internal/repo"
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
	mem := memory.NewService(repo.NewMemoryEpisodeRepo(gdb),
		repo.NewMemoryFactRepo(gdb), repo.NewMemoryProcedureRepo(gdb), t.TempDir())
	svc := NewChatService(sessRepo, msgRepo, repo.NewAiProviderRepo(gdb), setRepo, repo.NewTokenUsageRepo(gdb),
		event.New(), registry.New(), NewToolService(tool.NewRegistry(), setRepo), mem)
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
func testForkSession(t *testing.T) {
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

// ===== 审批 =====

// TestApprovalApproveAndDecide 审批全链路 + 已批准免审。
func testApprovalApproveAndDecide(t *testing.T) {
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
func testQueueSteerPersistsAndQueues(t *testing.T) {
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
func testTaskSubmitAndComplete(t *testing.T) {
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
func testTaskCancelRunning(t *testing.T) {
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

// ===== 工作区绑定 =====

// TestWorkspaceBindLifecycle 工作区绑定闭环：
// 创建即绑定（回归：CreateSession 曾丢弃 workspace_path）→ 解析优先绑定目录 →
// 未绑定回落默认根 → 不存在的目录被拒绝。
func testWorkspaceBindLifecycle(t *testing.T) {
	svc, _ := newChatOpsService(t)
	ctx := context.Background()

	// 创建即绑定：路径规范化（正斜杠→系统分隔符）并落库
	dir := t.TempDir()
	ses, err := svc.CreateSession(ctx, &domain.ChatSessionREQ{Name: "ws", WorkspacePath: filepath.ToSlash(dir)})
	require.NoError(t, err)
	assert.Equal(t, filepath.Clean(dir), ses.WorkspacePath, "创建时应绑定并规范化工作目录")

	// WorkspaceRoot：绑定会话返回绑定目录；未绑定会话回落默认根
	assert.Equal(t, filepath.Clean(dir), svc.WorkspaceRoot(ctx, ses.ID, "default-root"))
	other, err := svc.CreateSession(ctx, &domain.ChatSessionREQ{Name: "no-ws"})
	require.NoError(t, err)
	assert.Equal(t, "default-root", svc.WorkspaceRoot(ctx, other.ID, "default-root"))

	// 不存在的目录：创建与更新都要拒绝
	_, err = svc.CreateSession(ctx, &domain.ChatSessionREQ{Name: "bad", WorkspacePath: filepath.ToSlash(filepath.Join(dir, "missing"))})
	require.Error(t, err)
	_, err = svc.UpdateWorkspace(ctx, ses.ID, filepath.ToSlash(filepath.Join(dir, "missing")))
	require.Error(t, err)

	// 解绑：空路径清空绑定，回落默认根
	updated, err := svc.UpdateWorkspace(ctx, ses.ID, "")
	require.NoError(t, err)
	assert.Empty(t, updated.WorkspacePath)
	assert.Equal(t, "default-root", svc.WorkspaceRoot(ctx, ses.ID, "default-root"))
}

// TestCompactSessionArchive 复现「摘要+归档」压缩语义：
//
// <p>早期轮次整体标 archived 剔出 LLM 上下文（正文不删改）；归档边界不得切进
// assistant(tool_calls) 与其 tool 结果之间；归档摘要写会话元数据供 buildSystem 注入。
func testCompactSessionArchive(t *testing.T) {
	svc, msgRepo := newChatOpsService(t)
	ctx := context.Background()
	ses, _ := seedSession(t, svc, msgRepo, ctx)
	// 追加工具段 + 收尾轮：M_6 用户 / M_7 assistant(tool_calls) / M_8 tool / M_9 assistant / M_10 用户
	seed := []domain.MessageDO{
		{ID: "M_6", SessionID: ses.ID, Seq: 6, Role: domain.MessageRoleUser, Content: "再来一步", Status: domain.MessageStatusCompleted},
		{ID: "M_7", SessionID: ses.ID, Seq: 7, Role: domain.MessageRoleAssistant,
			ToolCalls: `[{"id":"CALL_X","type":"function","function":{"name":"exec","arguments":"{}"}}]`,
			Status:    domain.MessageStatusCompleted},
		{ID: "M_8", SessionID: ses.ID, Seq: 8, Role: domain.MessageRoleTool, ToolCallID: "CALL_X", Content: "ok", Status: domain.MessageStatusCompleted},
		{ID: "M_9", SessionID: ses.ID, Seq: 9, Role: domain.MessageRoleAssistant, Content: "搞定", Status: domain.MessageStatusCompleted},
		{ID: "M_10", SessionID: ses.ID, Seq: 10, Role: domain.MessageRoleUser, Content: "收尾", Status: domain.MessageStatusCompleted},
	}
	for i := range seed {
		require.NoError(t, msgRepo.Insert(ctx, &seed[i]))
	}

	res, err := svc.CompactSession(ctx, ses.ID, domain.CompactREQ{KeepRecent: 3})
	require.NoError(t, err)
	require.Zero(t, res.Failed)

	rows, err := msgRepo.ListBySession(ctx, ses.ID, 0, 0)
	require.NoError(t, err)
	require.Len(t, rows, 10, "归档不删消息，UI 仍可回看")

	// 边界回退：cut=7 落在 tool 消息上 → 回退到 M_7（assistant），M_1..M_6 归档
	archived := map[string]bool{}
	for _, r := range rows {
		archived[r.ID] = r.Status == domain.MessageStatusArchived
	}
	for _, id := range []string{"M_1", "M_2", "M_3", "M_4", "M_5", "M_6"} {
		assert.True(t, archived[id], "%s 应已归档", id)
	}
	for _, id := range []string{"M_7", "M_8", "M_9", "M_10"} {
		assert.False(t, archived[id], "%s 应保留在上下文", id)
	}

	// 归档后上下文不含归档行；保留侧 assistant(tool_calls)+tool 配对完整
	out, err := svc.toLLMMessages(rows)
	require.NoError(t, err)
	require.Len(t, out, 4)
	keptRoles := map[llm.RoleType]int{}
	for _, m := range out {
		keptRoles[m.Role]++
		if m.Role == llm.RoleTool {
			require.Equal(t, "CALL_X", m.ToolCallID, "保留侧 tool 必须与保留侧 assistant 配对")
		}
	}
	assert.Equal(t, 1, keptRoles[llm.RoleUser])
	assert.Equal(t, 2, keptRoles[llm.RoleAssistant])

	// 归档摘要落会话元数据（buildSystem 每轮注入 system）
	fresh, err := svc.sessions.GetByID(ctx, ses.ID)
	require.NoError(t, err)
	sum := archiveSummary(fresh)
	require.NotEmpty(t, sum)
	assert.Contains(t, sum, "用户：msg1")
	assert.NotContains(t, sum, "收尾", "保留侧轮次不应出现在归档摘要里")
}

// TestToLLMMessagesDropsOrphanTools 续跑/压缩后孤儿 tool 消息不会把整轮送进 LLM。
//
// <p>回归：toLLMMessages 历史上会把所有 role=tool 一并发回模型；若某条 tool 消息
// 的 tool_call_id 在全列表里没有任何 assistant.tool_calls 匹配，上游 LLM 会以
// 400 「tool result's tool id not found」拒绝整轮。修复后这些孤儿被静默剥掉。
// TestToLLMMessagesDropsEmptyAssistant 复现 GLM 400（上游码 1214「messages 参数非法」）：
//
// <p>上次 run 失败/中断后落库的 assistant 占位（content 为空、无 tool_calls）若原样
// 回发上游，GLM 等厂商直接以 400 拒绝整轮；且消息已持久化，会话被永久毒化。
// 修复后空 assistant 消息被静默剥掉；空 content 的 tool 消息兜底为 "(empty)"。
func testToLLMMessagesDropsEmptyAssistant(t *testing.T) {
	svc, _ := newChatOpsService(t)
	toolCallsJSON := `[{"id":"CALL_1","type":"function","function":{"name":"exec","arguments":"{}"}}]`

	hists := []domain.MessageDO{
		{ID: "M1", Role: domain.MessageRoleUser, Content: "开工", Status: domain.MessageStatusCompleted},
		// 上次 run 失败残留的空 assistant 占位（部分 provider 会因空 content 拒绝整轮）
		{ID: "M2", Role: domain.MessageRoleAssistant, Content: "", Status: domain.MessageStatusFailed},
		{ID: "M3", Role: domain.MessageRoleUser, Content: "继续", Status: domain.MessageStatusCompleted},
		// 空 content 的 tool 消息：兜底 "(empty)" 而非剥掉（剥掉会破坏 tool_call 配对）
		{ID: "M4", Role: domain.MessageRoleAssistant, Content: "", ToolCalls: toolCallsJSON, Status: domain.MessageStatusCompleted},
		{ID: "M5", Role: domain.MessageRoleTool, ToolCallID: "CALL_1", Content: "", Status: domain.MessageStatusCompleted},
	}
	out, err := svc.toLLMMessages(hists)
	require.NoError(t, err)
	require.Len(t, out, 4, "空 assistant 占位应被剥掉，其余保留")

	for _, m := range out {
		if m.Role == llm.RoleAssistant && len(m.ToolCalls) == 0 {
			require.NotEmpty(t, m.Content, "无 tool_calls 的 assistant 不允许空 content（GLM 1214）")
		}
		if m.Role == llm.RoleTool {
			require.Equal(t, "(empty)", m.Content, "空 tool 结果应兜底为 (empty)")
		}
	}
}

func testToLLMMessagesDropsOrphanTools(t *testing.T) {
	svc, _ := newChatOpsService(t)
	toolCallsJSON := `[{"id":"CALL_REAL","type":"function","function":{"name":"exec","arguments":"{}"}}]`

	hists := []domain.MessageDO{
		{ID: "M1", Role: domain.MessageRoleUser, Content: "开工", Status: domain.MessageStatusCompleted},
		{ID: "M2", Role: domain.MessageRoleAssistant, Content: "", ToolCalls: toolCallsJSON, Status: domain.MessageStatusCompleted},
		{ID: "M3", Role: domain.MessageRoleTool, ToolCallID: "CALL_REAL", Content: "OK", Status: domain.MessageStatusCompleted},
		// 孤儿：tool_call_id 在上下文中没有匹配的 assistant tool_call（压缩或续跑产生）
		{ID: "M4", Role: domain.MessageRoleTool, ToolCallID: "CALL_GHOST", Content: "stale", Status: domain.MessageStatusCompleted},
		// 防御：tool_call_id 为空的 tool 消息也应被剥掉
		{ID: "M5", Role: domain.MessageRoleTool, ToolCallID: "", Content: "?", Status: domain.MessageStatusCompleted},
		{ID: "M6", Role: domain.MessageRoleUser, Content: "继续", Status: domain.MessageStatusCompleted},
		{ID: "M7", Role: domain.MessageRoleAssistant, Content: "完成", Status: domain.MessageStatusCompleted},
	}
	out, err := svc.toLLMMessages(hists)
	require.NoError(t, err)
	require.Len(t, out, 5, "应剥掉 2 条孤儿 tool 消息，剩 user/asst/tool/user/asst")

	for _, m := range out {
		if m.Role == llm.RoleTool {
			require.NotEmpty(t, m.ToolCallID, "剥除后剩余的 tool 消息必须带有效 tool_call_id")
		}
	}
	// 找到唯一一条幸存 tool 消息，断言它就是 CALL_REAL 那条
	var toolCount int
	for _, m := range out {
		if m.Role == llm.RoleTool {
			toolCount++
			require.Equal(t, "CALL_REAL", m.ToolCallID)
		}
	}
	require.Equal(t, 1, toolCount)
}

// ===== 聚合入口 =====
//
// 场景实现为上面的私有函数（不被 go test 直接发现），由下列 6 个按能力域划分的
// 父测试以 t.Run 聚合；改某个能力只需跑对应的一个父测试。

// TestChatSessionOps 会话操作：分叉 / 压缩归档。
func TestChatSessionOps(t *testing.T) {
	t.Run("fork", testForkSession)
	t.Run("compact_archive", testCompactSessionArchive)
}

// TestChatApproval 危险命令审批：判定回执与决策落库。
func TestChatApproval(t *testing.T) {
	t.Run("approve_and_decide", testApprovalApproveAndDecide)
}

// TestChatSteerQueue 中途插话：steer 持久化并入队。
func TestChatSteerQueue(t *testing.T) {
	t.Run("persists_and_queues", testQueueSteerPersistsAndQueues)
}

// TestChatBackgroundTasks 后台任务：提交完成 / 运行中取消。
func TestChatBackgroundTasks(t *testing.T) {
	t.Run("submit_and_complete", testTaskSubmitAndComplete)
	t.Run("cancel_running", testTaskCancelRunning)
}

// TestChatWorkspaceBind 工作区绑定生命周期。
func TestChatWorkspaceBind(t *testing.T) {
	t.Run("lifecycle", testWorkspaceBindLifecycle)
}

// TestChatLLMMessageHygiene 回发上游前的消息清洗（协议硬约束：
// 空 assistant 与孤儿 tool 消息都会让上游 400 拒绝整轮）。
func TestChatLLMMessageHygiene(t *testing.T) {
	t.Run("drops_empty_assistant", testToLLMMessagesDropsEmptyAssistant)
	t.Run("drops_orphan_tools", testToLLMMessagesDropsOrphanTools)
}
