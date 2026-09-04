package nodes

import (
	"context"
	"strings"
	"time"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"

	"github.com/dop251/goja"
)

// CodeNode JavaScript 代码节点（goja 沙箱；goja 最低要求 Go 1.16，1.24 可用）。
//
// 沙箱边界：goja 是纯语言运行时——无 require / process / fs / network；
// 全局 $input = 上游输出 map；结果写入全局 output 变量（或作为末尾表达式返回）。
// 超时 / ctx 取消经 vm.Interrupt 打断（默认 10s，上限 60s；goja 无抢占，靠中断信号）。
type CodeNode struct{}

// NewCodeNode 构造；自动注册 schema（覆盖 builtin 版本）。
func NewCodeNode() *CodeNode {
	n := &CodeNode{}
	Register(n)
	return n
}

// Type 实现 Node 接口。
func (n *CodeNode) Type() domain.WorkflowNodeType { return domain.WorkflowNodeCode }

// Schema 实现 Node 接口。
func (n *CodeNode) Schema() Schema {
	return Schema{
		Type:     domain.WorkflowNodeCode,
		Required: []string{"script"},
		Optional: []string{"timeout_ms"},
		Fields: []FieldSpec{
			{Name: "script", Label: "JavaScript 脚本", Type: FieldTextarea, Required: true,
				Description: "全局 $input 为上游输出；结果写入全局 output 变量（或作为末尾表达式返回）"},
			{Name: "timeout_ms", Label: "超时（毫秒）", Type: FieldNumber, Default: 10000,
				Description: "执行超时，1000~60000"},
		},
	}
}

// 默认/上限执行参数。
const (
	codeDefaultTimeoutMS = 10_000
	codeMaxTimeoutMS     = 60_000
)

// Execute 跑一段 JS：$input 注入 → output 全局或 IIFE 返回值为节点输出。
func (n *CodeNode) Execute(ctx context.Context, inputs map[string]any, cfg map[string]any, upstream map[string]any) (map[string]any, error) {
	script, _ := cfg["script"].(string)
	if strings.TrimSpace(script) == "" {
		return nil, pkg.New(9104, "Code 节点缺少 cfg.script", "")
	}
	timeoutMS := codeDefaultTimeoutMS
	if v, ok := cfg["timeout_ms"].(float64); ok && v >= 1000 && v <= codeMaxTimeoutMS {
		timeoutMS = int(v)
	}

	vm := goja.New()
	vm.Set("$input", upstream)

	// 超时 / 取消 → Interrupt（InterruptedError 从 RunString 冒出）
	done := make(chan struct{})
	defer close(done)
	timer := time.AfterFunc(time.Duration(timeoutMS)*time.Millisecond, func() { vm.Interrupt(nil) })
	defer timer.Stop()
	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt(nil)
		case <-done:
		}
	}()

	wrapped := "(function(){\n" + script + "\n})()"
	res, err := vm.RunString(wrapped)
	if err != nil {
		if _, interrupted := err.(*goja.InterruptedError); interrupted {
			return nil, pkg.New(9105, "code execute timeout or cancelled", "")
		}
		return nil, pkg.New(9105, "code execute failed: "+err.Error(), "")
	}

	var out any
	if g := vm.Get("output"); g != nil && !goja.IsUndefined(g) && !goja.IsNull(g) {
		out = g.Export()
	} else if res != nil {
		out = res.Export()
	}
	switch v := out.(type) {
	case nil:
		return map[string]any{}, nil
	case map[string]any:
		return v, nil
	default:
		return map[string]any{"value": v}, nil
	}
}
