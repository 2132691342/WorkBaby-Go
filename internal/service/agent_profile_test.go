package service

import (
	"testing"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/harness"
	"WorkBaby/internal/repo"

	"github.com/stretchr/testify/require"
)

// TestAgentProfileLifecycle 校验 upsert → 注册表同步 → 启停 → 删除全链路。
func TestAgentProfileLifecycle(t *testing.T) {
	gdb := newChatOpsTestDB(t)
	svc := NewAgentProfileService(repo.NewAgentProfileRepo(gdb))
	ctx := t.Context()

	enabled := true
	created, err := svc.Upsert(ctx, &domain.AgentProfileREQ{
		Name:         "translator",
		Description:  "中英翻译",
		SystemPrompt: "你是翻译助手，只输出译文。",
		ToolsAllow:   []string{"webfetch"},
		MaxTurns:     6,
		Enabled:      &enabled,
	})
	require.NoError(t, err)
	require.Equal(t, "translator", created.Name)
	require.Equal(t, []string{"webfetch"}, created.ToolsAllow)

	// 注册表已同步：harness.Agent 命中自定义定义
	def := harness.Agent("translator")
	require.Equal(t, "translator", def.Name)
	require.Equal(t, "你是翻译助手，只输出译文。", def.Persona)
	require.Equal(t, 6, def.Budget.MaxTurns)
	found := false
	for _, d := range harness.AllAgents() {
		if d.Name == "translator" {
			found = true
		}
	}
	require.True(t, found)

	// 内置保留名拒绝
	_, err = svc.Upsert(ctx, &domain.AgentProfileREQ{Name: "coding"})
	require.Error(t, err)

	// 非法名拒绝
	_, err = svc.Upsert(ctx, &domain.AgentProfileREQ{Name: "Bad Name!"})
	require.Error(t, err)

	// 停用后从注册表摘除
	require.NoError(t, svc.SetEnabled(ctx, "translator", false))
	require.Equal(t, "default", harness.Agent("translator").Name)

	// 重新启用即回归注册表
	require.NoError(t, svc.SetEnabled(ctx, "translator", true))
	require.Equal(t, "translator", harness.Agent("translator").Name)

	// 删除后回退 default
	require.NoError(t, svc.Delete(ctx, "translator"))
	require.Equal(t, "default", harness.Agent("translator").Name)
	_, err = svc.List(ctx)
	require.NoError(t, err)
}
