// Package domain 的 Workflow 聚合根：DAG 定义、节点类型枚举、错误变量。
//
// 约束：纯域对象；不引 service/repo/api/能力域/wails。
package domain

import (
	"gorm.io/gorm"

	"WorkBaby/internal/pkg"
)

// WorkflowNodeType 内置节点类型。
type WorkflowNodeType string

const (
	WorkflowNodeLLM        WorkflowNodeType = "llm"
	WorkflowNodeTool       WorkflowNodeType = "tool"
	WorkflowNodeCode       WorkflowNodeType = "code"
	WorkflowNodeCondition  WorkflowNodeType = "condition"
	WorkflowNodeHTTP       WorkflowNodeType = "http"
	WorkflowNodeHumanInput WorkflowNodeType = "human_input"
	WorkflowNodeChannel    WorkflowNodeType = "channel"
)

// AllWorkflowNodeTypes 编辑器下拉与校验器使用。
var AllWorkflowNodeTypes = []WorkflowNodeType{
	WorkflowNodeLLM, WorkflowNodeTool, WorkflowNodeCode, WorkflowNodeCondition,
	WorkflowNodeHTTP, WorkflowNodeHumanInput, WorkflowNodeChannel,
}

// IsValidNodeType 是否为已知节点类型。
func (t WorkflowNodeType) IsValid() bool {
	for _, x := range AllWorkflowNodeTypes {
		if x == t {
			return true
		}
	}
	return false
}

// WorkflowDO 工作流定义（workflows 表）。
//
// Graph 字段是 JSON 字符串：{"name","inputs","nodes":[{id,type,deps,config,inputs,branch}],"outputs"}；
// 解析形态见 internal/workflow.Graph。
type WorkflowDO struct {
	ID          string         `gorm:"primaryKey;size:64" json:"id"`
	Name        string         `gorm:"size:128"          json:"name"`
	Description string         `gorm:"size:512"          json:"description"`
	Graph       string         `gorm:"type:text"         json:"-"` // DAG JSON
	Enabled     bool           `gorm:"default:true"      json:"enabled"`
	CreatedAt   int64          `gorm:"autoCreateTime:milli" json:"created_at"`
	UpdatedAt   int64          `gorm:"autoUpdateTime:milli" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index"             json:"-"`
}

// TableName 固定表名。
func (WorkflowDO) TableName() string { return "workflows" }

// WorkflowGraphREQ 保存或更新入参；Graph 是 JSON 字符串，service 层负责解析 + 校验。
type WorkflowGraphREQ struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Graph       string `json:"graph"`
	Enabled     bool   `json:"enabled"`
}

// WorkflowRESP API 出参（含 Graph 原样回传；前端编辑器据此渲染）。
type WorkflowRESP struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Graph       string `json:"graph"`
	Enabled     bool   `json:"enabled"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

// WorkflowRunREQ 手动运行入参。
type WorkflowRunREQ struct {
	Inputs map[string]any `json:"inputs"`
}

// WorkflowRunRESP 触发运行返回（返回 executionID 供前端轮询详情）。
type WorkflowRunRESP struct {
	ExecutionID string `json:"execution_id"`
}

// WorkflowPos 画布坐标（Vue Flow position，编辑器持久化用）。
type WorkflowPos struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// WorkflowDAGNode 可视化编辑器节点（Vue Flow 格式）。
//
// Branch 是 condition 分支归属（"true"/"false"/自定义字符串）；Pos 是画布坐标；
// Inputs 是节点入参变量引用（key → "nodeId.field"），三 者都持久化到 Graph JSON。
type WorkflowDAGNode struct {
	ID     string            `json:"id"`
	Type   string            `json:"type"`
	Params map[string]any    `json:"params"`
	Branch string            `json:"branch,omitempty"`
	Pos    *WorkflowPos      `json:"pos,omitempty"`
	Inputs map[string]string `json:"inputs,omitempty"` // 入参变量引用：key → "nodeId.field"
}

// WorkflowDAGEdge 可视化编辑器连线。
//
// SourceHandle 表示从源节点哪个出口连出：普通节点留空；condition 节点为
// "true"/"false"（或自定义分支串），保存时写入目标节点的 Branch 字段。
type WorkflowDAGEdge struct {
	From         string `json:"from"`
	To           string `json:"to"`
	SourceHandle string `json:"source_handle,omitempty"`
}

// WorkflowNodeFieldRESP 节点字段描述（schema 驱动前端属性面板）。
type WorkflowNodeFieldRESP struct {
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Type        string   `json:"type"` // text / number / textarea / select / json
	Required    bool     `json:"required"`
	Description string   `json:"description,omitempty"`
	Options     []string `json:"options,omitempty"`
	Default     any      `json:"default,omitempty"`
}

// WorkflowNodeTypeRESP 节点类型目录（调色板 + 属性面板共用）。
type WorkflowNodeTypeRESP struct {
	Type   string                  `json:"type"`
	Label  string                  `json:"label"`
	Fields []WorkflowNodeFieldRESP `json:"fields"`
}

// WorkflowDAGREQ 可视化编辑器保存的图（前端格式；service 转换为 Graph JSON 落库）。
type WorkflowDAGREQ struct {
	Name    string            `json:"name"`
	Inputs  map[string]string `json:"inputs,omitempty"`  // 图级入参：key → "nodeId.field" 引用
	Nodes   []WorkflowDAGNode `json:"nodes"`
	Edges   []WorkflowDAGEdge `json:"edges"`
	Outputs map[string]string `json:"outputs,omitempty"` // 图级输出：key → "nodeId.field"
}

// WorkflowDAGRESP 可视化编辑器加载的图（后端 Graph JSON → 前端格式）。
type WorkflowDAGRESP struct {
	Name    string            `json:"name"`
	Inputs  map[string]string `json:"inputs,omitempty"`
	Nodes   []WorkflowDAGNode `json:"nodes"`
	Edges   []WorkflowDAGEdge `json:"edges"`
	Outputs map[string]string `json:"outputs,omitempty"` // 图级输出：key → "nodeId.field"
}

// 包级错误变量；错误码段位 9100。
var (
	ErrWorkflowNotFound      = pkg.New(9101, "workflow not found", "")
	ErrWorkflowInvalidGraph  = pkg.New(9102, "workflow graph invalid", "")
	ErrWorkflowCycle         = pkg.New(9103, "workflow has cycle", "")
	ErrWorkflowConfigInvalid = pkg.New(9104, "workflow node config invalid", "")
	ErrWorkflowNodeFailed    = pkg.New(9105, "workflow node execution failed", "")
	ErrWorkflowHumanTimeout  = pkg.New(9106, "workflow human input timeout", "")
	ErrWorkflowCancelled     = pkg.New(9107, "workflow cancelled", "")
)
