// Package webhook 提供 Webhook 通知通道（HTTP GET/POST，method 走 pkg/httprules 白名单）。
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"WorkBaby/internal/channel"
	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
)

// Config webhook 通道配置（存于 configJson）。
type Config struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`            // 缺省 POST；仅 GET/POST
	Headers map[string]string `json:"headers,omitempty"` // 附加请求头
	Auth    *Auth             `json:"auth,omitempty"`
}

// Auth 鉴权（bearer / basic）。
type Auth struct {
	Type  string `json:"type"`
	Token string `json:"token"`
	User  string `json:"user"`
	Pass  string `json:"pass"`
}

// Channel Webhook 通道。
type Channel struct {
	client *http.Client
}

// New 构造；超时默认 30s。
func New() *Channel {
	return &Channel{client: &http.Client{Timeout: 30 * time.Second}}
}

// Type 实现 channel.Channel。
func (c *Channel) Type() domain.ChannelType { return domain.ChannelTypeWebhook }

// Validate 配置完整性自检：URL 必填，method 缺省 POST，非空时必须命中 GET/POST 白名单。
func (c *Channel) Validate(config *domain.ChannelConfigDO) error {
	cfg, err := parse(config.ConfigJSON)
	if err != nil {
		return pkg.New(9305, "webhook config invalid", err.Error())
	}
	if cfg.URL == "" {
		return pkg.New(9305, "webhook config missing url", "")
	}
	if raw := strings.TrimSpace(cfg.Method); raw != "" {
		if _, ok := pkg.NormalizeMethod(raw); !ok {
			return pkg.New(9305, "webhook method must be GET or POST", raw)
		}
	}
	return nil
}

// Send 投递 webhook。
func (c *Channel) Send(ctx context.Context, config *domain.ChannelConfigDO, msg *channel.Message) error {
	cfg, err := parse(config.ConfigJSON)
	if err != nil {
		return pkg.Wrap(9305, "webhook config invalid", err)
	}
	payload, err := json.Marshal(map[string]any{
		"subject":     msg.Subject,
		"body":        msg.Body,
		"contentType": msg.ContentType,
		"sessionID":   msg.SessionID,
	})
	if err != nil {
		return pkg.Wrap(9301, "marshal webhook payload failed", err)
	}
	method := http.MethodPost // 缺省 POST
	if raw := strings.TrimSpace(cfg.Method); raw != "" {
		var ok bool
		method, ok = pkg.NormalizeMethod(raw)
		if !ok {
			return pkg.New(9305, "webhook method must be GET or POST", raw)
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, cfg.URL, bytes.NewReader(payload))
	if err != nil {
		return pkg.Wrap(9301, "build webhook request failed", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "WorkBaby/1.0")
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}
	applyAuth(req, cfg.Auth)
	resp, err := c.client.Do(req)
	if err != nil {
		return pkg.Wrap(9301, "webhook request failed", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return pkg.New(9301, "webhook returned error", strconv.Itoa(resp.StatusCode))
	}
	return nil
}

func applyAuth(req *http.Request, a *Auth) {
	if a == nil {
		return
	}
	switch strings.ToLower(strings.TrimSpace(a.Type)) {
	case "bearer":
		if a.Token != "" {
			req.Header.Set("Authorization", "Bearer "+a.Token)
		}
	case "basic":
		if a.User != "" || a.Pass != "" {
			req.SetBasicAuth(a.User, a.Pass)
		}
	}
}

func parse(raw string) (Config, error) {
	var cfg Config
	if raw == "" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}
