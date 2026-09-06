package capability

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"WorkBaby/internal/harness"
	"WorkBaby/internal/tool"
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
	assert.True(t, seen["m1"] && seen["m2"], "每个能力的 Capture 均被并发调用")
}
