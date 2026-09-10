package planmode

import (
	"context"
	"testing"

	"WorkBaby/internal/harness"
	"WorkBaby/internal/pkg"
)

// funcApprover tool.Approver 接口的测试桩（签名与 Approve 一致，隐式实现）。
type funcApprover func(ctx context.Context, command string, risk string) bool

func (f funcApprover) Approve(ctx context.Context, command string, risk string) bool {
	return f(ctx, command, risk)
}

// planmode_test.go 覆盖计划模式的执行器级硬拦：激活后写工具必须被拒、
// 退出必须经审批（拒绝则保持计划模式）——这是「先计划后执行」的安全底线。

func readonlyFalse(string) bool { return false }

func newTestStore() *Store {
	return NewStore(func(name string) bool {
		return name == "file_read" || name == "file_grep" || name == "knowledge_search"
	})
}

// TestGuardBlocksSideEffects 激活后：只读放行、写/执行拒绝、exit 放行；关闭后恢复。
func TestGuardBlocksSideEffects(t *testing.T) {
	s := newTestStore()
	if reason := s.Guard("S1", "file_write"); reason != "" {
		t.Fatalf("未激活时不应拦截: %q", reason)
	}
	s.Enable("S1")
	for _, name := range []string{"file_read", "file_grep", "exit_plan_mode"} {
		if reason := s.Guard("S1", name); reason != "" {
			t.Fatalf("%s 在计划模式下应放行: %q", name, reason)
		}
	}
	for _, name := range []string{"file_write", "file_edit", "exec", "run_skill_script"} {
		if reason := s.Guard("S1", name); reason == "" {
			t.Fatalf("%s 在计划模式下应被拒绝", name)
		}
	}
	s.Disable("S1")
	if reason := s.Guard("S1", "file_write"); reason != "" {
		t.Fatalf("关闭后应恢复放行: %q", reason)
	}
}

// TestExitRequiresApproval 退出计划必须经审批：拒绝保持激活，批准才关闭。
func TestExitRequiresApproval(t *testing.T) {
	s := newTestStore()
	s.Enable("S1")

	// 审批拒绝：保持计划模式
	denied := NewExit(s, funcApprover(func(context.Context, string, string) bool { return false }))
	res := denied.Execute(harness.WithRunContext(context.Background(), "R", "S1"), nil)
	if res.Err != nil {
		t.Fatalf("unexpected err: %v", res.Err)
	}
	if !s.Active("S1") {
		t.Fatalf("审批拒绝后应保持计划模式")
	}

	// 审批批准：退出
	approved := NewExit(s, funcApprover(func(context.Context, string, string) bool { return true }))
	res = approved.Execute(harness.WithRunContext(context.Background(), "R", "S1"), nil)
	if res.Err != nil {
		t.Fatalf("unexpected err: %v", res.Err)
	}
	if s.Active("S1") {
		t.Fatalf("审批批准后应退出计划模式")
	}
}

// TestEnterEnablesWithoutSession 会话上下文缺失时报错而非静默（fail closed）。
func TestEnterEnablesWithoutSession(t *testing.T) {
	s := newTestStore()
	res := NewEnter(s).Execute(context.Background(), nil)
	if res.Err == nil {
		t.Fatalf("无会话上下文应报错")
	}
	if _, ok := res.Err.(*pkg.AppError); !ok {
		t.Fatalf("应返回 AppError，got %T", res.Err)
	}
}
