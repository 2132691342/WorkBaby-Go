// Package domain 的 Workflow 执行聚合根：执行实例 + 节点执行；状态机由 service/executor 维护。
package domain

// WorkflowExecutionStatus 工作流执行状态机。
type WorkflowExecutionStatus string

const (
	WorkflowStatusPending   WorkflowExecutionStatus = "pending"
	WorkflowStatusRunning   WorkflowExecutionStatus = "running"
	WorkflowStatusPaused    WorkflowExecutionStatus = "paused"
	WorkflowStatusCompleted WorkflowExecutionStatus = "completed"
	WorkflowStatusFailed    WorkflowExecutionStatus = "failed"
	WorkflowStatusCancelled WorkflowExecutionStatus = "cancelled"
)

// WorkflowNodeStatus 单节点执行状态。
type WorkflowNodeStatus string

const (
	NodeStatusPending   WorkflowNodeStatus = "pending"
	NodeStatusRunning   WorkflowNodeStatus = "running"
	NodeStatusCompleted WorkflowNodeStatus = "completed"
	NodeStatusFailed    WorkflowNodeStatus = "failed"
	NodeStatusSkipped   WorkflowNodeStatus = "skipped"
	NodeStatusWaiting   WorkflowNodeStatus = "waiting_input" // 等 HumanInput 回执
)

// WorkflowExecutionDO 一次工作流执行（workflow_executions 表）。
type WorkflowExecutionDO struct {
	ID         string                  `gorm:"primaryKey;size:64" json:"id"`
	WorkflowID string                  `gorm:"size:64;index"      json:"workflow_id"`
	Status     WorkflowExecutionStatus `gorm:"size:16;index"      json:"status"`
	Inputs     string                  `gorm:"type:text"          json:"-"` // JSON
	Outputs    string                  `gorm:"type:text"          json:"-"` // JSON
	ErrorMsg   string                  `gorm:"size:1024"          json:"error_msg"`
	StartedAt  int64                   `gorm:"index"              json:"started_at"`
	FinishedAt *int64                  `                            json:"finished_at,omitempty"`
	CreatedAt  int64                   `gorm:"autoCreateTime:milli" json:"created_at"`
}

// TableName 固定表名。
func (WorkflowExecutionDO) TableName() string { return "workflow_executions" }

// WorkflowNodeExecutionDO 单节点执行（workflow_node_executions 表）。
type WorkflowNodeExecutionDO struct {
	ID          string             `gorm:"primaryKey;size:64" json:"id"`
	ExecutionID string             `gorm:"size:64;index"      json:"execution_id"`
	NodeID      string             `gorm:"size:64"            json:"node_id"`
	NodeType    WorkflowNodeType   `gorm:"size:32"            json:"node_type"`
	Status      WorkflowNodeStatus `gorm:"size:16"            json:"status"`
	Inputs      string             `gorm:"type:text"          json:"-"` // JSON：上游渲染后入参
	Outputs     string             `gorm:"type:text"          json:"-"` // JSON
	ErrorMsg    string             `gorm:"size:1024"          json:"error_msg"`
	StartedAt   int64              `                            json:"started_at"`
	FinishedAt  *int64             `                            json:"finished_at,omitempty"`
}

// TableName 固定表名。
func (WorkflowNodeExecutionDO) TableName() string { return "workflow_node_executions" }

// WorkflowExecutionRESP API 出参：把 JSON 字段反序列化展开。
type WorkflowExecutionRESP struct {
	ID         string                  `json:"id"`
	WorkflowID string                  `json:"workflow_id"`
	Status     WorkflowExecutionStatus `json:"status"`
	Inputs     map[string]any          `json:"inputs,omitempty"`
	Outputs    map[string]any          `json:"outputs,omitempty"`
	ErrorMsg   string                  `json:"error_msg,omitempty"`
	StartedAt  int64                   `json:"started_at"`
	FinishedAt *int64                  `json:"finished_at,omitempty"`
	CreatedAt  int64                   `json:"created_at"`
}

// WorkflowNodeExecutionRESP 单节点 API 出参。
type WorkflowNodeExecutionRESP struct {
	ID          string             `json:"id"`
	ExecutionID string             `json:"execution_id"`
	NodeID      string             `json:"node_id"`
	NodeType    WorkflowNodeType   `json:"node_type"`
	Status      WorkflowNodeStatus `json:"status"`
	Inputs      map[string]any     `json:"inputs,omitempty"`
	Outputs     map[string]any     `json:"outputs,omitempty"`
	ErrorMsg    string             `json:"error_msg,omitempty"`
	StartedAt   int64              `json:"started_at"`
	FinishedAt  *int64             `json:"finished_at,omitempty"`
}

// ResolveHumanInputREQ 前端回填 HumanInput 节点等待的入参。
type ResolveHumanInputREQ struct {
	NodeID string `json:"node_id"`
	Value  string `json:"value"`
}

// ExecutionDetailRESP 执行详情：执行记录 + 节点执行列表。
type ExecutionDetailRESP struct {
	Execution WorkflowExecutionRESP       `json:"execution"`
	Nodes     []WorkflowNodeExecutionRESP `json:"nodes"`
}
