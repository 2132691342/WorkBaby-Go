package exec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"WorkBaby/internal/tool"
)

// fakeBin 在临时目录建一个永远不该被命中的可执行文件，用来证明 PATH 优先级生效。
// 如果"系统 PATH 里同名文件"先被命中（说明 builtin PATH 没生效），测试就会失败。
func fakeBin(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
	// 跨平台扩展名：Windows 是 .exe，其他平台为空
	exe := name
	if filepath.Separator == '\\' {
		exe += ".exe"
	}
	path := filepath.Join(dir, exe)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake exe: %v", err)
	}
	return path
}

// TestResolveWithBuiltinPrefersBuiltin 验证内置运行时 bin 目录在 PATH 最前。
//
// 复现阶段2的内置环境越界 bug：父进程 PATH 里同名可执行文件会被 exec.LookPath 命中，
// 导致内置 python/node/pwsh 失联。修复后必须先命中 builtin 目录里的 fake。
func TestResolveWithBuiltinPrefersBuiltin(t *testing.T) {
	builtinDir := t.TempDir()
	systemDir := t.TempDir()

	// 两个目录各放一个同名可执行文件。
	fakeBin(t, builtinDir, "pyfake")
	fakeBin(t, systemDir, "pyfake")

	// 把系统目录放进 PATH（不带 dirs 前置）—— 模拟未启用内置环境时的状态。
	t.Setenv("PATH", systemDir)

	got, prefix := resolveWithBuiltin("pyfake", []string{builtinDir})
	if len(prefix) != 0 {
		t.Fatalf("expected direct exe, got prefix %v", prefix)
	}
	// 命中文件必须位于 builtinDir 而非 systemDir
	if !strings.HasPrefix(strings.ToLower(got), strings.ToLower(builtinDir)) {
		t.Fatalf("expected builtin hit, got %s", got)
	}
}

// TestResolveWithBuiltinFallsBackToSystem 验证 builtin 目录无目标时正常回落到系统 PATH。
func TestResolveWithBuiltinFallsBackToSystem(t *testing.T) {
	systemDir := t.TempDir()
	fakeBin(t, systemDir, "sysonly")
	t.Setenv("PATH", systemDir)

	got, prefix := resolveWithBuiltin("sysonly", nil)
	if len(prefix) != 0 {
		t.Fatalf("expected direct exe, got prefix %v", prefix)
	}
	if !strings.HasPrefix(strings.ToLower(got), strings.ToLower(systemDir)) {
		t.Fatalf("expected system hit, got %s", got)
	}
}

// TestResolveWithBuiltinRestoresParentPath 验证查找后恢复父进程 PATH，
// 防止并发命令之间相互污染。
func TestResolveWithBuiltinRestoresParentPath(t *testing.T) {
	systemDir := t.TempDir()
	fakeBin(t, systemDir, "x")
	origPath := systemDir + string(os.PathListSeparator) + "/usr/bin"
	t.Setenv("PATH", origPath)

	builtinDir := t.TempDir()
	_, _ = resolveWithBuiltin("x", []string{builtinDir})

	after := os.Getenv("PATH")
	if after != origPath {
		t.Fatalf("PATH should be restored: before=%q after=%q", origPath, after)
	}
}

// TestEnvWithPathPrefixed 验证子进程环境 PATH 把 dirs 前置。
func TestEnvWithPathPrefixed(t *testing.T) {
	t.Setenv("PATH", "/usr/bin"+string(os.PathListSeparator)+"/bin")

	env := envWithPath([]string{"/opt/builtin"})
	hasBuiltinFirst := false
	for _, kv := range env {
		if strings.HasPrefix(strings.ToLower(kv), "path=") {
			v := kv[len("PATH="):]
			if strings.HasPrefix(v, "/opt/builtin") {
				hasBuiltinFirst = true
			}
			if strings.Contains(v, "/opt/builtin"+string(os.PathListSeparator)+"/usr/bin") {
				// builtin 在 system 之前
				if !strings.HasPrefix(v, "/opt/builtin") {
					t.Fatalf("builtin not first in PATH: %s", v)
				}
				return
			}
		}
	}
	if !hasBuiltinFirst {
		t.Fatal("builtin path not found in env")
	}
}

// TestExecPolicy 不触发 runtime.ExecPolicy 的旁路，仅校验 tool 包的 policy 装配。
func TestExecPolicyBaseline(t *testing.T) {
	policy := tool.ExecPolicy{
		AllowedBinaries: []string{"pyfake"},
	}
	if ok, _ := policy.Classify("pyfake --version"); !ok {
		t.Fatal("expected whitelisted cmd to pass")
	}
	if ok, _ := policy.Classify("danger --rm -rf /"); ok {
		t.Fatal("expected non-whitelisted to be blocked")
	}
}