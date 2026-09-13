package service

import (
	"context"
	"testing"

	"WorkBaby/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGoalLifecycle 目标模式状态机：set → pause → resume → clear 全链路，
// 目标持久化在会话元数据（重启后 readSessionMeta 仍可恢复）。
func TestGoalLifecycle(t *testing.T) {
	svc, _ := newChatOpsService(t)
	ctx := context.Background()

	sesResp, err := svc.CreateSession(ctx, &domain.ChatSessionREQ{Name: "goal"})
	require.NoError(t, err)
	sesID := sesResp.ID

	// 无参数 set 被拒：目标描述不能为空
	_, err = svc.SetGoal(ctx, sesID, domain.GoalREQ{Action: "set"})
	require.Error(t, err)

	// set → active（Round 从 0 起）
	g1, err := svc.SetGoal(ctx, sesID, domain.GoalREQ{Action: "set", Text: "修复所有 TS 编译错误并保持测试通过"})
	require.NoError(t, err)
	require.NotNil(t, g1.Goal)
	assert.Equal(t, domain.GoalStatusActive, g1.Goal.Status)
	assert.Equal(t, 0, g1.Goal.Round)
	assert.Equal(t, domain.GoalDefaultMaxRounds, g1.Goal.MaxRounds)

	// 权威拉取（模拟重启 / 切会话）
	got, err := svc.Goal(ctx, sesID)
	require.NoError(t, err)
	require.NotNil(t, got.Goal)
	assert.Equal(t, "修复所有 TS 编译错误并保持测试通过", got.Goal.Text)

	// set 已有活动目标 = replace，Round 保留
	g2, err := svc.SetGoal(ctx, sesID, domain.GoalREQ{Action: "set", Text: "新目标"})
	require.NoError(t, err)
	assert.Equal(t, "新目标", g2.Goal.Text)

	// pause / resume
	gp, err := svc.SetGoal(ctx, sesID, domain.GoalREQ{Action: "pause"})
	require.NoError(t, err)
	assert.Equal(t, domain.GoalStatusPaused, gp.Goal.Status)
	gr, err := svc.SetGoal(ctx, sesID, domain.GoalREQ{Action: "resume"})
	require.NoError(t, err)
	assert.Equal(t, domain.GoalStatusActive, gr.Goal.Status)

	// clear → 无目标
	gc, err := svc.SetGoal(ctx, sesID, domain.GoalREQ{Action: "clear"})
	require.NoError(t, err)
	assert.Nil(t, gc.Goal)

	// pause 空目标报错
	_, err = svc.SetGoal(ctx, sesID, domain.GoalREQ{Action: "pause"})
	require.Error(t, err)
}
