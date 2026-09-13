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

	// set 已有活动目标 = replace（ZCode 同款），Round 保留
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

// TestParseGoalVerdict 校验输出宽松解析：容忍代码块包裹；缺 next_step 拒绝。
func TestParseGoalVerdict(t *testing.T) {
	v, err := parseGoalVerdict("```json\n{\"done\":false,\"next_step\":\"跑 go test\",\"because\":\"测试仍红\"}\n```")
	require.NoError(t, err)
	assert.False(t, v.Done)
	assert.Equal(t, "跑 go test", v.NextStep)

	done, err := parseGoalVerdict(`{"done":true,"because":"全部测试通过"}`)
	require.NoError(t, err)
	assert.True(t, done.Done)

	_, err = parseGoalVerdict(`{"done":false}`)
	require.Error(t, err, "未达成但没给下一步动作：无法续跑，必须拒绝")
}

// TestAgentsMdPieces AGENTS.md 注入：工作区文件存在才出段，缺失静默。
func TestAgentsMdPieces(t *testing.T) {
	ses := &domain.ChatSessionDO{WorkspacePath: t.TempDir()}
	pieces := agentsMdPieces(ses)
	assert.Empty(t, pieces, "无 AGENTS.md 时静默跳过")
}
