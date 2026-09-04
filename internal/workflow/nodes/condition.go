package nodes

import (
	"context"
	"fmt"

	"github.com/expr-lang/expr"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
)

// ConditionNode 对 expr 表达式求值 → 输出 branch 标签。
//
// 表达式用 expr-lang/expr 求值（未定义变量取 nil 不报错）；返回值映射：
// 字符串 → 原样作分支标签；bool → "true"/"false"；数字 → 非零 "true"。
type ConditionNode struct{}

// NewConditionNode 构造；自动注册 schema。
func NewConditionNode() *ConditionNode {
	n := &ConditionNode{}
	Register(n)
	return n
}

// Type 实现 Node 接口。
func (n *ConditionNode) Type() domain.WorkflowNodeType { return domain.WorkflowNodeCondition }

// Schema 实现 Node 接口。
func (n *ConditionNode) Schema() Schema {
	return Schema{
		Type:     domain.WorkflowNodeCondition,
		Required: []string{"expression"},
	}
}

// Execute：合并 inputs + upstream 作为变量表，求值后映射为 branch 标签。
func (n *ConditionNode) Execute(ctx context.Context, inputs map[string]any, cfg map[string]any, upstream map[string]any) (map[string]any, error) {
	src, _ := cfg["expression"].(string)
	if src == "" {
		return nil, pkg.New(9104, "Condition 节点缺少 cfg.expression", "")
	}
	vars := make(map[string]any, len(inputs)+len(upstream))
	for k, v := range inputs {
		vars[k] = v
	}
	for k, v := range upstream {
		vars[k] = v
	}
	v, err := evalExpr(src, vars)
	if err != nil {
		return nil, pkg.Wrap(9105, "condition 表达式求值失败", err)
	}
	branch, err := branchOf(v)
	if err != nil {
		return nil, err
	}
	return map[string]any{"branch": branch, "raw": v}, nil
}

// evalExpr 编译并求值；AllowUndefinedVariables 让未产出的上游变量取 nil 而非编译失败。
func evalExpr(src string, vars map[string]any) (any, error) {
	program, err := expr.Compile(src, expr.Env(map[string]any{}), expr.AllowUndefinedVariables())
	if err != nil {
		return nil, err
	}
	return expr.Run(program, vars)
}

// branchOf 把求值结果映射为分支标签。
func branchOf(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case bool:
		if x {
			return "true", nil
		}
		return "false", nil
	case int:
		return boolBranch(x != 0), nil
	case int64:
		return boolBranch(x != 0), nil
	case float64:
		return boolBranch(x != 0), nil
	}
	return "", pkg.New(9105, "condition 表达式返回类型不支持", fmt.Sprintf("got %T", v))
}

func boolBranch(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
