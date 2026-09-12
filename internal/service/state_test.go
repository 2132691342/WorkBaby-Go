package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/repo"
)

// TestSessionTodoPersistence 会话计划跨重启可回放、勾选落库、重列为覆盖语义。
// 每次断言都用新 store 实例读回（等价于进程内无缓存）。
func TestSessionTodoPersistence(t *testing.T) {
	gdb := newChatOpsTestDB(t)
	ctx := context.Background()
	const sid = "SESSION_TODO"
	newStore := func() *SessionTodoStore { return NewSessionTodoStore(repo.NewSessionTodoRepo(gdb)) }

	st := newStore()
	require.NoError(t, st.Save(ctx, sid, []domain.TodoItem{
		{ID: "TODO_1", Title: "读代码"},
		{ID: "TODO_2", Title: "改实现"},
	}))

	got, err := newStore().Load(ctx, sid)
	require.NoError(t, err)
	require.Len(t, got, 2, "重启后必须读回完整计划")
	assert.Equal(t, "读代码", got[0].Title)
	assert.Equal(t, "TODO_2", got[1].ID, "展示顺序按写入顺序稳定")

	state, err := newStore().Toggle(ctx, sid, "TODO_1")
	require.NoError(t, err)
	assert.Equal(t, 1, state.DoneCount)

	after, err := newStore().Load(ctx, sid)
	require.NoError(t, err)
	assert.True(t, after[0].Done, "勾选状态必须落库")

	_, err = newStore().Toggle(ctx, sid, "TODO_MISSING")
	assert.ErrorIs(t, err, domain.ErrTodoItemNotFound)

	require.NoError(t, st.Save(ctx, sid, []domain.TodoItem{{ID: "TODO_9", Title: "新计划"}}))
	replaced, err := newStore().Load(ctx, sid)
	require.NoError(t, err)
	require.Len(t, replaced, 1, "重列计划是覆盖语义")
}

// TestSessionVarThreeScopes 三层 State：user 跨会话可见、session 仅本会话、temp 不落库且可按 run 清理；
// 删除按作用域路由（删 user 级不影响会话级同名 key）。
func TestSessionVarThreeScopes(t *testing.T) {
	ctx := context.Background()
	svc := NewSessionVarService(repo.NewSessionVariableRepo(newChatOpsTestDB(t)))

	_, err := svc.Set(ctx, "S1", domain.SessionVarScopeSession, "task", "写文档")
	require.NoError(t, err)
	_, err = svc.Set(ctx, "S1", domain.SessionVarScopeUser, "lang", "zh")
	require.NoError(t, err)

	// 用户级跨会话可见：S2 只应看到 user 级
	items, err := svc.List(ctx, "S2")
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, domain.SessionVarScopeUser, items[0].Scope)

	// S1 同时看到两级
	items, err = svc.List(ctx, "S1")
	require.NoError(t, err)
	require.Len(t, items, 2)

	// temp 只在内存，可整体清理；且不能经持久化路径写入
	items, err = svc.SetTemp("RUN1", "draft", "草稿")
	require.NoError(t, err)
	require.Equal(t, domain.SessionVarScopeTemp, items[0].Scope)
	require.Len(t, svc.ListTemp("RUN1"), 1)
	svc.ClearTemp("RUN1")
	require.Empty(t, svc.ListTemp("RUN1"))
	_, err = svc.Set(ctx, "S1", domain.SessionVarScopeTemp, "k", "v")
	require.Error(t, err)

	// 删除按作用域路由：user 级有同名 key 时，删 user 级不影响会话级
	_, err = svc.Set(ctx, "S1", domain.SessionVarScopeUser, "task", "user 级同名")
	require.NoError(t, err)
	_, err = svc.Delete(ctx, "S1", domain.SessionVarScopeUser, "task")
	require.NoError(t, err)
	items, err = svc.List(ctx, "S1")
	require.NoError(t, err)
	require.Len(t, items, 2, "剩下的应是会话级 task 与用户级 lang")
	for _, it := range items {
		if it.Key == "task" {
			require.Equal(t, domain.SessionVarScopeSession, it.Scope)
			require.Equal(t, "写文档", it.Value)
		}
	}
	_, err = svc.Delete(ctx, "S1", domain.SessionVarScopeUser, "task")
	require.ErrorIs(t, err, domain.ErrSessionVarNotFound)

	// 删用户级 lang：S2 也随之清空（用户级是全局的，任何会话都能删）
	_, err = svc.Delete(ctx, "S1", domain.SessionVarScopeUser, "lang")
	require.NoError(t, err)
	items, err = svc.List(ctx, "S2")
	require.NoError(t, err)
	require.Empty(t, items)
}

// TestInboxReviewFlow 收件箱主链路：提交去重 → 待审列表（摘要由载荷派生）→ 合入 → 重复合入拒绝 → 忽略；
// 未注册 applier 的类型必须显式失败而非静默成功。
func TestInboxReviewFlow(t *testing.T) {
	ctx := context.Background()
	svc := NewInboxService(repo.NewInboxRepo(newChatOpsTestDB(t)))

	var applied []string
	svc.WithApplier(domain.InboxKindFact, func(_ context.Context, it domain.InboxItemDO) error {
		applied = append(applied, it.Title)
		return nil
	})

	fact := domain.InboxCandidate{
		Kind: domain.InboxKindFact, Title: "user:preference", Source: domain.InboxSourceLLM, Confidence: 0.8,
		Payload: domain.InboxFactPayload{Subject: "user", Key: "preference", Value: "深色主题", Confidence: 0.8},
	}
	n, err := svc.Submit(ctx, "S1", "R1", []domain.InboxCandidate{fact, fact})
	require.NoError(t, err)
	require.Equal(t, 1, n, "同 kind + 同 title 的重复候选只入箱一条")

	items, err := svc.List(ctx, "pending", 10)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "preference = 深色主题", items[0].Summary, "摘要由载荷派生")

	resp, err := svc.Approve(ctx, items[0].ID)
	require.NoError(t, err)
	require.Equal(t, domain.InboxStatusApproved, resp.Status)
	require.Equal(t, []string{"user:preference"}, applied)
	_, err = svc.Approve(ctx, items[0].ID)
	require.ErrorIs(t, err, domain.ErrInboxNotPending)

	// 未注册 applier：显式失败
	_, err = svc.Submit(ctx, "S1", "R2", []domain.InboxCandidate{{Kind: domain.InboxKindSkill, Title: "no-applier"}})
	require.NoError(t, err)
	rows, err := svc.List(ctx, "pending", 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	_, err = svc.Approve(ctx, rows[0].ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "暂不支持合入")

	// 忽略路径
	require.NoError(t, svc.Reject(ctx, rows[0].ID))
	stats, err := svc.Stats(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), stats.Approved)
	require.Equal(t, int64(1), stats.Rejected)
	require.Equal(t, int64(0), stats.Pending)
}
