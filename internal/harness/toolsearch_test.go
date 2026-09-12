package harness

// 折叠激活（deferred tools）：未激活执行侧收紧拒绝，tool_search 检索激活后可正常执行。

import (
	"context"
	"encoding/json"
	"testing"

	"WorkBaby/internal/llm"
	"WorkBaby/internal/tool"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mailTool 独立工具（区别于 echo）：验证「未激活拒绝 → 激活 → 可执行」全链路。
type mailTool struct{}

func (mailTool) Name() string              { return "mail_send" }
func (mailTool) Description() string       { return "send an email" }
func (mailTool) RiskLevel() tool.RiskLevel { return tool.RiskWriteLocal }
func (mailTool) Schema() tool.ToolSchema {
	return tool.ToolSchema{Name: "mail_send", Parameters: json.RawMessage(`{"type":"object"}`)}
}
func (mailTool) Execute(_ context.Context, _ json.RawMessage) tool.ToolResult {
	return tool.ToolResult{Content: "sent"}
}

func TestToolSearchDeferredActivation(t *testing.T) {
	reg := tool.NewRegistry()
	require.NoError(t, reg.Register(echoTool{}))
	require.NoError(t, reg.Register(mailTool{}))
	base := []llm.ToolDefinition{
		{Name: "echo", Description: "echo tool", Parameters: map[string]any{"type": "object"}},
		{Name: ToolSearchName, Description: ToolSearchDef().Description, Parameters: ToolSearchDef().Parameters},
	}
	hidden := []llm.ToolDefinition{{Name: "mail_send", Description: "send an email", Parameters: map[string]any{"type": "object"}}}
	r := NewRunner(&scriptedProvider{}, &recordingSink{}, DefaultConfig()).WithTools(reg, base).WithDeferredTools(hidden)

	call := func(id, name, args string) llm.NormalizedToolCall {
		return llm.NormalizedToolCall{ID: id, Name: name, Arguments: json.RawMessage(args)}
	}

	// 未激活：执行侧收紧，hidden 工具直接拒绝
	msg := r.execOne(context.Background(), "RUN_TS", "SESSION_TS", 0, call("c0", "mail_send", `{}`), nil)
	require.NotNil(t, msg)
	assert.Contains(t, msg.Content, `"reason_code":"not_exposed"`)

	// tool_search 检索激活：返回命中清单
	msg = r.execOne(context.Background(), "RUN_TS", "SESSION_TS", 0, call("c1", ToolSearchName, `{"query":"mail"}`), nil)
	require.NotNil(t, msg)
	assert.Contains(t, msg.Content, "mail_send")

	// 激活后：执行侧放行，工具真实执行
	msg = r.execOne(context.Background(), "RUN_TS", "SESSION_TS", 0, call("c2", "mail_send", `{}`), nil)
	require.NotNil(t, msg)
	assert.Equal(t, "sent", msg.Content)

	// 无命中：提示继续检索，不误激活
	msg = r.execOne(context.Background(), "RUN_TS", "SESSION_TS", 0, call("c3", ToolSearchName, `{"query":"calendar"}`), nil)
	require.NotNil(t, msg)
	assert.Contains(t, msg.Content, "no tool matched")
}
