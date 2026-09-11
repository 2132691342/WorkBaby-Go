package harness

import (
	"path"
	"strings"
	"time"

	"WorkBaby/internal/llm"
)

// Definition 声明式 Agent 定义：人设 / 工具策略 / 记忆策略 / 运行预算。纯数据，无依赖注入。
//
// 装配方（service）按定义物化每次 run 的 Runner 配置，因此同一定义可在 chat / 工作流节点 /
// 定时任务 / 子 Agent 中复用，替换各调用点的硬编码参数。
type Definition struct {
	Name        string       // 唯一名（"default" / "coding" / "research" / "writer"）
	Description string       // 一句话职责（未来供 UI 选择与命令面板）
	Persona     string       // 人设/方法论 system 段；空 = 不注入
	Tools       ToolPolicy   // 工具策略（Allow/Ask/Deny 三级）
	Memory      MemoryPolicy // 记忆策略
	Budget      Budget       // 运行预算
}

// ToolPolicy Agent 层的工具白名单/黑名单：声明「本 Agent 该带哪些工具」。
// 与执行层 internal/tool/policy.go 分工：后者负责 allow/ask/deny 的运行时裁决。
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

// FilterTools 按 Agent 策略过滤工具定义：Deny 命中剔除 → Allow 非空时仅留命中（glob）
// → MaxTools 按序截断。在装配方既有过滤（启用状态 / Skill 白名单）之后应用。
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

// ToolDefBudgetPercent 工具定义占上下文预算的份额（%）。
// 工具 schema 与历史消息抢同一个 prompt 预算，MCP 挂载几十个工具时会挤掉对话历史。
const ToolDefBudgetPercent = 15

// TrimToolDefs 按 token 预算裁剪工具定义：超限时从尾部丢弃（调用方给定的优先级序），
// 至少保留 minToolDefs 个，返回保留集与被丢弃的工具名；maxTokens<=0 视为不限。
func TrimToolDefs(defs []llm.ToolDefinition, maxTokens int) ([]llm.ToolDefinition, []string) {
	if maxTokens <= 0 || len(defs) <= minToolDefs {
		return defs, nil
	}
	out := defs
	var dropped []string
	for len(out) > minToolDefs && EstimateToolTokens(out) > maxTokens {
		dropped = append(dropped, out[len(out)-1].Name)
		out = out[:len(out)-1]
	}
	return out, dropped
}

// minToolDefs 裁剪下限：低于该数量宁可超预算，也不把主力工具裁没。
const minToolDefs = 8

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
	// personaMethodology 通用工作方法论：内置 Agent 共用，避免各人设各维护一套原则。
	// 每条都是可判定的动作约束——空泛的「先理解再行动」无法指导行为。
	personaMethodology = `
## 工作原则
1. 先探查再动手：信息不足时先用 file_list / file_read / doc_reader / knowledge_search 看清现状，不凭猜测下结论。
2. 多步任务先列计划：开工前用 todo(plan) 拆成可勾选的步骤，每完成一项立即 todo(mark_done)。
3. 最小改动：只动与目标直接相关的部分，不顺手重构，不臆造不存在的 API、路径或参数。
4. 用工具验证结果：写完代码就 exec 跑构建或测试，改完文件读回来确认——不要凭记忆断言「已完成」。
5. 失败要改道，不要硬重试：同一调用连续失败两次就换工具、换参数或向用户说明，禁止原样重复。
6. 不确定就问：关键前提缺失且无法自行推断时，用 request_input 向用户确认，不要替用户假设。
7. 成果必须可核验：严禁声称「已创建/已生成/已保存」任何文件或「已执行」任何操作，除非本轮确实调用了对应工具且成功；没有可用工具（如缺少技能/运行时）就如实说明缺什么，绝不虚构完成过程或编造产出路径。

## 输出
- 简洁中文，先结论后过程；不复述工具返回的原文，只给结论与必要证据。
- 任务收尾时明确列出：改了哪些文件、跑了什么命令、结果如何。`
	// personaDefault 通用办公助手：一次一问、先计划后动手、陈述不确定性。
	personaDefault = "你是 WorkBaby，运行在用户本机上的个人 AI 助手，可以调用工具真实干活，而不只是给建议。" + personaMethodology
	// personaCoding 工程助手：项目级任务（读代码、改文件、跑命令）。
	personaCoding = "你是 WorkBaby 的工程助手，负责在本机代码仓库干活。\n" +
		"工作方式：先探查再修改；改动最小化；跑验证后再汇报；不臆造不存在的 API。" + personaMethodology
	// personaResearch 检索助手：搜索与知识库优先。
	personaResearch = "你是 WorkBaby 的调研助手，负责联网搜索与本地知识库检索。\n" +
		"工作方式：优先引用可核验来源；区分事实与推断；给出结论时附依据。" + personaMethodology
	// personaWriter 写作助手：轻工具、长输出。
	personaWriter = "你是 WorkBaby 的写作助手，负责整理、总结与创作。\n" +
		"工作方式：先确认目标与篇幅；只在你明确需要时才调用工具；输出结构清晰。" + personaMethodology
)
