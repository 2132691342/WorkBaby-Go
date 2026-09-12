package service

import (
	"context"
	"encoding/json"
	"strings"

	"WorkBaby/internal/capability"
	"WorkBaby/internal/domain"
	"WorkBaby/internal/llm"
	"WorkBaby/internal/llm/registry"
	"WorkBaby/internal/pkg"
)

// inboxExtractTimeoutSec LLM 抽取的墙钟上限由能力沉淀超时兜底；此处只控制单次请求输出规模。
const (
	inboxExtractMaxFacts = 3
	inboxExtractMaxProc  = 1
	inboxExtractMaxSkill = 1
	inboxExtractMaxRunes = 6000 // 送入抽取的对话正文上限
)

// NewInboxExtractor 构造 run 终局候选抽取器：用本 run 的 provider/model 做一次 strict-JSON 抽取。
// 只产候选不落正式库（合入由人审收件箱决定）；模型无把握时返回空数组；失败静默不影响 run 主链路。
func NewInboxExtractor(reg *registry.Registry) capability.Extractor {
	return func(ctx context.Context, c *capability.CaptureCtx) ([]domain.InboxCandidate, error) {
		if reg == nil || c == nil || c.ProviderID == "" || c.Model == "" {
			return nil, nil
		}
		prov, err := reg.Get(c.ProviderID)
		if err != nil {
			return nil, nil // provider 未就绪：静默跳过
		}
		body := renderTranscriptForExtract(c.Transcript)
		if strings.TrimSpace(body) == "" {
			return nil, nil
		}
		temp := 0.2
		maxTok := 1200
		resp, err := prov.Chat(ctx, &llm.ChatRequest{
			Model:       c.Model,
			Messages:    []*llm.Message{llm.SystemMessage(inboxExtractPrompt), llm.UserMessage(body)},
			Temperature: &temp,
			MaxTokens:   &maxTok,
		})
		if err != nil || resp == nil {
			pkg.L.Warn("inbox extract failed", "session", c.SessionID, "err", errString(err))
			return nil, nil
		}
		return parseInboxCandidates(resp.Message.Content), nil
	}
}

const inboxExtractPrompt = `你是记忆整理助手。阅读下面一段「用户 ⇄ 助手」的对话，抽取值得长期保留的信息，只输出 JSON。
规则：
- 只输出 JSON 对象，不要解释、不要 Markdown 代码块。
- 宁缺毋滥：没有把握就返回空数组。
- facts：用户偏好或稳定事实，最多 3 条；subject 通常为 "user"；key 用简短短语，value 具体明确。
- procedures：可复用的多步操作流程（≥3 步且本轮执行成功才提），最多 1 条。
- skills：可沉淀为技能的稳定工作流，最多 1 条；body 是 Markdown 操作指导；name 用 kebab-case。
- 不要编造对话中不存在的信息。
输出结构：
{"facts":[{"subject":"user","key":"","value":"","confidence":0.8}],"procedures":[{"name":"","steps":[""]}],"skills":[{"name":"kebab-case","description":"","when_to_use":"","body":""}]}`

// renderTranscriptForExtract 把对话渲染成抽取输入（逐条截断 + 总量上限）。
func renderTranscriptForExtract(msgs []llm.Message) string {
	var sb strings.Builder
	for i := range msgs {
		m := msgs[i]
		content := strings.TrimSpace(m.Content)
		if content == "" && len(m.ToolCalls) == 0 {
			continue
		}
		sb.WriteString(roleLabel(m.Role))
		sb.WriteString(": ")
		sb.WriteString(pkg.TruncateRunes(content, 600))
		for _, tc := range m.ToolCalls {
			sb.WriteString(" [调用工具 " + tc.Function.Name + "]")
		}
		sb.WriteString("\n")
		if len([]rune(sb.String())) > inboxExtractMaxRunes {
			break
		}
	}
	s := sb.String()
	if r := []rune(s); len(r) > inboxExtractMaxRunes {
		s = string(r[:inboxExtractMaxRunes])
	}
	return strings.TrimSpace(s)
}

func roleLabel(role llm.RoleType) string {
	switch role {
	case llm.RoleUser:
		return "用户"
	case llm.RoleAssistant:
		return "助手"
	case llm.RoleTool:
		return "工具"
	default:
		return "系统"
	}
}

// extractPayload 抽取响应结构。
type extractPayload struct {
	Facts []struct {
		Subject    string  `json:"subject"`
		Key        string  `json:"key"`
		Value      string  `json:"value"`
		Confidence float64 `json:"confidence"`
	} `json:"facts"`
	Procedures []struct {
		Name  string   `json:"name"`
		Steps []string `json:"steps"`
	} `json:"procedures"`
	Skills []struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		WhenToUse   string   `json:"when_to_use"`
		Body        string   `json:"body"`
		AllowedTool []string `json:"allowed_tools"`
	} `json:"skills"`
}

// parseInboxCandidates 解析模型输出为候选；宽容处理代码块包裹与截断 JSON。
func parseInboxCandidates(raw string) []domain.InboxCandidate {
	obj := extractJSONObject(raw)
	if obj == "" {
		return nil
	}
	var p extractPayload
	if err := json.Unmarshal([]byte(obj), &p); err != nil {
		return nil
	}
	out := make([]domain.InboxCandidate, 0, len(p.Facts)+len(p.Procedures)+len(p.Skills))
	for i, f := range p.Facts {
		if i >= inboxExtractMaxFacts || strings.TrimSpace(f.Key) == "" || strings.TrimSpace(f.Value) == "" {
			continue
		}
		subject := strings.TrimSpace(f.Subject)
		if subject == "" {
			subject = "user"
		}
		conf := f.Confidence
		if conf <= 0 || conf > 1 {
			conf = 0.7
		}
		out = append(out, domain.InboxCandidate{
			Kind: domain.InboxKindFact, Title: subject + ":" + f.Key, Source: domain.InboxSourceLLM, Confidence: conf,
			Payload: domain.InboxFactPayload{Subject: subject, Key: f.Key, Value: f.Value, Confidence: conf},
		})
	}
	for i, pr := range p.Procedures {
		if i >= inboxExtractMaxProc || strings.TrimSpace(pr.Name) == "" || len(pr.Steps) == 0 {
			continue
		}
		out = append(out, domain.InboxCandidate{
			Kind: domain.InboxKindProcedure, Title: pr.Name, Source: domain.InboxSourceLLM, Confidence: 0.6,
			Payload: domain.InboxProcedurePayload{Name: pr.Name, Steps: pr.Steps},
		})
	}
	for i, sk := range p.Skills {
		if i >= inboxExtractMaxSkill || strings.TrimSpace(sk.Name) == "" || strings.TrimSpace(sk.Body) == "" {
			continue
		}
		out = append(out, domain.InboxCandidate{
			Kind: domain.InboxKindSkill, Title: sk.Name, Source: domain.InboxSourceLLM, Confidence: 0.6,
			Payload: domain.InboxSkillPayload{
				Name: sk.Name, Description: sk.Description, WhenToUse: sk.WhenToUse, Body: sk.Body, AllowedTools: sk.AllowedTool,
			},
		})
	}
	return out
}

// extractJSONObject 取首个 { 到末个 } 之间的子串（容忍代码块包裹与前后说明文字）。
func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
