package capability

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"WorkBaby/internal/harness"
	"WorkBaby/internal/llm"
	"WorkBaby/internal/memory"
	"WorkBaby/internal/tool"
	"WorkBaby/internal/tool/memorywrite"
)

// mockCap 可编程 mock：Preload 可返回多段 / 报错 / panic。
type mockCap struct {
	id      string
	pieces  []harness.ContextPiece
	err     error
	panic_  bool
	capture func(context.Context, *CaptureCtx) error
}

func (m *mockCap) ID() string { return m.id }

func (m *mockCap) Preload(_ context.Context, _ *PreloadCtx) ([]harness.ContextPiece, error) {
	if m.panic_ {
		panic("boom")
	}
	return m.pieces, m.err
}

func (m *mockCap) Tools() []tool.Tool { return nil }

func (m *mockCap) Capture(ctx context.Context, c *CaptureCtx) error {
	if m.capture != nil {
		return m.capture(ctx, c)
	}
	return nil
}

func TestRegistryPreloadOrder(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&mockCap{id: "c", pieces: []harness.ContextPiece{{Key: "c"}}}, OrderSkill))
	require.NoError(t, r.Register(&mockCap{id: "a", pieces: []harness.ContextPiece{{Key: "a"}}}, OrderPersona))
	// 重复 ID 拒绝
	assert.Error(t, r.Register(&mockCap{id: "a"}, OrderMemory))

	pieces := r.PreloadAll(context.Background(), &PreloadCtx{})
	require.Len(t, pieces, 2)
	assert.Equal(t, "a", pieces[0].Key, "order 小者先注入")
	assert.Equal(t, "c", pieces[1].Key)
}

func TestRegistryPreloadFailureIsolation(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&mockCap{id: "err", err: errors.New("x")}, OrderPersona))
	require.NoError(t, r.Register(&mockCap{id: "panic", panic_: true}, OrderMemory))
	require.NoError(t, r.Register(&mockCap{id: "ok", pieces: []harness.ContextPiece{{Key: "ok"}}}, OrderSkill))

	pieces := r.PreloadAll(context.Background(), &PreloadCtx{})
	require.Len(t, pieces, 1, "报错与 panic 的能力被跳过，其余正常注入")
	assert.Equal(t, "ok", pieces[0].Key)
}

func TestRegistryCaptureAll(t *testing.T) {
	r := NewRegistry()
	called := make(chan string, 2)
	require.NoError(t, r.Register(&mockCap{id: "m1", capture: func(_ context.Context, _ *CaptureCtx) error {
		called <- "m1"
		return nil
	}}, OrderMemory))
	require.NoError(t, r.Register(&mockCap{id: "m2", capture: func(_ context.Context, _ *CaptureCtx) error {
		called <- "m2"
		return nil
	}}, OrderSkill))

	r.CaptureAll(&CaptureCtx{SessionID: "s"}, 0)
	seen := map[string]bool{}
	for range 2 {
		seen[<-called] = true
	}
	assert.True(t, seen["m1"] && seen["m2"], "每个能力的 Capture 均被异步调用")
}

func TestSkillCapStatePropagation(t *testing.T) {
	src := NewSkillSource(
		func(input string) string {
			if input == "翻译这段" {
				return "translator"
			}
			return ""
		},
		func(name string) (string, []string, bool) {
			return "按译文规范执行", []string{"file_read", "http"}, true
		},
	)
	c := NewSkill(src)

	// 命中：注入正文并把工具白名单写入运行态
	pieces, err := c.Preload(context.Background(), &PreloadCtx{UserInput: "翻译这段", State: &RunState{}})
	require.NoError(t, err)
	require.Len(t, pieces, 1)
	assert.Equal(t, "skill", pieces[0].Key)
	st := &RunState{}
	_, err = c.Preload(context.Background(), &PreloadCtx{UserInput: "翻译这段", State: st})
	require.NoError(t, err)
	assert.Equal(t, []string{"file_read", "http"}, st.SkillTools)

	// 未命中：不注入
	pieces, err = c.Preload(context.Background(), &PreloadCtx{UserInput: "随便聊聊", State: &RunState{}})
	require.NoError(t, err)
	assert.Empty(t, pieces)
}

func TestMemoryWriteTool(t *testing.T) {
	w := &mockWriter{}
	mw := memorywrite.New(w, func(context.Context) string { return "s1" })

	// long_term 路径
	res := mw.Execute(context.Background(), json.RawMessage(`{"content":"用户偏好深色主题"}`))
	require.NoError(t, res.Err)
	assert.Equal(t, "用户偏好深色主题", w.longTermDelta)

	// fact 路径
	res = mw.Execute(context.Background(), json.RawMessage(`{"kind":"fact","content":"每周一开例会","subject":"用户","key":"日程"}`))
	require.NoError(t, res.Err)
	require.NotNil(t, w.fact)
	assert.Equal(t, "用户", w.fact.Subject)
	assert.Equal(t, "日程", w.fact.Key)

	// 空内容拒绝
	res = mw.Execute(context.Background(), json.RawMessage(`{}`))
	assert.Error(t, res.Err)
}

type mockWriter struct {
	longTermDelta string
	fact          *memory.FactProposal
}

func (w *mockWriter) AppendLongTerm(_ context.Context, _, delta string) error {
	w.longTermDelta = delta
	return nil
}

func (w *mockWriter) WriteFact(_ context.Context, p memory.FactProposal) (string, error) {
	w.fact = &p
	return "FACT_test", nil
}

// TestLongTermDeltaStripsThink 兼容端点把推理写进正文时，记忆里不能出现 <think> 原文；
// 纯推理的助手消息（剥离后为空）整行不入档。
func TestLongTermDeltaStripsThink(t *testing.T) {
	transcript := []llm.Message{
		*llm.UserMessage("看一下工作区有哪些文件？"),
		{Role: llm.RoleAssistant, Content: "<think>我应该用 file_list 工具</think>\n\n我来查看工作区文件。"},
	}
	delta := longTermDelta(transcript)
	assert.Contains(t, delta, "用户: 看一下工作区有哪些文件？")
	assert.Contains(t, delta, "助手: 我来查看工作区文件。")
	assert.NotContains(t, delta, "<think>", "think 标签不得进入长期记忆")

	// 助手只有推理（剥离后为空）→ 助手行整条省略
	pure := []llm.Message{
		*llm.UserMessage("hi"),
		{Role: llm.RoleAssistant, Content: "<think>只想想</think>"},
	}
	delta = longTermDelta(pure)
	assert.Equal(t, "用户: hi", delta)
}
