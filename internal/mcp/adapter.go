package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"WorkBaby/internal/pkg"
	"WorkBaby/internal/tool"
)

// MCPToolPrefix MCP 工具全局命名前缀：mcp_{server}_{tool}，与内置工具、其他 server 天然隔离。
const MCPToolPrefix = "mcp_"

// fallbackSchema 远端 inputSchema 缺失或非法 JSON 时的兜底：不信任外部输入，但不让工具彻底不可用。
var fallbackSchema = json.RawMessage(`{"type":"object","properties":{}}`)

// MCPAdapter 把 MCP 远端工具适配成本地 tool.Tool。
type MCPAdapter struct {
	ServerName string
	Client     Client
	Tool       ToolDef
}

// Name 全局唯一工具名。
func (a *MCPAdapter) Name() string { return MCPToolPrefix + a.ServerName + "_" + a.Tool.Name }

// Description 远端无描述时给出可读兜底。
func (a *MCPAdapter) Description() string {
	if a.Tool.Description != "" {
		return a.Tool.Description
	}
	return "MCP 工具（来自 server " + a.ServerName + "）"
}

// Schema 暴露给 LLM 的参数定义。
func (a *MCPAdapter) Schema() tool.ToolSchema {
	params := a.Tool.InputSchema
	if len(bytes.TrimSpace(params)) == 0 || !json.Valid(params) {
		params = fallbackSchema
	}
	return tool.ToolSchema{Name: a.Name(), Description: a.Description(), Parameters: params}
}

// RiskLevel 外部进程能力不可控，默认按网络级。
func (a *MCPAdapter) RiskLevel() tool.RiskLevel { return tool.RiskNetwork }

// Execute 转发到远端；远端返回 isError 时内容仍然回填给 LLM，同时置 Err 便于前端提示。
func (a *MCPAdapter) Execute(ctx context.Context, args json.RawMessage) tool.ToolResult {
	res, err := a.Client.CallTool(ctx, a.Tool.Name, args)
	if err != nil {
		return tool.ToolResult{Err: err}
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	out := tool.ToolResult{Content: sb.String()}
	if res.IsError {
		out.Err = pkg.New(8005, "mcp tool returned an error result", a.Tool.Name)
	}
	return out
}
