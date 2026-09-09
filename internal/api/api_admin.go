package api

import (
	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
)

// GetAdminOverview 返回管理后台概览（版本/环境/计数聚合）。
func (h *Handler) GetAdminOverview() (*domain.AdminOverviewRESP, error) {
	version := h.metaSvc.GetVersion(h.ctx)
	home := ""
	if h.paths != nil {
		home = h.paths.Home
	}
	return h.dashSvc.Overview(h.ctx, version.Version, version.Phase, home, h.cfg != nil)
}

// CleanupMisreportedTokenUsage 一次性清理上游误报的缓存 token。
//
// <p>背景：GLM 等 OpenAI 兼容实现偶发把 cached_tokens 报成等于 prompt_tokens，导致
// 仪表盘命中率 100%、token 总数虚高。LLM adapter 已在解析时 clamp 新数据；本端点
// 专治存量 token_usages 表里的脏数据——把 `cache_read_tokens > input_tokens` 的行
// cache_read / cache_write 清零，让 1.08M 这种历史虚高恢复真实。
//
// <p>不可逆、幂等：admin 域人工操作，无需鉴权（WorkBaby 本地单用户）。
func (h *Handler) CleanupMisreportedTokenUsage() (map[string]any, error) {
	if h.usageRepo == nil {
		return nil, pkg.New(2012, "token usage repo not ready", "")
	}
	n, err := h.usageRepo.CleanupMisreported(h.ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"cleaned": n}, nil
}
