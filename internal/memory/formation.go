package memory

import (
	"context"
	"regexp"
	"strings"

	"WorkBaby/internal/llm"
)

// Thresholds 形成策略阈值。
//
// Episodic 设到 0.30 后，基础对话分（0.15）单独不够形成情景记忆——必须有触发器
// 主动加分才能沉淀，杜绝「几乎每轮都落一条情景」的噪音爆表问题。
type Thresholds struct {
	Episodic   float64 // 0.30
	Semantic   float64 // 0.25（v2）
	Procedural float64 // 0.35（v2）
}

// DefaultThresholds 默认阈值。
func DefaultThresholds() Thresholds {
	return Thresholds{Episodic: 0.30, Semantic: 0.25, Procedural: 0.35}
}

// DefaultTriggers 触发器 → 加分。
var DefaultTriggers = map[string]float64{
	"EXPLICIT_REMEMBER":   0.55, // 「记住」「remember」
	"PREFERENCE_DETECTED": 0.40, // 「我喜欢」「I prefer」
	"MULTI_STEP_SUCCESS":  0.35, // 多步工具全部成功
	"CORRECTION":          0.30, // 用户纠正了助手
	"TOOL_SUCCESS":        0.20, // 工具成功
	"TURN_COMPLETE":       0.20, // 对话完整完成（基础分 0.15 + 0.20 ≥ Episodic 阈值，正常问答回合都能沉淀）
	"USER_DISLIKE":        -1.0, // 用户表示不满 → 直接抑制
}

// FormationPolicy 确定性打分策略；不调 LLM。
type FormationPolicy struct {
	thresholds Thresholds
}

// NewFormationPolicy 构造策略。
func NewFormationPolicy(t Thresholds) *FormationPolicy {
	if t.Episodic <= 0 {
		t = DefaultThresholds()
	}
	return &FormationPolicy{thresholds: t}
}

var (
	rememberRe = regexp.MustCompile(`(?i)(记住|记得|remember)`)
	preferRe   = regexp.MustCompile(`(?i)(我喜欢|我偏好|prefer|favorite)`)
	// dislikeRe 不含「不要」「报错」——这类词在工作上下文里高频出现
	//（「不要删这个文件」「这个报错…」「不要错把它当故障」），不属于负反馈。
	// 留作匹配明确表达不满的词（不喜欢 / 错误理解 / 别这样 / 不好用 / dislike / wrong）。
	dislikeRe   = regexp.MustCompile(`(?i)(不喜欢|错误|别这样|不好用|dislike|wrong)`)
	correctedRe = regexp.MustCompile(`(?i)(更正|纠正|不对|其实应该)`)
)

// Evaluate 对一次对话打分并决定是否形成记忆。
func (f *FormationPolicy) Evaluate(ctx context.Context, sessionID string, transcript []llm.Message) FormationResult {
	if len(transcript) == 0 {
		return FormationResult{}
	}
	score := f.baseScore(transcript)
	triggers := f.detectTriggers(transcript)
	for _, tr := range triggers {
		score += DefaultTriggers[tr]
	}
	// USER_DISLIKE 一票否决
	if score < 0 {
		return FormationResult{}
	}
	res := FormationResult{}
	if score >= f.thresholds.Episodic {
		res.Episode = &EpisodeProposal{
			SessionID:  sessionID,
			Summary:    f.makeSummary(transcript),
			Transcript: transcript,
			Score:      score,
			Triggers:   triggers,
		}
	}
	if score >= f.thresholds.Semantic {
		res.Semantic = f.extractFacts(sessionID, transcript)
	}
	if score >= f.thresholds.Procedural {
		if pp := f.extractProcedure(transcript); pp != nil {
			res.Procedural = []ProcedureProposal{*pp}
		}
	}
	return res
}

// extractFacts 确定性提取语义事实：用户表达偏好 / 明确要记住 → subject=user。
// sessionID 落 MemoryFactDO.Source，记忆面板据此溯源。
func (f *FormationPolicy) extractFacts(sessionID string, transcript []llm.Message) []FactProposal {
	for _, m := range transcript {
		if m.Role != llm.RoleUser || m.Content == "" {
			continue
		}
		switch {
		case preferRe.MatchString(m.Content):
			return []FactProposal{{Subject: "user", Key: "preference", Value: snippet(m.Content, 200), Confidence: 0.7, SessionID: sessionID}}
		case rememberRe.MatchString(m.Content):
			return []FactProposal{{Subject: "user", Key: "remembered", Value: snippet(m.Content, 200), Confidence: 0.8, SessionID: sessionID}}
		}
	}
	return nil
}

