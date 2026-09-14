package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"WorkBaby/internal/domain"
)

// TestHookMatcher 匹配式三种写法：全部 / 精确名单 / 正则。
// 匹配式写错会让钩子在错误的工具上生效或完全静默，属于安全护栏。
func TestHookMatcher(t *testing.T) {
	cases := []struct {
		name    string
		matcher string
		tool    string
		want    bool
	}{
		{"empty matches all", "", "exec", true},
		{"star matches all", "*", "write_file", true},
		{"name list hit", "Write|Edit|exec", "Edit", true},
		{"name list miss", "Write|Edit", "exec", false},
		{"name list not substring", "Write", "write_file", false},
		{"regex hit", `^(write|edit)_.*$`, "write_file", true},
		{"regex miss", `^(write|edit)_.*$`, "exec", false},
		{"invalid regex skipped", `([unclosed`, "exec", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, hookMatches(tc.matcher, tc.tool))
		})
	}
}

// TestHookMatcherValidation 写入期校验：非法正则直接拒绝，避免落库后静默失效。
func TestHookMatcherValidation(t *testing.T) {
	require.NoError(t, checkMatcher(""))
	require.NoError(t, checkMatcher("*"))
	require.NoError(t, checkMatcher("Write|Edit"))
	require.NoError(t, checkMatcher(`^mcp__.*$`))
	require.Error(t, checkMatcher(`([unclosed`))
}

// TestHookEventNormalize 历史事件名归一：旧库数据继续生效，不静默失效。
func TestHookEventNormalize(t *testing.T) {
	assert.Equal(t, domain.HookEventPreToolUse, domain.NormalizeHookEvent("before_tool"))
	assert.Equal(t, domain.HookEventPostToolUse, domain.NormalizeHookEvent("after_tool"))
	assert.Equal(t, domain.HookEventSessionStart, domain.NormalizeHookEvent("run_start"))
	assert.Equal(t, domain.HookEventStop, domain.NormalizeHookEvent("run_end"))
	assert.Equal(t, domain.HookEventPreToolUse, domain.NormalizeHookEvent("PreToolUse"))
	assert.False(t, isHookEvent("bogus_event"))
}

// TestHookOutputParsing 子进程输出契约：只认以 { 开头的合法 JSON 行，退出码 2 等价 deny。
func TestHookOutputParsing(t *testing.T) {
	cases := []struct {
		name       string
		stdout     string
		exitCode   int
		wantPerm   string
		wantReason string
		wantCtx    string
	}{
		{name: "empty output", stdout: "", exitCode: 0},
		{name: "exit 2 denies", stdout: "", exitCode: 2, wantPerm: "deny"},
		{name: "exit 2 with reason", stdout: `{"reason":"not allowed"}`, exitCode: 2, wantPerm: "deny", wantReason: "not allowed"},
		{
			name:     "standard deny",
			stdout:   `{"hookSpecificOutput":{"permissionDecision":"deny","permissionDecisionReason":"prod dir"}}`,
			exitCode: 0, wantPerm: "deny", wantReason: "prod dir",
		},
		{
			name:     "standard ask",
			stdout:   `{"hookSpecificOutput":{"permissionDecision":"ask"}}`,
			exitCode: 0, wantPerm: "ask",
		},
		{
			name:     "permission decision body",
			stdout:   `{"hookSpecificOutput":{"decision":{"behavior":"deny","message":"policy"}}}`,
			exitCode: 0, wantPerm: "deny", wantReason: "policy",
		},
		{
			name:     "additional context",
			stdout:   `{"hookSpecificOutput":{"additionalContext":"只改相关文件"}}`,
			exitCode: 0, wantCtx: "只改相关文件",
		},
		{name: "underscore context alias", stdout: `{"additional_context":"legacy"}`, exitCode: 0, wantCtx: "legacy"},
		{name: "noisy stderr tolerated", stdout: "log line\n{\"additionalContext\":\"ok\"}\nmore log", exitCode: 0, wantCtx: "ok"},
		{name: "non json ignored", stdout: "Traceback (most recent call last)", exitCode: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := parseHookOutput([]byte(tc.stdout))
			d.ExitCode = tc.exitCode
			perm, reason := d.ToolPermission()
			assert.Equal(t, tc.wantPerm, perm)
			assert.Equal(t, tc.wantReason, reason)
			assert.Equal(t, tc.wantCtx, d.ContextText())
		})
	}
}

// TestHookStopDecision Stop 续跑判定：必须有反馈文本才续跑，否则收尾。
func TestHookStopDecision(t *testing.T) {
	block := func(v bool) *bool { return &v }
	cases := []struct {
		name   string
		dec    domain.HookDecision
		reason string
		want   bool
	}{
		{name: "block with reason", dec: domain.HookDecision{Decision: "block", Reason: "还缺测试结果"}, reason: "还缺测试结果", want: true},
		{name: "block without feedback", dec: domain.HookDecision{Decision: "block"}, want: false},
		{name: "exit 2 with reason", dec: domain.HookDecision{ExitCode: 2, Reason: "补齐说明"}, reason: "补齐说明", want: true},
		{name: "continue true with context", dec: domain.HookDecision{Continue: block(true), AdditionalContext: "再检查一次"}, reason: "再检查一次", want: true},
		{name: "no decision", dec: domain.HookDecision{Decision: "allow"}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			feedback, block := stopFeedback(tc.dec)
			assert.Equal(t, tc.want, block)
			assert.Equal(t, tc.reason, feedback)
		})
	}
}

// TestHookSortOrder 钩子按 sort 升序执行：顺序决定 deny/ask 叠加的先后语义。
func TestHookSortOrder(t *testing.T) {
	rows := []domain.UserHookDO{{Name: "c", Sort: 30}, {Name: "a", Sort: 10}, {Name: "b", Sort: 20}}
	sortHooks(rows)
	assert.Equal(t, []string{"a", "b", "c"}, []string{rows[0].Name, rows[1].Name, rows[2].Name})
}
