package channel

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/repo"
)

// Service 通道编排：配置 CRUD + 生命周期 + 发送日志（workflow ChannelSender 实现）。
type Service struct {
	cfgRepo  *repo.ChannelConfigRepo
	logRepo  *repo.ChannelMessageLogRepo
	registry *Registry
}

// NewService 构造；registry 由装配方注册 email/webhook/console。
func NewService(cfgRepo *repo.ChannelConfigRepo, logRepo *repo.ChannelMessageLogRepo, registry *Registry) *Service {
	return &Service{cfgRepo: cfgRepo, logRepo: logRepo, registry: registry}
}

// List 全部通道配置。
func (s *Service) List(ctx context.Context) ([]domain.ChannelConfigRESP, error) {
	rows, err := s.cfgRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.ChannelConfigRESP, 0, len(rows))
	for i := range rows {
		out = append(out, toRESP(&rows[i]))
	}
	return out, nil
}

// Create 新建通道配置（默认停用状态）。
func (s *Service) Create(ctx context.Context, req domain.ChannelConfigREQ) (domain.ChannelConfigRESP, error) {
	typ, ok := normalizeType(req.ChannelType)
	if !ok {
		return domain.ChannelConfigRESP{}, pkg.New(9305, "unknown channel type", string(req.ChannelType))
	}
	row := domain.ChannelConfigDO{
		UserID:      domain.LocalUserID,
		ChannelType: typ,
		Enabled:     enabledOf(req.Enabled),
		ConfigJSON:  req.ConfigJSON,
		Status:      domain.ChannelStatusStopped,
	}
	if row.ConfigJSON == "" {
		row.ConfigJSON = assembleConfig(typ, req)
	}
	if err := s.cfgRepo.Create(ctx, &row); err != nil {
		return domain.ChannelConfigRESP{}, err
	}
	return toRESP(&row), nil
}

// Update 更新通道配置（保留 status/lastError/createdAt）。
func (s *Service) Update(ctx context.Context, id string, req domain.ChannelConfigREQ) (domain.ChannelConfigRESP, error) {
	typ, ok := normalizeType(req.ChannelType)
	if !ok {
		return domain.ChannelConfigRESP{}, pkg.New(9305, "unknown channel type", string(req.ChannelType))
	}
	orig, err := s.cfgRepo.GetByID(ctx, id)
	if err != nil {
		return domain.ChannelConfigRESP{}, err
	}
	cfgJSON := req.ConfigJSON
	if cfgJSON == "" {
		cfgJSON = assembleConfig(typ, req)
	}
	orig.ChannelType = typ
	orig.Enabled = enabledOf(req.Enabled)
	orig.ConfigJSON = cfgJSON
	if err := s.cfgRepo.Update(ctx, orig); err != nil {
		return domain.ChannelConfigRESP{}, err
	}
	return toRESP(orig), nil
}

// Delete 删除通道配置。
func (s *Service) Delete(ctx context.Context, id string) error {
	if _, err := s.cfgRepo.GetByID(ctx, id); err != nil {
		return err
	}
	return s.cfgRepo.Delete(ctx, id)
}

// Start 启动（更新为 running 状态并刷新 lastActiveAt）。
func (s *Service) Start(ctx context.Context, id string) (domain.ChannelConfigRESP, error) {
	row, err := s.cfgRepo.GetByID(ctx, id)
	if err != nil {
		return domain.ChannelConfigRESP{}, err
	}
	row.Status = domain.ChannelStatusRunning
	row.LastError = ""
	row.LastActiveAt = time.Now().UnixMilli()
	if err := s.cfgRepo.Update(ctx, row); err != nil {
		return domain.ChannelConfigRESP{}, err
	}
	return toRESP(row), nil
}

// Stop 停止。
func (s *Service) Stop(ctx context.Context, id string) (domain.ChannelConfigRESP, error) {
	row, err := s.cfgRepo.GetByID(ctx, id)
	if err != nil {
		return domain.ChannelConfigRESP{}, err
	}
	row.Status = domain.ChannelStatusStopped
	if err := s.cfgRepo.Update(ctx, row); err != nil {
		return domain.ChannelConfigRESP{}, err
	}
	return toRESP(row), nil
}

// Test 配置完整性自检（不做真实网络拨测）。
func (s *Service) Test(ctx context.Context, id string) (map[string]any, error) {
	row, err := s.cfgRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	c := s.registry.Get(row.ChannelType)
	if c == nil {
		return map[string]any{"ok": false, "message": "channel implementation not registered"}, nil
	}
	if err := c.Validate(row); err != nil {
		return map[string]any{"ok": false, "message": err.Error()}, nil
	}
	return map[string]any{"ok": true, "message": "ok"}, nil
}

