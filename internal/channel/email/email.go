// Package email 提供 SMTP 邮件通道（标准库 net/smtp）。
// SMTP 服务器配置来自 system_settings（由装配方组装并解密后注入）。
package email

import (
	"context"
	"encoding/json"
	"fmt"
	"net/smtp"
	"strings"

	"WorkBaby/internal/channel"
	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
)

// SmtpConfig SMTP 服务器配置（password 已解密为明文）。
// 发送走标准库 net/smtp（STARTTLS，587 端口）。
type SmtpConfig struct {
	Enabled  bool
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

// SmtpProvider 按需提供 SMTP 配置（装配方从 system_settings 组装并解密）。
type SmtpProvider func(ctx context.Context) (SmtpConfig, error)

// Channel SMTP 邮件通道。
type Channel struct {
	provider SmtpProvider
}

// New 构造；provider 由装配方注入。
func New(provider SmtpProvider) *Channel { return &Channel{provider: provider} }

// Type 实现 channel.Channel。
func (c *Channel) Type() domain.ChannelType { return domain.ChannelTypeEmail }

// Validate 配置完整性自检：SMTP 已配置且收件人非空。
func (c *Channel) Validate(config *domain.ChannelConfigDO) error {
	if toOf(config) == "" {
		return pkg.New(9305, "email channel missing recipient (configJson.to)", "")
	}
	sc, err := c.provider(context.Background())
	if err != nil {
		return pkg.Wrap(9303, "smtp config unavailable", err)
	}
	if !sc.Enabled || sc.Host == "" {
		return pkg.New(9303, "SMTP not enabled, configure in Settings", "")
	}
	return nil
}

// Send 经 SMTP 发送邮件。
func (c *Channel) Send(ctx context.Context, config *domain.ChannelConfigDO, msg *channel.Message) error {
	sc, err := c.provider(ctx)
	if err != nil {
		return pkg.Wrap(9303, "smtp config unavailable", err)
	}
	if !sc.Enabled {
		return pkg.New(9303, "SMTP not enabled, configure in Settings", "")
	}
	if sc.Host == "" {
		return pkg.New(9303, "SMTP host not configured", "")
	}
	to := toOf(config)
	if to == "" {
		return pkg.New(9305, "email channel missing recipient (configJson.to)", "")
	}
	subject := msg.Subject
	if subject == "" {
		subject = "WorkBaby: " + msg.SessionID
	}
	mime := buildMime(sc.From, to, subject, msg.Body, msg.ContentType)
	addr := fmt.Sprintf("%s:%d", sc.Host, sc.Port)
	auth := smtp.PlainAuth("", sc.Username, sc.Password, sc.Host)
	if err := smtp.SendMail(addr, auth, sc.From, []string{to}, []byte(mime)); err != nil {
		return pkg.Wrap(9303, "smtp send failed", err)
	}
	return nil
}

// buildMime 组装最小 MIME 邮件（单行 subject，正文按 ContentType）。
func buildMime(from, to, subject, body, contentType string) string {
	if contentType == "" {
		contentType = "text/plain"
	}
	subject = strings.ReplaceAll(subject, "\r", "")
	subject = strings.ReplaceAll(subject, "\n", "")
	return fmt.Sprintf("MIME-version: 1.0;\r\nContent-Type: %s; charset=UTF-8\r\n"+
		"From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s", contentType, from, to, subject, body)
}

// toOf 从 configJson 取收件人（兼容 {to, displayName}）。
func toOf(config *domain.ChannelConfigDO) string {
	if config == nil || config.ConfigJSON == "" {
		return ""
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(config.ConfigJSON), &m); err != nil {
		return ""
	}
	return m["to"]
}
