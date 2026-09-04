package nodes

import (
	"context"
	"time"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
)

// ChannelSender 通道发送抽象；由 executor 启动期注入，nil 时 ChannelNode 返回 9105。
type ChannelSender interface {
	// Send 发送文本；返回 channel 侧消息 ID。
	Send(ctx context.Context, channelID, msgType, content string) (messageID string, err error)
}

// ChannelNode 通过 channel 发送消息。
//
// cfg 必填：channelId / messageType（"text"/"markdown"）；可选：template（已 RenderConfigMap）。
type ChannelNode struct {
	sender ChannelSender
}

// NewChannelNode 构造；自动注册 schema。
func NewChannelNode(s ChannelSender) *ChannelNode {
	n := &ChannelNode{sender: s}
	Register(n)
	return n
}

// Type 实现 Node 接口。
func (n *ChannelNode) Type() domain.WorkflowNodeType { return domain.WorkflowNodeChannel }

// Schema 实现 Node 接口。
func (n *ChannelNode) Schema() Schema {
	return Schema{
		Type:     domain.WorkflowNodeChannel,
		Required: []string{"channelID", "messageType"},
		Optional: []string{"template"},
	}
}

// Execute：调用 sender.Send；返回 {sentAt, messageId}。
func (n *ChannelNode) Execute(ctx context.Context, inputs map[string]any, cfg map[string]any, upstream map[string]any) (map[string]any, error) {
	if n.sender == nil {
		return nil, pkg.New(9105, "Channel 节点未配置 sender（internal/channel 尚未接入）", "")
	}
	channelID, _ := cfg["channelID"].(string)
	msgType, _ := cfg["messageType"].(string)
	if channelID == "" || msgType == "" {
		return nil, pkg.New(9104, "Channel 节点缺少 cfg.channelId / messageType", "")
	}
	content, _ := cfg["template"].(string)
	id, err := n.sender.Send(ctx, channelID, msgType, content)
	if err != nil {
		return nil, pkg.Wrap(9105, "channel send failed", err)
	}
	return map[string]any{"sentAt": time.Now().UnixMilli(), "messageID": id, "channelID": channelID}, nil
}
