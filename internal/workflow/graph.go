// Package workflow 实现 DAG 工作流引擎：图定义、变量模板、拓扑分层、节点执行、落库、事件。
// 边界：不依赖 service / wails / api；LLM/Tool/Channel/Human 通过接口注入。
package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
)

// NodeType 是 domain.WorkflowNodeType 的本地别名；让 Graph JSON 的 type 字段直接用枚举值。
type NodeType = domain.WorkflowNodeType

// Graph 工作流图定义（解析后形态；DO.Graph 字段是 JSON 字符串）。
//
// 顶层字段：name / inputs（key→type） / nodes[] / outputs（key→"nodeId.field"）。
type Graph struct {
	Name    string            `json:"name"`
	Inputs  map[string]string `json:"inputs,omitempty"` // key → 类型名（"string"/"int"/...，仅声明，不强制）
	Nodes   []*NodeDef        `json:"nodes"`
	Outputs map[string]string `json:"outputs,omitempty"` // key → "nodeID" 或 "nodeId.field"
}

// Pos 画布坐标（编辑器持久化；执行器忽略该字段）。
type Pos struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// NodeDef 单节点定义。
//
// Deps 是上游节点 ID；Inputs 是变量引用（key→"nodeId.field"）；Branch 是 condition 节点输出分支归属；
// Pos 是编辑器画布坐标（仅可视化持久化，执行时不用）。
type NodeDef struct {
	ID     string            `json:"id"`
	Type   NodeType          `json:"type"`
	Deps   []string          `json:"deps,omitempty"`
	Config map[string]any    `json:"config,omitempty"`
	Inputs map[string]string `json:"inputs,omitempty"` // 入参 key → "nodeId.field" 引用
	Branch string            `json:"branch,omitempty"` // 命中 condition 哪个分支（"true"/"false"/...）
	Pos    *Pos              `json:"pos,omitempty"`
}

// ParseGraph 解析 Graph JSON。
func ParseGraph(raw string) (*Graph, error) {
	if raw == "" {
		return nil, pkg.New(9102, "图定义为空白", "")
	}
	var g Graph
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		return nil, pkg.Wrap(9102, "图定义 JSON 解析失败", err)
	}
	return &g, nil
}

// RenderTemplate 把 tpl 中的 {{#id#}} / {{#id.field#}} 替换为 outputs[id] / outputs[id][field]。
//
// inputs 是 nodeID → 该节点完整输出的 map；只读，不修改原数据。
func RenderTemplate(tpl string, outputs map[string]map[string]any) string {
	if tpl == "" {
		return ""
	}
	// 简单实现：手动扫描，比 regex 在 # }} 这类 ASCII 上更稳。
	var sb strings.Builder
	i := 0
	for i < len(tpl) {
		// 找 {{#
		j := strings.Index(tpl[i:], "{{#")
		if j < 0 {
			sb.WriteString(tpl[i:])
			break
		}
		sb.WriteString(tpl[i : i+j])
		i += j
		// 找 #}}
		k := strings.Index(tpl[i+3:], "#}}")
		if k < 0 {
			sb.WriteString(tpl[i:])
			break
		}
		ref := tpl[i+3 : i+3+k]
		parts := strings.SplitN(ref, ".", 2)
		nodeOut, ok := outputs[parts[0]]
		if !ok {
			// 引用不存在，保留原文
			sb.WriteString(tpl[i : i+3+k+3])
			i += 3 + k + 3
			continue
		}
		var replacement string
		if len(parts) == 1 {
			replacement = fmt.Sprintf("%v", nodeOut)
		} else {
			v, ok := digPath(nodeOut, parts[1])
			if !ok {
				replacement = ""
			} else {
				replacement = fmt.Sprintf("%v", v)
			}
		}
		sb.WriteString(replacement)
		i += 3 + k + 3
	}
	return sb.String()
}

// RenderConfigMap 对 cfg 中所有 string 值做模板渲染（递归 map；遇到非 string 跳过）。
func RenderConfigMap(cfg map[string]any, outputs map[string]map[string]any) map[string]any {
	if cfg == nil {
		return nil
	}
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		switch x := v.(type) {
		case string:
			out[k] = RenderTemplate(x, outputs)
		case map[string]any:
			out[k] = RenderConfigMap(x, outputs)
		case []any:
			arr := make([]any, len(x))
			for i := range x {
				if s, ok := x[i].(string); ok {
					arr[i] = RenderTemplate(s, outputs)
				} else {
					arr[i] = x[i]
				}
			}
			out[k] = arr
		default:
			out[k] = v
		}
	}
	return out
}

