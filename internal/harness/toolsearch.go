package harness

// 工具折叠激活（deferred tools + tool_search）：工具 schema 会吃 prompt token，
// 超预算的工具折叠为「按需检索激活」而非静默丢弃——只暴露 tool_search，
// 激活的工具定义从下一轮起进入请求且执行侧放行。激活态为 run 内语义。

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"WorkBaby/internal/llm"
)

// ToolSearchName 虚拟工具名（不注册进 tool.Registry——由工具链在解析层之前拦截执行）。
const ToolSearchName = "tool_search"

// toolSearchMaxActivate 单次检索最多激活的工具数（防止一次激活把预算吃回去）。
const toolSearchMaxActivate = 8

// ToolSearchDef 虚拟工具定义（service 在折叠时追加进暴露清单）。
func ToolSearchDef() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        ToolSearchName,
		Description: "搜索并激活更多工具。当需要的工具不在当前可用列表时，用它按名称或用途检索；命中的工具下一轮起可直接调用。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "工具名或用途关键词（如 mail / 截图 / 日历）"},
			},
			"required": []string{"query"},
		},
	}
}

// WithDeferredTools 启用折叠激活：hidden 为被预算裁掉的工具定义（仍注册在 Registry，可执行）。
func (r *Runner) WithDeferredTools(hidden []llm.ToolDefinition) *Runner {
	r.hiddenDefs = hidden
	return r
}

// exposedDefs 当前应暴露给模型的全部定义：基础清单 + 已激活的隐藏工具。
func (r *Runner) exposedDefs() []llm.ToolDefinition {
	r.loopMu.Lock()
	defer r.loopMu.Unlock()
	if len(r.activatedHidden) == 0 {
		return r.toolDefs
	}
	out := make([]llm.ToolDefinition, 0, len(r.toolDefs)+len(r.activatedHidden))
	out = append(out, r.toolDefs...)
	out = append(out, r.activatedHidden...)
	return out
}

// activateDeferred 按检索词激活隐藏工具（名称/描述子串匹配，相关性排序，条数封顶）。
// 返回本次激活的工具名。loopMu 保护（tool_search 只读可并行，激活可能并发发生）。
func (r *Runner) activateDeferred(query string) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	type hit struct {
		idx      int
		def      llm.ToolDefinition
		relevant int
	}
	r.loopMu.Lock()
	defer r.loopMu.Unlock()
	var hits []hit
	kept := make([]llm.ToolDefinition, 0, len(r.hiddenDefs))
	for _, d := range r.hiddenDefs {
		name := strings.ToLower(d.Name)
		desc := strings.ToLower(d.Description)
		score := 0
		switch {
		case strings.Contains(name, q):
			score = 2
		case strings.Contains(desc, q):
			score = 1
		}
		if score > 0 {
			hits = append(hits, hit{def: d, relevant: score})
		} else {
			kept = append(kept, d)
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].relevant > hits[j].relevant })
	activated := make([]string, 0, len(hits))
	for _, h := range hits {
		if len(activated) >= toolSearchMaxActivate {
			kept = append(kept, h.def)
			continue
		}
		activated = append(activated, h.def.Name)
		r.activatedHidden = append(r.activatedHidden, h.def)
	}
	r.hiddenDefs = kept
	return activated
}

// layerToolSearch 虚拟工具执行层：挂在解析层之前（tool_search 不在 Registry）。
func (r *Runner) layerToolSearch(tc *toolCallCtx) *llm.Message {
	if tc.call.Name != ToolSearchName {
		return nil
	}
	var args struct {
		Query string `json:"query"`
	}
	_ = json.Unmarshal(tc.call.Arguments, &args)
	activated := r.activateDeferred(args.Query)
	content := map[string]any{
		"activated": activated,
		"hint":      "activated tools are callable from the next turn; search again if the tool you need is missing",
	}
	if len(activated) == 0 {
		content["hint"] = "no tool matched; try broader keywords"
	}
	bs, err := json.Marshal(content)
	if err != nil {
		bs = []byte(fmt.Sprintf(`{"activated":[]}`))
	}
	r.sink.Emit(Event{Kind: EventToolResult, RunID: tc.runID, SessionID: tc.sessionID, Turn: tc.turn, Payload: ToolResultPayload{
		ToolCallID: tc.call.ID,
		Name:       ToolSearchName,
		Content:    string(bs),
		DurationMs: 0,
		UIHint:     "tool_search",
	}})
	return llm.ToolMessage(tc.call.ID, tc.call.Name, string(bs))
}
