package workflow

import (
	"strings"
	"testing"

	"WorkBaby/internal/domain"
)

// graph_test.go 覆盖 DAG 图语义：分支路由活性、拓扑分层、变量渲染。
// 这些是执行器的地基：分层错会死等，路由错会漏跑/误跑分支。

// TestValidateCycle 环依赖必须被拒绝（执行器无法拓扑分层会无限等待）。
func TestValidateCycle(t *testing.T) {
	g := &Graph{Nodes: []*NodeDef{
		{ID: "a", Type: domain.WorkflowNodeHTTP, Deps: []string{"b"}, Config: map[string]any{"url": "x"}},
		{ID: "b", Type: domain.WorkflowNodeHTTP, Deps: []string{"a"}, Config: map[string]any{"url": "y"}},
	}}
	err := Validate(g)
	if err == nil || !strings.Contains(err.Error(), "环") {
		t.Fatalf("want cycle error, got %v", err)
	}
}




// TestNodeRunnableSkipPropagation 唯一上游被跳过 → 跳过向下传播。
func TestNodeRunnableSkipPropagation(t *testing.T) {
	c := &NodeDef{ID: "c", Type: "http", Deps: []string{"a2"}}
	if NodeRunnable(c, nil, map[string]bool{"a2": true}) {
		t.Fatalf("node whose only upstream skipped should propagate skip")
	}
}

// TestNodeRunnableJoinAlive 汇合节点只要有一路上游存活即运行。
func TestNodeRunnableJoinAlive(t *testing.T) {
	d := &NodeDef{ID: "d", Type: "tool", Deps: []string{"cond", "a2"}}
	if !NodeRunnable(d, map[string]string{"cond": "false"}, map[string]bool{"a2": true}) {
		t.Fatalf("join node with one alive upstream should run")
	}
}