// Send 按通道投递并写发送日志（pending → success/failed）。
// 实现 workflow/nodes.ChannelSender 接口（duck typing）。
func (s *Service) Send(ctx context.Context, channelID, msgType, content string) (string, error) {
	row, err := s.cfgRepo.GetByID(ctx, channelID)
	if err != nil {
		return "", err
	}
	if !row.Enabled {
		return "", domain.ErrChannelNotFound
	}
	c := s.registry.Get(row.ChannelType)
	if c == nil {
		return "", pkg.New(9304, "channel implementation not registered", string(row.ChannelType))
	}
	logRow := &domain.ChannelMessageLogDO{
		ChannelID:      row.ID,
		Direction:      "outbound",
		MessageType:    msgType,
		UserExternalID: domain.LocalUserID,
		ContentSummary: summarize(content),
		Status:         "pending",
	}
	if err := s.logRepo.Create(ctx, logRow); err != nil {
		return "", err
	}
	msg := &Message{Body: content, ContentType: contentTypeOf(msgType)}
	if err := c.Send(ctx, row, msg); err != nil {
		_ = s.logRepo.UpdateStatus(ctx, logRow.ID, "failed", truncateErr(err))
		return logRow.ID, err
	}
	_ = s.logRepo.UpdateStatus(ctx, logRow.ID, "success", "")
	return logRow.ID, nil
}

// Messages 按通道取消息日志。
func (s *Service) Messages(ctx context.Context, channelID string, limit int) ([]domain.ChannelMessageLogRESP, error) {
	rows, err := s.logRepo.ListByChannel(ctx, channelID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]domain.ChannelMessageLogRESP, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		out = append(out, domain.ChannelMessageLogRESP{
			ID: r.ID, ChannelID: r.ChannelID, Direction: r.Direction,
			MessageType: r.MessageType, UserExternalID: r.UserExternalID,
			SessionID: r.SessionID, ContentSummary: r.ContentSummary,
			Status: r.Status, ErrorMessage: r.ErrorMessage, CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}

// normalizeType 归一化通道类型；前端传大写，内部宽松处理大小写。
func normalizeType(t domain.ChannelType) (domain.ChannelType, bool) {
	switch domain.ChannelType(strings.ToUpper(strings.TrimSpace(string(t)))) {
	case domain.ChannelTypeEmail:
		return domain.ChannelTypeEmail, true
	case domain.ChannelTypeWebhook:
		return domain.ChannelTypeWebhook, true
	case domain.ChannelTypeConsole:
		return domain.ChannelTypeConsole, true
	}
	return "", false
}

// assembleConfig 前端结构化字段（emailTo/webhookUrl）兜底组装 configJson。
func assembleConfig(typ domain.ChannelType, req domain.ChannelConfigREQ) string {
	var m map[string]string
	switch typ {
	case domain.ChannelTypeEmail:
		m = map[string]string{}
		if req.EmailTo != "" {
			m["to"] = req.EmailTo
		}
		if req.EmailDisplayName != "" {
			m["displayName"] = req.EmailDisplayName
		}
	case domain.ChannelTypeWebhook:
		if req.WebhookURL != "" {
			m = map[string]string{"url": req.WebhookURL}
		}
	}
	if len(m) == 0 {
		return ""
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// toRESP 映射；webhook 通道的 url 从 configJson 提取回填 webhookUrl。
func toRESP(d *domain.ChannelConfigDO) domain.ChannelConfigRESP {
	resp := domain.ChannelConfigRESP{
		ID: d.ID, UserID: d.UserID, ChannelType: d.ChannelType,
		Enabled: d.Enabled, ConfigJSON: d.ConfigJSON,
		Status: d.Status, LastError: d.LastError, LastActiveAt: d.LastActiveAt,
		CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
	if d.ChannelType == domain.ChannelTypeWebhook && d.ConfigJSON != "" {
		var m map[string]string
		if json.Unmarshal([]byte(d.ConfigJSON), &m) == nil {
			resp.WebhookURL = m["url"]
		}
	}
	return resp
}

func enabledOf(v *bool) bool {
	if v == nil {
		return true
	}
	return *v
}

func contentTypeOf(msgType string) string {
	switch msgType {
	case "markdown":
		return "text/markdown"
	case "html":
		return "text/html"
	default:
		return "text/plain"
	}
}

func summarize(content string) string {
	r := []rune(content)
	if len(r) > 200 {
		return string(r[:200])
	}
	return content
}

func truncateErr(err error) string {
	msg := err.Error()
	r := []rune(msg)
	if len(r) > 500 {
		return string(r[:500])
	}
	return msg
}
