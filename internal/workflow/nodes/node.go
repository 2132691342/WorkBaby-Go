// Package nodes 实现 7 种内置工作流节点；Schema 声明字段描述符（FieldSpec），供校验与可视化编辑器共用。
package nodes

import (
	"context"

	"WorkBaby/internal/domain"
)

// FieldType 字段渲染类型（schema 驱动前端属性面板）。
type FieldType string

const (
	FieldText     FieldType = "text"
	FieldNumber   FieldType = "number"
	FieldTextarea FieldType = "textarea"
	FieldSelect   FieldType = "select"
	FieldJSON     FieldType = "json"
)

// FieldSpec 单字段描述：name 是 cfg 键（与节点 Execute 读取一致），其余供编辑器渲染。
type FieldSpec struct {
	Name        string    `json:"name"`
	Label       string    `json:"label"`
	Type        FieldType `json:"type"`
	Required    bool      `json:"required"`
	Description string    `json:"description,omitempty"`
	Options     []string  `json:"options,omitempty"`
	Default     any       `json:"default,omitempty"`
}

// Schema 节点配置 schema（编辑器 / 校验器共用）。
//
// Required/Optional 为 cfg 必填/可选键；Fields 是编辑器属性面板的完整字段描述。
// Fields 若为空则编辑器退化为裸 KV 编辑（保留向后兼容）。
type Schema struct {
	Type     domain.WorkflowNodeType
	Required []string
	Optional []string
	Fields   []FieldSpec
}

// requiredSet 供校验器快速判断。
func (s Schema) requiredSet() map[string]bool {
	m := make(map[string]bool, len(s.Required))
	for _, k := range s.Required {
		m[k] = true
	}
	return m
}

// Node 工作流节点统一接口。
type Node interface {
	Type() domain.WorkflowNodeType
	Execute(ctx context.Context, inputs map[string]any, cfg map[string]any, upstream map[string]any) (map[string]any, error)
	Schema() Schema
}

// schemaRegistry 全局节点 schema 表（每个 NewXxxNode 自动 Register，校验器可独立使用）。
var schemaRegistry = map[domain.WorkflowNodeType]Schema{}

// builtinSchemas 与 schemaRegistry 同源但写死，用于校验器 / 编辑器在无实例化时也能取 schema。
// SchemaFor 优先查 registry；没注册时返回 builtin（保留 NewXxxNode 注册的 override 语义）。
//
// 字段描述里的 name 即 cfg 键，必须与对应节点 Execute 中读取的键一致（如 providerID / toolName）。
var builtinSchemas = map[domain.WorkflowNodeType]Schema{
	domain.WorkflowNodeLLM: {
		Type:     domain.WorkflowNodeLLM,
		Required: []string{"providerID", "model"},
		Optional: []string{"systemPrompt", "userPromptTemplate", "temperature"},
		Fields: []FieldSpec{
			{Name: "providerID", Label: "模型提供方", Type: FieldSelect, Required: true, Description: "选择已配置的 LLM Provider"},
			{Name: "model", Label: "模型名", Type: FieldText, Required: true, Description: "如 deepseek-chat / gpt-4o"},
			{Name: "systemPrompt", Label: "系统提示词", Type: FieldTextarea, Description: "可选；注入 system 角色"},
			{Name: "userPromptTemplate", Label: "用户提示词模板", Type: FieldTextarea, Description: "支持 {{#nodeId.field#}} 引用上游输出"},
			{Name: "temperature", Label: "温度", Type: FieldNumber, Default: float64(0.2), Description: "0~2，留空走模型默认"},
		},
	},
	domain.WorkflowNodeTool: {
		Type:     domain.WorkflowNodeTool,
		Required: []string{"toolName"},
		Optional: []string{"args", "argsTemplate"},
		Fields: []FieldSpec{
			{Name: "toolName", Label: "工具", Type: FieldSelect, Required: true, Description: "选择已注册的工具"},
			{Name: "args", Label: "参数（JSON）", Type: FieldJSON, Description: "直接传 JSON 参数对象"},
			{Name: "argsTemplate", Label: "参数模板", Type: FieldTextarea, Description: "支持 {{#nodeId.field#}} 引用的字符串形式"},
		},
	},
	domain.WorkflowNodeCode: {
		Type:     domain.WorkflowNodeCode,
		Required: []string{"script"},
		Optional: []string{"timeout_ms"},
		Fields: []FieldSpec{
			{Name: "script", Label: "JavaScript 脚本", Type: FieldTextarea, Required: true,
				Description: "goja 沙箱；全局 $input 为上游输出，结果写入全局 output（或末表达式返回）"},
			{Name: "timeout_ms", Label: "超时（毫秒）", Type: FieldNumber, Default: 10000, Description: "1000~60000"},
		},
	},
	domain.WorkflowNodeCondition: {
		Type:     domain.WorkflowNodeCondition,
		Required: []string{"expression"},
		Optional: []string{},
		Fields: []FieldSpec{
			{Name: "expression", Label: "表达式", Type: FieldTextarea, Required: true,
				Description: "expr 表达式，如 score > 60；结果 true/false → 分支 true/false，字符串 → 直接作为分支标签"},
		},
	},
	domain.WorkflowNodeHTTP: {
		Type:     domain.WorkflowNodeHTTP,
		Required: []string{"url"},
		Optional: []string{"method", "headers", "body"},
		Fields: []FieldSpec{
			{Name: "method", Label: "方法", Type: FieldSelect, Options: []string{"GET", "POST"}, Default: "GET"},
			{Name: "url", Label: "URL", Type: FieldText, Required: true, Description: "支持 {{#nodeId.field#}} 引用"},
			{Name: "headers", Label: "请求头（JSON）", Type: FieldJSON},
			{Name: "body", Label: "请求体", Type: FieldTextarea, Description: "支持 {{#nodeId.field#}} 引用"},
		},
	},
	domain.WorkflowNodeHumanInput: {
		Type:     domain.WorkflowNodeHumanInput,
		Required: []string{"prompt"},
		Optional: []string{"ttlSeconds"},
		Fields: []FieldSpec{
			{Name: "prompt", Label: "提示文案", Type: FieldTextarea, Required: true, Description: "运行时展示给用户的问题"},
			{Name: "ttlSeconds", Label: "等待超时（秒）", Type: FieldNumber, Default: float64(300)},
		},
	},
	domain.WorkflowNodeChannel: {
		Type:     domain.WorkflowNodeChannel,
		Required: []string{"channelID", "messageType"},
		Optional: []string{"template"},
		Fields: []FieldSpec{
			{Name: "channelID", Label: "通道 ID", Type: FieldText, Required: true},
			{Name: "messageType", Label: "消息类型", Type: FieldText, Required: true},
			{Name: "template", Label: "内容模板", Type: FieldTextarea, Description: "支持 {{#nodeId.field#}} 引用"},
		},
	},
}

// Register 把节点实现与 schema 注册到工作流引擎；重复注册会覆盖（同 Type 视为同一节点版本更新）。
func Register(n Node) {
	schemaRegistry[n.Type()] = n.Schema()
}

// SchemaFor 取 schema：优先实例注册的版本，否则 builtin。
func SchemaFor(t domain.WorkflowNodeType) Schema {
	if s, ok := schemaRegistry[t]; ok {
		return s
	}
	return builtinSchemas[t]
}

// RegisterAll 一次性注册多个节点（与 New 各自注册的语义一致；保留做手动批注册接口）。
func RegisterAll(ns ...Node) {
	for _, n := range ns {
		Register(n)
	}
}
