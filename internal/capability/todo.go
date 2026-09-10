package capability

import (
	"context"
	"strconv"
	"strings"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/harness"
	"WorkBaby/internal/tool"
)

// TodoStore 计划存取最小接口（对齐 tool/todo.Store，避免包耦合）。
type TodoStore interface {
	Load(sessionID string) ([]domain.TodoItem, error)
}

// Todo 计划能力：把会话当前待办计划作为常驻 system 段每轮回显。
//
// 任务状态与对话历史分离：计划不依赖模型「记得」上一轮列过什么，
// 长任务跨轮/跨压缩不丢目标。无计划时本轮不注入。
type Todo struct {
	store TodoStore
}

// NewTodo 构造计划能力；store 为 nil 时 Preload 恒为空。
func NewTodo(store TodoStore) *Todo { return &Todo{store: store} }

// ID 实现 Capability。
func (t *Todo) ID() string { return "todo" }

// Tools 实现 Capability：todo 工具由装配方单独注册到工具注册表，此处不重复暴露。
func (t *Todo) Tools() []tool.Tool { return nil }

// Capture 实现 Capability：计划的持久化由 todo 工具自身负责，此处无沉淀。
func (t *Todo) Capture(ctx context.Context, c *CaptureCtx) error { return nil }

// Preload 实现 Capability：有计划时回显当前清单与勾选进度。
func (t *Todo) Preload(ctx context.Context, c *PreloadCtx) ([]harness.ContextPiece, error) {
	if t == nil || t.store == nil {
		return nil, nil
	}
	items, err := t.store.Load(c.SessionID)
	if err != nil || len(items) == 0 {
		return nil, nil // 无计划 / 存储异常：静默跳过，不阻断 run
	}
	done := 0
	var b strings.Builder
	for _, it := range items {
		mark := "[ ]"
		if it.Done {
			mark = "[x]"
			done++
		}
		b.WriteString(mark + " " + it.Title + "\n")
	}
	b.WriteString("进度 " + strconv.Itoa(done) + "/" + strconv.Itoa(len(items)) +
		"。执行中持续对照此计划：完成一项立即 todo(mark_done)，新增步骤先 todo(plan) 补录。")
	return []harness.ContextPiece{{
		Key:      "plan",
		Title:    "当前计划（会话待办）",
		Body:     b.String(),
		Priority: harness.PriorityLow,
	}}, nil
}
