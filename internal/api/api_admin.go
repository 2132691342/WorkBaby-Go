package api

import "WorkBaby/internal/domain"

// GetAdminOverview 返回管理后台概览（版本/环境/计数聚合）。
func (h *Handler) GetAdminOverview() (*domain.AdminOverviewRESP, error) {
	version := h.metaSvc.GetVersion(h.ctx)
	home := ""
	if h.paths != nil {
		home = h.paths.Home
	}
	return h.dashSvc.Overview(h.ctx, version.Version, version.Phase, home, h.cfg != nil)
}
