package capability

import (
	"context"

	"WorkBaby/internal/harness"
	"WorkBaby/internal/tool"
)

// SkillHit 一次 Skill 命中的完整信息：上下文注入与前端时间线回放共用同一份。
type SkillHit struct {
	Name        string
	Body        string
	Tools       []string // 工具白名单；空 = 不限制
	Source      string   // builtin / download / custom
	Version     string
	Description string
}

// SkillSource Skill 检索源（service.SkillService 实现）。
type SkillSource interface {
	// Match 按用户输入匹配 Skill 名；无命中返回空。
	Match(input string) string
	// Hit 取命中详情；未找到返回 ok=false。
	Hit(name string) (hit SkillHit, ok bool)
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
	hit   func(name string) (SkillHit, bool)
}

// NewSkillSource 用函数构造 SkillSource：service 侧已有 Match/Get 实现时免写适配类型。
func NewSkillSource(match func(input string) string, hit func(name string) (SkillHit, bool)) SkillSource {
	return &funcSource{match: match, hit: hit}
}

func (f *funcSource) Match(input string) string {
	if f.match == nil {
		return ""
	}
	return f.match(input)
}

func (f *funcSource) Hit(name string) (SkillHit, bool) {
	if f.hit == nil {
		return SkillHit{}, false
	}
	return f.hit(name)
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
	hit, ok := c.src.Hit(name)
	if !ok || hit.Body == "" {
		return nil, nil
	}
	// 命中详情经运行态回传装配方：工具白名单约束本轮可见工具，其余字段供前端时间线回放
	if p.State != nil {
		p.State.SkillTools = hit.Tools
		p.State.SkillName = hit.Name
		p.State.SkillSource = hit.Source
		p.State.SkillVersion = hit.Version
		p.State.SkillDescription = hit.Description
		p.State.SkillInjectedLen = len(hit.Body)
	}
	return []harness.ContextPiece{{
		Key:   "skill",
		Title: "触发 Skill「" + name + "」，请严格按其指导执行",
		Body:  hit.Body,
	}}, nil
}

func (c *skillCap) Capture(_ context.Context, _ *CaptureCtx) error { return nil }
