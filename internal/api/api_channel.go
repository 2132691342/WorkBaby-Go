package api

import (
	"WorkBaby/internal/domain"
)

// ListChannels 通道配置列表。
func (h *Handler) ListChannels() ([]domain.ChannelConfigRESP, error) {
	return h.channelSvc.List(h.ctx)
}

// CreateChannel 新建通道配置。
func (h *Handler) CreateChannel(req domain.ChannelConfigREQ) (domain.ChannelConfigRESP, error) {
	return h.channelSvc.Create(h.ctx, req)
}

// UpdateChannel 更新通道配置（POST /channels/update/{id}）。
func (h *Handler) UpdateChannel(id string, req domain.ChannelConfigREQ) (domain.ChannelConfigRESP, error) {
	return h.channelSvc.Update(h.ctx, id, req)
}

// DeleteChannel 删除通道配置（POST /channels/delete/{id}）。
func (h *Handler) DeleteChannel(id string) (map[string]any, error) {
	if err := h.channelSvc.Delete(h.ctx, id); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "deleted": true}, nil
}

// StartChannel 启动通道（POST /channels/start/{id}）。
func (h *Handler) StartChannel(id string) (domain.ChannelConfigRESP, error) {
	return h.channelSvc.Start(h.ctx, id)
}

// StopChannel 停止通道（POST /channels/stop/{id}）。
func (h *Handler) StopChannel(id string) (domain.ChannelConfigRESP, error) {
	return h.channelSvc.Stop(h.ctx, id)
}

// TestChannel 测试通道配置（POST /channels/test/{id}；配置自检不拨测）。
func (h *Handler) TestChannel(id string) (map[string]any, error) {
	return h.channelSvc.Test(h.ctx, id)
}

// ListChannelMessages 通道消息日志（GET /channels/messages/{id}）。
func (h *Handler) ListChannelMessages(id string, limit int) ([]domain.ChannelMessageLogRESP, error) {
	return h.channelSvc.Messages(h.ctx, id, limit)
}
