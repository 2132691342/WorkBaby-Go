package service

import (
	"testing"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/repo"

	"github.com/stretchr/testify/require"
)

// TestUserCommandLifecycle 自定义斜杠命令：校验 + upsert 幂等 + 删除。
func TestUserCommandLifecycle(t *testing.T) {
	svc := NewUserCommandService(repo.NewUserCommandRepo(newChatOpsTestDB(t)))
	ctx := t.Context()

	created, err := svc.Upsert(ctx, &domain.UserCommandREQ{
		Name:        "weekly",
		Prompt:      "帮我把本周的工作整理成周报：$ARGUMENTS",
		Description: "生成周报",
	})
	require.NoError(t, err)
	require.Equal(t, "weekly", created.Name)

	// 同名 upsert 覆盖
	updated, err := svc.Upsert(ctx, &domain.UserCommandREQ{Name: "weekly", Prompt: "周报 v2"})
	require.NoError(t, err)
	list, err := svc.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "周报 v2", list[0].Prompt)
	require.Equal(t, updated.ID, list[0].ID)

	// 非法名 / 空 prompt 拒绝
	_, err = svc.Upsert(ctx, &domain.UserCommandREQ{Name: "Bad Name!", Prompt: "x"})
	require.Error(t, err)
	_, err = svc.Upsert(ctx, &domain.UserCommandREQ{Name: "ok-name", Prompt: "  "})
	require.Error(t, err)

	// 删除
	require.NoError(t, svc.Delete(ctx, "weekly"))
	_, err = svc.List(ctx)
	require.NoError(t, err)
	list, err = svc.List(ctx)
	require.NoError(t, err)
	require.Empty(t, list)
}
