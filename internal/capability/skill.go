package capability

import (
	"context"

	"WorkBaby/internal/harness"
	"WorkBaby/internal/tool"
)

// SkillSource Skill 检索源（service.SkillService 实现）。
type SkillSource interface {
	// Match 按用户输入匹配 Skill 名；无命中返回空。
	Match(input string) string
	// Body 取 Skill 正文与工具白名单；未找到返回 ok=false。
	Body(name string) (body string, tools []string, ok bool)
}

// skillCap 按用户输入触发 Skill 并把它的指导正文注入上下文。
//
// 脚本执行工具（skillrun）由装配方注册，此处不重复暴露。
type skillCap struct{ src SkillSource }

// NewSkill 构造 Skill 能力。
func NewSkill(src SkillSource) Capability { return &skillCap{src: src} }

// funcSource 函数适配器。
type funcSource struct {
	match func(input string) string
	body  func(name string) (string, []string, bool)
}

// NewSkillSource 用函数构造 SkillSource：service 侧已有 Match/Get 实现时免写适配类型。
func NewSkillSource(match func(input string) string, body func(name string) (string, []string, bool)) SkillSource {
	return &funcSource{match: match, body: body}
}

func (f *funcSource) Match(input string) string {
	if f.match == nil {
		return ""
	}
	return f.match(input)
}

func (f *funcSource) Body(name string) (string, []string, bool) {
	if f.body == nil {
		return "", nil, false
	}
	return f.body(name)
}

func (c *skillCap) ID() string { return "skill" }

func (c *skillCap) Tools() []tool.Tool { return nil }

func (c *skillCap) Preload(_ context.Context, p *PreloadCtx) ([]harness.ContextPiece, error) {
	if c.src == nil {
		return nil, nil
	}
	name := c.src.Match(p.UserInput)
	if name == "" {
		return nil, nil
	}
	body, tools, ok := c.src.Body(name)
	if !ok || body == "" {
		return nil, nil
	}
	// 白名单经运行态回传装配方：本轮只暴露 Skill 声明的工具
	if p.State != nil {
		p.State.SkillTools = tools
	}
	return []harness.ContextPiece{{
		Key:   "skill",
		Title: "触发 Skill「" + name + "」，请严格按其指导执行",
		Body:  body,
	}}, nil
}

func (c *skillCap) Capture(_ context.Context, _ *CaptureCtx) error { return nil }
