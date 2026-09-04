package service

import (
	"WorkBaby/internal/domain"
	"WorkBaby/internal/repo"
	"context"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"os"
	"path/filepath"
	"testing"
)

func newTrustSvc(t *testing.T) *TrustService {
	t.Helper()
	gdb := newChatOpsTestDB(t)
	roots := []string{filepath.Join(t.TempDir(), "trusted")}
	return NewTrustService(repo.NewWorkspaceTrustRepo(gdb), roots...)
}



// TestTrustResolve_DefaultAsk 未登记目录：ask + 未命中。
func TestTrustResolve_DefaultAsk(t *testing.T) {
	svc := newTrustSvc(t)
	res, err := svc.Resolve(context.Background(), filepath.Join(t.TempDir(), "unknown-project"))
	require.NoError(t, err)
	assert.Equal(t, domain.TrustStateAsk, res.State)
	assert.False(t, res.Matched)
}

// TestTrustResolve_InheritedAllow 登记祖先目录：后代继承 allow。
func TestTrustResolve_InheritedAllow(t *testing.T) {
	svc := newTrustSvc(t)
	ctx := context.Background()
	parent := filepath.Join(t.TempDir(), "proj")
	child := filepath.Join(parent, "sub", "deep")
	_, err := svc.Decide(ctx, domain.WorkspaceTrustREQ{Path: parent, State: string(domain.TrustStateAllow)})
	require.NoError(t, err)
	res, err := svc.Resolve(ctx, child)
	require.NoError(t, err)
	assert.Equal(t, domain.TrustStateAllow, res.State)
	assert.Equal(t, parent, res.Source)
}

// TestTrustEnsure_ApproveAndPersist ask → 询问 → 批准后落盘 allow；下次直接 allow。
func TestTrustEnsure_ApproveAndPersist(t *testing.T) {
	svc := newTrustSvc(t)
	ctx := context.Background()
	project := filepath.Join(t.TempDir(), "myproj")

	approveCount := 0
	svc.WithApprover(func(ctx context.Context, _ string) bool {
		approveCount++
		return true
	})

	ok, reason := svc.Ensure(ctx, project)
	require.True(t, ok, reason)
	assert.Equal(t, 1, approveCount)

	// 再次访问：已落盘 allow，不应再询问
	ok2, reason2 := svc.Ensure(ctx, project)
	require.True(t, ok2, reason2)
	assert.Equal(t, 1, approveCount)
}


// TestTrustRevoke 撤销后回到 ask。
func TestTrustRevoke(t *testing.T) {
	svc := newTrustSvc(t)
	ctx := context.Background()
	p := filepath.Join(t.TempDir(), "p")
	_, err := svc.Decide(ctx, domain.WorkspaceTrustREQ{Path: p, State: string(domain.TrustStateDeny)})
	require.NoError(t, err)
	res, err := svc.Resolve(ctx, p)
	require.NoError(t, err)
	assert.Equal(t, domain.TrustStateDeny, res.State)

	require.NoError(t, svc.Revoke(ctx, p))
	res2, err := svc.Resolve(ctx, p)
	require.NoError(t, err)
	assert.Equal(t, domain.TrustStateAsk, res2.State)
}

func newWorkspaceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "ws.db")+"?_pragma=journal_mode(MEMORY)"), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, e := gdb.DB()
		if e == nil {
			_ = sqlDB.Close()
		}
	})
	return gdb
}

func setupWorkspace(t *testing.T, files map[string]string) (*WorkspaceService, string) {
	t.Helper()
	root := t.TempDir()
	sid := "SESSION_TEST"
	// 工作区文件以 session 子目录为根（service.Dir(sessionID) = root/sessionID）。
	for rel, content := range files {
		full := filepath.Join(root, sid, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	return NewWorkspaceService(root), sid
}

// TestWorkspaceReadFileBlocksPathTraversal ReadFile 必须拦截 ../ 越界；
// 跨平台语义：filepath.Clean + Rel 能可靠识别 ../ 跨越。
func TestWorkspaceReadFileBlocksPathTraversal(t *testing.T) {
	svc, sid := setupWorkspace(t, map[string]string{
		"inside.txt":     "ok",
		"sub/secret.txt": "secret",
	})

	cases := []struct {
		name string
		path string
	}{
		{"traversal up", "../../etc/passwd"},
		{"traversal mix", "sub/../../etc/hosts"},
		{"backslash traversal", `..\..\windows\system.ini`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := svc.ReadFile(context.Background(), sid, tc.path)
			require.Error(t, err, "路径 %q 应被拦截", tc.path)
			require.Contains(t, err.Error(), "escapes", "错误信息应明确为越界")
		})
	}
}

// TestWorkspaceListFilesHidesInternal 内部隐藏目录（local/、.workbaby/、.index/）
// 不出现在文件面板中——这些是会话工作区外的 runtime 数据。
func TestWorkspaceListFilesHidesInternal(t *testing.T) {
	svc, sid := setupWorkspace(t, map[string]string{
		"visible.md":          "visible",
		"local/draft.md":      "should hide",
		".workbaby/meta.json": "should hide",
		".index/cache.bin":    "should hide",
		"sub/normal.txt":      "visible",
	})
	files, err := svc.ListFiles(context.Background(), sid)
	require.NoError(t, err)
	paths := make(map[string]bool, len(files))
	for _, f := range files {
		paths[f.Path] = true
	}
	require.True(t, paths["visible.md"])
	require.True(t, paths["sub/normal.txt"])
	require.False(t, paths["local/draft.md"], "local/ 必须隐藏")
	require.False(t, paths[".workbaby/meta.json"], ".workbaby/ 必须隐藏")
	require.False(t, paths[".index/cache.bin"], ".index/ 必须隐藏")
}

// 编译期检查：repo.NewChatSessionRepo 的引用不会让本文件编译失败（占位）
var _ = repo.NewChatSessionRepo
