package service

import (
	"testing"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/repo"

	"github.com/stretchr/testify/require"
)

// TestSideConversationLifecycle 辅助会话：ensure 幂等 / 不进侧栏列表 / 随主会话配置。
func TestSideConversationLifecycle(t *testing.T) {
	svc, _ := newChatOpsService(t)
	ctx := t.Context()

	ses, _ := seedSession(t, svc, sideMsgRepo(t, svc), ctx)

	side, err := svc.EnsureSideConversation(ctx, ses.ID)
	require.NoError(t, err)
	require.Equal(t, domain.SessionKindSide, side.Kind)
	require.Equal(t, ses.ID, side.ParentID)
	require.True(t, len(side.Name) > 0)

	// 幂等：再次 ensure 返回同一条
	again, err := svc.EnsureSideConversation(ctx, ses.ID)
	require.NoError(t, err)
	require.Equal(t, side.ID, again.ID)

	// 侧栏列表不含辅助会话
	list, err := svc.ListSessions(ctx, 1, 50)
	require.NoError(t, err)
	for _, it := range list.Items {
		require.NotEqual(t, side.ID, it.ID)
	}
	require.True(t, list.Total >= 1)

	// get 已有
	got, err := svc.GetSideConversation(ctx, ses.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, side.ID, got.ID)
}

// TestSideParentMessages 主会话历史前缀：有界截取 + user 轮次对齐 + system 来源标注。
func TestSideParentMessages(t *testing.T) {
	svc, msgRepo := newChatOpsService(t)
	ctx := t.Context()

	ses, err := svc.CreateSession(ctx, &domain.ChatSessionREQ{Name: "main-long"})
	require.NoError(t, err)
	// 20 条 × ~900 rune：超过 12000 rune 预算，前缀必须截断
	for i := 1; i <= 20; i++ {
		role := domain.MessageRoleUser
		if i%2 == 0 {
			role = domain.MessageRoleAssistant
		}
		content := make([]rune, 900)
		for j := range content {
			content[j] = '字'
		}
		require.NoError(t, msgRepo.Insert(ctx, &domain.MessageDO{
			ID: "M_SIDE_" + itoa(i), SessionID: ses.ID, Seq: int64(i),
			Role: role, Content: string(content), Status: domain.MessageStatusCompleted,
		}))
	}
	row, err := svc.sessions.GetByID(ctx, ses.ID)
	require.NoError(t, err)

	side, err := svc.EnsureSideConversation(ctx, ses.ID)
	require.NoError(t, err)
	sideRow, err := svc.sessions.GetByID(ctx, side.ID)
	require.NoError(t, err)

	msgs, err := svc.sideParentMessages(ctx, sideRow, false)
	require.NoError(t, err)
	require.NotNil(t, msgs)
	// 首条是来源标注 system
	require.Equal(t, "system", string(msgs[0].Role))
	// 预算截断：总 rune 数不应远超预算（header + 界内消息）
	used := 0
	for _, m := range msgs[1:] {
		used += len([]rune(m.Content))
	}
	require.LessOrEqual(t, used, sideParentHistoryRunes+2000)
	// 有界截断确实发生：20 条 × 900 rune 未全量带入
	require.Less(t, len(msgs)-1, 20)
	// 起点对齐 user 轮次
	require.Equal(t, "user", string(msgs[1].Role))

	// 普通会话（非 side）没有前缀
	normalMsgs, err := svc.sideParentMessages(ctx, row, false)
	require.NoError(t, err)
	require.Nil(t, normalMsgs)
}

// sideMsgRepo 测试种子数据用的消息仓储。
func sideMsgRepo(t *testing.T, svc *ChatService) *repo.MessageRepo {
	t.Helper()
	return svc.messages
}