// extractProcedure 确定性提取程序记忆：≥3 次工具调用 → 去重保序形成步骤。
func (f *FormationPolicy) extractProcedure(transcript []llm.Message) *ProcedureProposal {
	var steps []string
	for _, m := range transcript {
		for _, tc := range m.ToolCalls {
			steps = append(steps, tc.Function.Name)
		}
	}
	if len(steps) < 3 {
		return nil
	}
	seen := map[string]bool{}
	unique := steps[:0]
	for _, s := range steps {
		if !seen[s] {
			seen[s] = true
			unique = append(unique, s)
		}
	}
	return &ProcedureProposal{Name: "tool-flow-" + unique[0], Steps: unique}
}

// baseScore 基础分：对话长度 + 轮数。
func (f *FormationPolicy) baseScore(transcript []llm.Message) float64 {
	var userRunes, assistantRunes int
	for _, m := range transcript {
		switch m.Role {
		case llm.RoleUser:
			userRunes += len([]rune(m.Content))
		case llm.RoleAssistant:
			assistantRunes += len([]rune(m.Content))
		}
	}
	score := 0.05 // 基础存在分
	if userRunes > 10 {
		score += 0.05
	}
	if assistantRunes > 20 {
		score += 0.05
	}
	return score
}

// detectTriggers 规则检测触发器。
func (f *FormationPolicy) detectTriggers(transcript []llm.Message) []string {
	var triggers []string
	var userText strings.Builder
	var assistantText strings.Builder
	toolCount := 0
	for _, m := range transcript {
		switch m.Role {
		case llm.RoleUser:
			userText.WriteString(m.Content)
			userText.WriteString("\n")
		case llm.RoleAssistant:
			assistantText.WriteString(m.Content)
			if len(m.ToolCalls) > 0 {
				toolCount += len(m.ToolCalls)
			}
		case llm.RoleTool:
			toolCount++
		}
	}
	u := userText.String()
	if rememberRe.MatchString(u) {
		triggers = append(triggers, "EXPLICIT_REMEMBER")
	}
	if preferRe.MatchString(u) {
		triggers = append(triggers, "PREFERENCE_DETECTED")
	}
	if correctedRe.MatchString(u) {
		triggers = append(triggers, "CORRECTION")
	}
	if dislikeRe.MatchString(u) {
		triggers = append(triggers, "USER_DISLIKE")
	}
	if toolCount >= 3 {
		triggers = append(triggers, "MULTI_STEP_SUCCESS")
	} else if toolCount >= 1 {
		triggers = append(triggers, "TOOL_SUCCESS")
	}
	// 收尾判定同时认半角与全角问号：中文对话几乎只用「？」，
	// 只认半角会让正常中文问答永远拿不到 TURN_COMPLETE，记忆中心因此常年为空。
	if assistantText.Len() > 0 && strings.ContainsAny(u, "??") {
		triggers = append(triggers, "TURN_COMPLETE")
	}
	return triggers
}

// makeSummary v1 模板摘要：首条用户消息 + 末条助手消息 + 工具列表。
func (f *FormationPolicy) makeSummary(transcript []llm.Message) string {
	var firstUser, lastAssistant, toolNames []string
	for _, m := range transcript {
		if m.Role == llm.RoleUser && m.Content != "" && len(firstUser) == 0 {
			firstUser = append(firstUser, snippet(m.Content, 120))
		}
		if m.Role == llm.RoleAssistant && m.Content != "" {
			lastAssistant = []string{snippet(m.Content, 120)}
		}
		for _, tc := range m.ToolCalls {
			toolNames = append(toolNames, tc.Function.Name)
		}
	}
	var sb strings.Builder
	if len(firstUser) > 0 {
		sb.WriteString("用户: " + firstUser[0] + "\n")
	}
	if len(lastAssistant) > 0 {
		sb.WriteString("助手: " + lastAssistant[0] + "\n")
	}
	if len(toolNames) > 0 {
		sb.WriteString("工具: " + strings.Join(toolNames, ", "))
	}
	return strings.TrimSpace(sb.String())
}
