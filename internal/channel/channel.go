// Package channel 提供多通道通知：email（SMTP）/ webhook（v1）+ 发送日志 + 模板。
// 不依赖 Wails runtime；workflow 的 ChannelSender 由 Service 实现。
package channel

import (
	"context"
	"log/slog"

	"WorkBaby/internal/domain"
)

// Channel 通知通道适配器。
type Channel interface {
	// Type 返回该实现覆盖的通道类型。
	Type() domain.ChannelType
	// Send 投递消息；config 为通道配置（含已解析的 configJson）。
	Send(ctx context.Context, config *domain.ChannelConfigDO, msg *Message) error
	// Validate 配置完整性自检（不做真实网络拨测）。
	Validate(config *domain.ChannelConfigDO) error
}

// Message 出站消息。
type Message struct {
	Subject     string // 缺省用 "WorkBaby: {sessionId}"
	Body        string
	ContentType string // text/plain（缺省）/ text/markdown / text/html
	SessionID   string
}

// ConsoleChannel 兜底实现：CONSOLE 类型（兼容历史配置）发送即写应用日志。
type ConsoleChannel struct{}

// Type 实现 Channel 接口。
func (ConsoleChannel) Type() domain.ChannelType { return domain.ChannelTypeConsole }

// Send 写应用日志。
func (ConsoleChannel) Send(_ context.Context, _ *domain.ChannelConfigDO, msg *Message) error {
	slog.Info("console channel send", "contentType", msg.ContentType, "body", msg.Body)
	return nil
}

// Validate 恒通过。
func (ConsoleChannel) Validate(_ *domain.ChannelConfigDO) error { return nil }
