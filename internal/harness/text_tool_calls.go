package harness

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"WorkBaby/internal/llm"
)

// 文本工具调用兜底：部分 OpenAI 兼容端点不支持结构化 tool_calls，会把调用意图写进正文。
//
// 按 XML 标签 → Markdown 代码块 → 裸 JSON 三种形态解析（name/tool、input/arguments 别名都认）；
// 只认本次 run 暴露的工具名（defs 白名单），单轮最多解析 maxTextToolCalls 个。
type textToolCall struct {
	Name      string         `json:"name"`
	Tool      string         `json:"tool"`
	Input     map[string]any `json:"input"`
	Arguments map[string]any `json:"arguments"`
}

// maxTextToolCalls 单轮正文兜底最多解析的工具调用数。
const maxTextToolCalls = 8

var (
	fencedJSONBlock  = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)\\s*```")
	xmlToolCallBlock = regexp.MustCompile(`(?is)<tool_?call(?:\s+name\s*=\s*"([^"]+)")?\s*>(.*?)</tool_?call>`)
)

// textCandidate 一个待解析的调用候选；name 非空表示来自 XML 标签的 name 属性。
type textCandidate struct {
	body string
	name string
}

// parseTextToolCalls 从正文提取文本形态的 tool call；无命中返回 nil。
func parseTextToolCalls(content string, defs []llm.ToolDefinition) []llm.NormalizedToolCall {
	if strings.TrimSpace(content) == "" || len(defs) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(defs))
	for _, d := range defs {
		allowed[d.Name] = true
	}
	var out []llm.NormalizedToolCall
	for _, cand := range textCandidates(content) {
		if len(out) >= maxTextToolCalls {
			break
		}
		var tc textToolCall
		if err := json.Unmarshal([]byte(cand.body), &tc); err != nil {
			continue
		}
		name := tc.Name
		if name == "" {
			name = tc.Tool
		}
		if name == "" {
			name = cand.name
		}
		if !allowed[name] {
			continue
		}
		args := tc.Input
		if args == nil {
			args = tc.Arguments
		}
		if args == nil {
			args = map[string]any{}
		}
		bs, err := json.Marshal(args)
		if err != nil {
			continue
		}
		// ID 带序号：同轮多个调用（含同名）不撞号
		out = append(out, llm.NormalizedToolCall{
			ID:        fmt.Sprintf("text-call-%d-%s", len(out)+1, name),
			Name:      name,
			Arguments: bs,
		})
	}
	return out
}

// textCandidates 正文中的调用候选：XML 标签 → 代码块 → 整段裸对象。
func textCandidates(text string) []textCandidate {
	var out []textCandidate
	for _, m := range xmlToolCallBlock.FindAllStringSubmatch(text, -1) {
		if len(m) > 2 && strings.TrimSpace(m[2]) != "" {
			out = append(out, textCandidate{body: strings.TrimSpace(m[2]), name: strings.TrimSpace(m[1])})
		}
	}
	for _, m := range fencedJSONBlock.FindAllStringSubmatch(text, -1) {
		if len(m) > 1 && strings.TrimSpace(m[1]) != "" {
			out = append(out, textCandidate{body: strings.TrimSpace(m[1])})
		}
	}
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		out = append(out, textCandidate{body: trimmed})
	}
	return out
}

// nestedToolCallMarkers 参数文本中的伪调用标记（小写比较）。
// 工具结果（网页 / 文件内容）可能携带诱导性文本，模型原样回传进参数即成注入载体。
var nestedToolCallMarkers = []string{
	"<tool_call", "</tool_call", "<toolcall",
	`"tool_calls"`, `"tool_call"`,
}

// HasNestedToolCallMarker 检测参数文本里是否嵌着工具调用标记（提示注入防护）。
// 命中即拒绝执行：正常参数（路径 / 命令 / 查询语句）不会包含这些形态，
// 误杀率远低于把网页里的伪调用当真执行的风险。
func HasNestedToolCallMarker(args string) bool {
	lower := strings.ToLower(args)
	for _, m := range nestedToolCallMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}