// digPath map / slice 嵌套点路径取值（仅纯数据结构，不调方法）。
func digPath(v any, path string) (any, bool) {
	segs := strings.Split(path, ".")
	cur := v
	for _, s := range segs {
		switch x := cur.(type) {
		case map[string]any:
			next, ok := x[s]
			if !ok {
				return nil, false
			}
			cur = next
		case []any:
			idx := 0
			if _, err := fmt.Sscanf(s, "%d", &idx); err != nil {
				return nil, false
			}
			if idx < 0 || idx >= len(x) {
				return nil, false
			}
			cur = x[idx]
		default:
			return nil, false
		}
	}
	return cur, true
}

// TopologicalLayers 拓扑分层（Kahn），返回每层的节点 ID 列表；环返回 ErrWorkflowCycle。
func TopologicalLayers(g *Graph) ([][]string, error) {
	inDeg := map[string]int{}
	children := map[string][]string{}
	for _, n := range g.Nodes {
		inDeg[n.ID] = 0
	}
	for _, n := range g.Nodes {
		for _, d := range n.Deps {
			children[d] = append(children[d], n.ID)
			inDeg[n.ID]++
		}
	}
	var layers [][]string
	current := make([]string, 0)
	for id, d := range inDeg {
		if d == 0 {
			current = append(current, id)
		}
	}
	for len(current) > 0 {
		layers = append(layers, current)
		next := make([]string, 0)
		for _, id := range current {
			for _, ch := range children[id] {
				inDeg[ch]--
				if inDeg[ch] == 0 {
					next = append(next, ch)
				}
			}
		}
		current = next
	}
	total := 0
	for _, l := range layers {
		total += len(l)
	}
	if total != len(g.Nodes) {
		return nil, pkg.New(9103, "工作流存在环依赖", "")
	}
	return layers, nil
}

// ResolveUpstream 把节点 Inputs 中声明的引用（"nodeID" 或 "nodeId.field"）从 outputs 提取为入参 map。
func ResolveUpstream(nd *NodeDef, outputs map[string]map[string]any) map[string]any {
	if len(nd.Inputs) == 0 {
		return map[string]any{}
	}
	resolved := make(map[string]any, len(nd.Inputs))
	for k, ref := range nd.Inputs {
		parts := strings.SplitN(ref, ".", 2)
		nodeOut, ok := outputs[parts[0]]
		if !ok {
			resolved[k] = nil
			continue
		}
		if len(parts) == 1 {
			resolved[k] = nodeOut
			continue
		}
		v, _ := digPath(nodeOut, parts[1])
		resolved[k] = v
	}
	return resolved
}

// NodeActive 决定 condition 分支：节点未声明 branch 永远 active；
// 若声明则需要上游最近的 condition 节点输出 branch 等于该值。
//
// outputs 当前已执行节点的输出；conditionOutputs 是 condition 节点输出缓存（节点 ID → branch）。
func NodeActive(nd *NodeDef, conditionOutputs map[string]string) bool {
	if nd.Branch == "" {
		return true
	}
	// 在 deps 中找一个 condition 节点，看它的 branch 输出。
	for _, d := range nd.Deps {
		if b, ok := conditionOutputs[d]; ok {
			return b == nd.Branch
		}
	}
	// 没有任何 condition 上游 → 视为不命中（保守跳过）
	return false
}

// NodeRunnable 运行期活性判定（分支跳过传播 + 汇合语义）：
//
//  1. 自身 branch 不命中 → 跳过（NodeActive 保守语义）；
//  2. 无上游 → 恒运行；
//  3. 全部上游被跳过 → 整条分支路径不成立，**跳过传播**给下游
//     （修复深分支下「父被跳过、无独立 branch 的子仍执行」的错误）；
//  4. 至少一个上游存活（执行成功或为 condition）→ 运行。
//     多分支汇合即由此成立：condition 恒执行，任一分支存活即可重新汇入主流程。
func NodeRunnable(nd *NodeDef, conditionOutputs map[string]string, skipped map[string]bool) bool {
	if nd.Branch != "" && !NodeActive(nd, conditionOutputs) {
		return false
	}
	if len(nd.Deps) == 0 {
		return true
	}
	for _, d := range nd.Deps {
		if !skipped[d] {
			return true
		}
	}
	return false
}

// FindNode 按 ID 查找 NodeDef。
func FindNode(g *Graph, id string) *NodeDef {
	for _, n := range g.Nodes {
		if n.ID == id {
			return n
		}
	}
	return nil
}
