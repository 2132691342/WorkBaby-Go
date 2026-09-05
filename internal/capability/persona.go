package capability

import (
	"context"

	"WorkBaby/internal/harness"
	"WorkBaby/internal/tool"
)

// personaCap 注入 Agent 人设（Definition.Persona）。
type personaCap struct{}

// NewPersona 构造人格能力。
func NewPersona() Capability { return &personaCap{} }

func (c *personaCap) ID() string { return "persona" }

func (c *personaCap) Preload(_ context.Context, p *PreloadCtx) ([]harness.ContextPiece, error) {
	msg := p.Def.PersonaSystemMessage()
	if msg == nil {
		return nil, nil
	}
	return []harness.ContextPiece{{Key: "persona", Title: "角色", Body: msg.Content}}, nil
}

func (c *personaCap) Tools() []tool.Tool { return nil }

func (c *personaCap) Capture(_ context.Context, _ *CaptureCtx) error { return nil }
