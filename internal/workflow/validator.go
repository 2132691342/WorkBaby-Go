package workflow

import (
	"fmt"
	"regexp"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/workflow/nodes"
)

var idRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Validate 图定义校验：ID 合法唯一、deps 引用存在、环检测、必填配置齐全。
//
// 调用时机：保存工作流（service 层）+ 执行前（防 DB 被外部改动）。
func Validate(g *Graph) error {
	if g == nil {
		return pkg.New(9102, "图定义为空", "")
	}
	if len(g.Nodes) == 0 {
		return pkg.New(9102, "工作流至少需要一个节点", "")
	}

	seen := make(map[string]bool, len(g.Nodes))
	for _, n := range g.Nodes {
		if !idRe.MatchString(n.ID) {
			return pkg.New(9102, fmt.Sprintf("节点 ID 不合法: %q", n.ID), "")
		}
		if seen[n.ID] {
			return pkg.New(9102, fmt.Sprintf("节点 ID 重复: %q", n.ID), "")
		}
		seen[n.ID] = true
		if !n.Type.IsValid() {
			return pkg.New(9104, fmt.Sprintf("未知节点类型: %q", n.Type), "")
		}
	}

	// deps 引用存在
	for _, n := range g.Nodes {
		for _, d := range n.Deps {
			if !seen[d] {
				return pkg.New(9102, fmt.Sprintf("节点 %s 的依赖 %q 不存在", n.ID, d), "")
			}
		}
	}

	// 节点 Inputs 引用：上游存在（解析阶段不强制 field 存在性，运行时再 nil 兜底）
	for _, n := range g.Nodes {
		for k, ref := range n.Inputs {
			head := ref
			for i := 0; i < len(ref); i++ {
				if ref[i] == '.' {
					head = ref[:i]
					break
				}
			}
			if !seen[head] {
				return pkg.New(9102, fmt.Sprintf("节点 %s 的输入 %s 引用未知节点 %q", n.ID, k, head), "")
			}
		}
	}

	// 环检测
	if _, err := TopologicalLayers(g); err != nil {
		return err
	}

	// 节点必填配置（Schema 声明）
	for _, n := range g.Nodes {
		sch := nodes.SchemaFor(n.Type)
		if sch.Required == nil {
			continue
		}
		for _, req := range sch.Required {
			if _, ok := n.Config[req]; !ok {
				return pkg.New(9104, fmt.Sprintf("节点 %s 缺少必填配置: %s", n.ID, req), "")
			}
		}
	}

	// 分支归属：声明了 branch 的节点必须有一条指向 condition 上游的边（否则执行时会被 NodeActive 保守跳过）
	for _, n := range g.Nodes {
		if n.Branch == "" {
			continue
		}
		hasCondDep := false
		for _, d := range n.Deps {
			if dep := FindNode(g, d); dep != nil && dep.Type == domain.WorkflowNodeCondition {
				hasCondDep = true
				break
			}
		}
		if !hasCondDep {
			return pkg.New(9104, fmt.Sprintf("节点 %s 声明了分支 %q，但上游没有 condition 节点", n.ID, n.Branch), "")
		}
	}

	// Graph.Outputs 引用存在
	for k, ref := range g.Outputs {
		head := ref
		for i := 0; i < len(ref); i++ {
			if ref[i] == '.' {
				head = ref[:i]
				break
			}
		}
		if !seen[head] {
			return pkg.New(9102, fmt.Sprintf("outputs %s 引用未知节点 %q", k, head), "")
		}
	}

	return nil
}
