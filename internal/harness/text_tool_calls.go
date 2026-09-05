package harness

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"WorkBaby/internal/llm"
)

// text_tool_calls.go 文本工具调用兜底：部分 OpenAI 兼容端点（及不少国产/本地模型）
// 不支持结构化 tool_calls，会把调用意图写成正文。
//
// 解析形态（按优先级）：
//   - XML 标签：<tool_call name="x">{...}</tool_call> 或 <tool_call>{"name":"x",...}</tool_call>
//   - Markdown 代码块：```json { ... } ```
//   - 整段正文就是一个裸 JSON 对象。
//
// 参数别名：name/tool、input/arguments 都认，兼容各家 prompt 约定。
//
// 安全约束：
//   - 只有 JSON 携带「本次 run 暴露给 LLM 的工具名」（defs 白名单）才解析为调用，
//     普通的 JSON 代码示例 / 数据回答不受影响；
//   - 单轮最多解析 maxTextToolCalls 个，防止模型刷屏式误触发。
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
