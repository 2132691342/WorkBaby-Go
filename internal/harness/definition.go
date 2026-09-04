package harness

import (
	"path"
	"strings"
	"time"

	"WorkBaby/internal/llm"
)

// Definition 声明式 Agent 定义——「Agent 是什么」：
// 人设 / 工具策略 / 记忆策略 / 运行预算。纯数据，不携带依赖注入。
//
// 装配方（service）按 Definition 物化每次 run 的 Runner 配置；同一定义可随处运行
// （chat / workflow 节点 / cron / 子 Agent），替换每调用点的硬编码参数。
type Definition struct {
	Name        string       // 唯一名（"default" / "coding" / "research" / "writer"）
	Description string       // 一句话职责（未来供 UI 选择与命令面板）
	Persona     string       // 人设/方法论 system 段；空 = 不注入
	Tools       ToolPolicy   // 工具策略（v1 只消费 Allow/Deny；P1-D 扩展三级）
	Memory      MemoryPolicy // 记忆策略
	Budget      Budget       // 运行预算
}

// ToolPolicy Agent 层工具白名单/黑名单。
// 与执行层（tool/policy.go，P1-D）分工：本结构是「Agent 该带哪些工具」的声明，
// 执行层负责 allow/ask/deny 运行时裁决。
type ToolPolicy struct {
	Allow    []string // 白名单（glob）；空 = 全部可见工具
	Deny     []string // 黑名单（glob）
	MaxTools int      // 暴露给 LLM 的工具数上限；<=0 不限制
}

// MemoryPolicy 记忆接入策略。
type MemoryPolicy struct {
	Enabled     bool // 是否参与长期记忆召回
	RecallLimit int  // 召回条数上限；<=0 用默认
	Formation   bool // run 完成后是否形成情景记忆
}

// Budget 运行预算（Runner.Config 的语义化映射 + 墙钟上限）。
type Budget struct {
	MaxTurns        int           // 轮次上限；<=0 用默认 30
	MaxTokens       int           // run 累计 token 预算；<=0 不限
	ContextTokens   int           // 单轮消息估算 token 预算；超预算每轮自动压缩；<=0 用默认
	ToolCallTimeout time.Duration // 单次工具执行超时；<=0 用默认 5min
	MaxWallTime     time.Duration // 整个 run 墙钟上限；<=0 不限制
}

// Apply 把非零预算落到 Runner 配置。
func (b Budget) Apply(cfg *Config) {
	if cfg == nil {
		return
	}
	if b.MaxTurns > 0 {
		cfg.MaxTurns = b.MaxTurns
	}
	if b.MaxTokens > 0 {
		cfg.MaxTokens = b.MaxTokens
	}
	if b.ContextTokens > 0 {
		cfg.ContextBudget = b.ContextTokens
	}
	if b.ToolCallTimeout > 0 {
		cfg.ToolCallTimeout = b.ToolCallTimeout
	}
}

// PersonaSystemMessage 生成人设 system 消息；persona 为空返回 nil（不注入）。
func (d Definition) PersonaSystemMessage() *llm.Message {
	if strings.TrimSpace(d.Persona) == "" {
		return nil
	}
	return llm.SystemMessage(d.Persona)
}

// FilterTools 按 Agent 策略过滤工具定义：
//   - 命中 Deny 的剔除；
//   - Allow 非空时仅保留命中 Allow 的（支持 glob）；
//   - MaxTools>0 时按顺序截断。
//
// 在装配方既有过滤（启用状态 / Skill 白名单）之后应用。
func (d Definition) FilterTools(defs []llm.ToolDefinition) []llm.ToolDefinition {
	if len(d.Tools.Allow) == 0 && len(d.Tools.Deny) == 0 && d.Tools.MaxTools <= 0 {
		return defs
	}
	out := make([]llm.ToolDefinition, 0, len(defs))
	for _, def := range defs {
		if matchAnyGlob(def.Name, d.Tools.Deny) {
			continue
		}
		if len(d.Tools.Allow) > 0 && !matchAnyGlob(def.Name, d.Tools.Allow) {
			continue
		}
		out = append(out, def)
	}
	if d.Tools.MaxTools > 0 && len(out) > d.Tools.MaxTools {
		out = out[:d.Tools.MaxTools]
	}
	return out
}

// matchAnyGlob 判断 name 命中任一 glob 模式（path.Match 语义）。
func matchAnyGlob(name string, pats []string) bool {
	for _, p := range pats {
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}

// Persona 语义段（内置 Agent 复用）。
const (
	// personaDefault 通用办公助手：一次一问、先计划后动手、陈述不确定性。
	personaDefault = "你是 WorkBaby，本机个人 AI 助手。\n" +
		"原则：先理解再行动；需要外部动作时先说明计划；结果用简洁中文汇报；不确定时明说。"
	// personaCoding 工程助手：项目级任务（读代码、改文件、跑命令）。
	personaCoding = "你是 WorkBaby 的工程助手，负责在本机代码仓库干活。\n" +
		"工作方式：先探查再修改；改动最小化；跑验证后再汇报；不臆造不存在的 API。"
	// personaResearch 检索助手：搜索与知识库优先。
	personaResearch = "你是 WorkBaby 的调研助手，负责联网搜索与本地知识库检索。\n" +
		"工作方式：优先引用可核验来源；区分事实与推断；给出结论时附依据。"
	// personaWriter 写作助手：轻工具、长输出。
	personaWriter = "你是 WorkBaby 的写作助手，负责整理、总结与创作。\n" +
		"工作方式：先确认目标与篇幅；只在你明确需要时才调用工具；输出结构清晰。"
)
