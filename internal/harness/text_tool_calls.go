package harness

import (
	"encoding/json"
	"regexp"
	"strings"

	"WorkBaby/internal/llm"
)

// text_tool_calls.go 文本工具调用兜底：部分 OpenAI 兼容端点把 tool call 当正文返回。
//
// 解析形态：
//   - ```json 代码块内的 JSON 对象；
//   - 整段正文就是一个裸 JSON 对象。
//
// 安全约束：只有当 JSON 携带「本次 run 暴露给 LLM 的工具名」（defs 白名单）时才解析为调用，
// 普通的 JSON 代码示例 / 数据回答不受影响。
type textToolCall struct {
	Name      string         `json:"name"`
	Tool      string         `json:"tool"`
	Input     map[string]any `json:"input"`
	Arguments map[string]any `json:"arguments"`
}

var fencedJSONBlock = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)\\s*```")

// parseTextToolCalls 从正文提取文本形态的 tool call；无命中返回 nil。
func parseTextToolCalls(content string, defs []llm.ToolDefinition) []llm.NormalizedToolCall {
	if strings.TrimSpace(content) == "" || len(defs) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(defs))
	for _, d := range defs {
		allowed[d.Name] = true
	}
	for _, cand := range jsonCandidates(content) {
		var tc textToolCall
		if err := json.Unmarshal([]byte(cand), &tc); err != nil {
			continue
		}
		name := tc.Name
		if name == "" {
			name = tc.Tool
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
		return []llm.NormalizedToolCall{{ID: "text-call-" + name, Name: name, Arguments: bs}}
	}
	return nil
}

// jsonCandidates 正文中的 JSON 候选：代码块优先，其次整段裸对象。
func jsonCandidates(text string) []string {
	var out []string
	for _, m := range fencedJSONBlock.FindAllStringSubmatch(text, -1) {
		if len(m) > 1 && strings.TrimSpace(m[1]) != "" {
			out = append(out, strings.TrimSpace(m[1]))
		}
	}
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		out = append(out, trimmed)
	}
	return out
}
