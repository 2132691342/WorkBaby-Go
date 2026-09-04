package repo

import (
	"context"
	"time"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"gorm.io/gorm"
)

// DashboardRepo 仪表盘只读聚合统计（跨域，仅 SELECT 计数/列表）。
type DashboardRepo struct{ db *gorm.DB }

// NewDashboardRepo 构造。
func NewDashboardRepo(db *gorm.DB) *DashboardRepo { return &DashboardRepo{db: db} }

// count 通用计数（按可选时间下界过滤）。
func (r *DashboardRepo) count(ctx context.Context, model any, sinceMs int64) (int64, error) {
	q := r.db.WithContext(ctx).Model(model)
	if sinceMs > 0 {
		q = q.Where("created_at >= ?", sinceMs)
	}
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return 0, pkg.Wrap(2010, "dashboard count failed", err)
	}
	return n, nil
}

// CountMemoryEpisodes 情景记忆数。
func (r *DashboardRepo) CountMemoryEpisodes(ctx context.Context) (int64, error) {
	return r.count(ctx, &domain.MemoryEpisodeDO{}, 0)
}

// CountMemoryFacts 语义记忆数。
func (r *DashboardRepo) CountMemoryFacts(ctx context.Context) (int64, error) {
	return r.count(ctx, &domain.MemoryFactDO{}, 0)
}

// CountMemoryProcedures 程序记忆数。
func (r *DashboardRepo) CountMemoryProcedures(ctx context.Context) (int64, error) {
	return r.count(ctx, &domain.MemoryProcedureDO{}, 0)
}

// CountMediaArtifacts 媒体产物数。
func (r *DashboardRepo) CountMediaArtifacts(ctx context.Context) (int64, error) {
	return r.count(ctx, &domain.MediaArtifactDO{}, 0)
}

// CountCronJobs 定时任务数（未删除）。
func (r *DashboardRepo) CountCronJobs(ctx context.Context) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&domain.CronJobDO{}).
		Where("deleted_at = ?", 0).
		Count(&n).Error; err != nil {
		return 0, pkg.Wrap(2010, "count cron jobs failed", err)
	}
	return n, nil
}

// CountSessions 会话总数（未删除）。
func (r *DashboardRepo) CountSessions(ctx context.Context) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&domain.ChatSessionDO{}).
		Where("deleted_at = ?", 0).
		Count(&n).Error; err != nil {
		return 0, pkg.Wrap(2010, "count sessions failed", err)
	}
	return n, nil
}

// CountMessages 消息总数。
func (r *DashboardRepo) CountMessages(ctx context.Context) (int64, error) {
	return r.count(ctx, &domain.MessageDO{}, 0)
}

// CountKnowledgeDocs 知识库文档总数。
func (r *DashboardRepo) CountKnowledgeDocs(ctx context.Context) (int64, error) {
	return r.count(ctx, &domain.KnowledgeDocDO{}, 0)
}

// CountWorkflows 工作流总数。
func (r *DashboardRepo) CountWorkflows(ctx context.Context) (int64, error) {
	return r.count(ctx, &domain.WorkflowDO{}, 0)
}

// CountChannels 通知通道总数（未删除）。
func (r *DashboardRepo) CountChannels(ctx context.Context) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&domain.ChannelConfigDO{}).
		Where("deleted_at = ?", 0).
		Count(&n).Error; err != nil {
		return 0, pkg.Wrap(2010, "count channels failed", err)
	}
	return n, nil
}

// CountProviders 模型 Provider 总数。
func (r *DashboardRepo) CountProviders(ctx context.Context) (int64, error) {
	return r.count(ctx, &domain.AiProviderDO{}, 0)
}

// CountTodaySessions 今日新建会话数。
func (r *DashboardRepo) CountTodaySessions(ctx context.Context, todayStart int64) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&domain.ChatSessionDO{}).
		Where("created_at >= ? AND deleted_at = ?", todayStart, 0).
		Count(&n).Error; err != nil {
		return 0, pkg.Wrap(2010, "count today sessions failed", err)
	}
	return n, nil
}

// CountTodayMessages 今日消息数。
func (r *DashboardRepo) CountTodayMessages(ctx context.Context, todayStart int64) (int64, error) {
	return r.count(ctx, &domain.MessageDO{}, todayStart)
}

// SumTodayTokens 今日 token 消耗（assistant 消息 totalTokens 求和）。
func (r *DashboardRepo) SumTodayTokens(ctx context.Context, todayStart int64) (int64, error) {
	var sum int64
	if err := r.db.WithContext(ctx).Model(&domain.MessageDO{}).
		Where("created_at >= ?", todayStart).
		Select("COALESCE(SUM(total_tokens), 0)").
		Scan(&sum).Error; err != nil {
		return 0, pkg.Wrap(2010, "sum today tokens failed", err)
	}
	return sum, nil
}

// ListRecentMessages 最近消息（跨会话，倒序）。
func (r *DashboardRepo) ListRecentMessages(ctx context.Context, limit int) ([]domain.MessageDO, error) {
	if limit <= 0 || limit > 50 {
		limit = 8
	}
	var rows []domain.MessageDO
	if err := r.db.WithContext(ctx).Order("created_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, pkg.Wrap(2010, "list recent messages failed", err)
	}
	return rows, nil
}

// ListRecentMedia 最近媒体产物（倒序）。
func (r *DashboardRepo) ListRecentMedia(ctx context.Context, limit int) ([]domain.MediaArtifactDO, error) {
	if limit <= 0 || limit > 50 {
		limit = 8
	}
	var rows []domain.MediaArtifactDO
	if err := r.db.WithContext(ctx).Order("created_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, pkg.Wrap(2010, "list recent media failed", err)
	}
	return rows, nil
}

// ListRecentExecutions 最近工作流执行（跨工作流，倒序）。
func (r *DashboardRepo) ListRecentExecutions(ctx context.Context, limit int) ([]domain.WorkflowExecutionDO, error) {
	if limit <= 0 || limit > 50 {
		limit = 8
	}
	var rows []domain.WorkflowExecutionDO
	if err := r.db.WithContext(ctx).Order("started_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, pkg.Wrap(2010, "list recent executions failed", err)
	}
	return rows, nil
}

// Trend 最近 range 天的会话/消息/token 趋势（按天聚合，平行数组对齐）。
func (r *DashboardRepo) Trend(ctx context.Context, days int) (*domain.DashboardTrendRESP, error) {
	if days <= 0 || days > 90 {
		days = 7
	}
	now := time.Now()
	out := &domain.DashboardTrendRESP{
		Days:     make([]string, 0, days),
		Sessions: make([]int64, 0, days),
		Messages: make([]int64, 0, days),
		Tokens:   make([]int64, 0, days),
	}
	for i := days - 1; i >= 0; i-- {
		day := now.AddDate(0, 0, -i)
		start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, now.Location())
		end := start.AddDate(0, 0, 1)

		var sessions, messages, tokens int64
		if err := r.db.WithContext(ctx).Model(&domain.ChatSessionDO{}).
			Where("created_at >= ? AND created_at < ? AND deleted_at = ?", start.UnixMilli(), end.UnixMilli(), 0).
			Count(&sessions).Error; err != nil {
			return nil, pkg.Wrap(2010, "trend sessions failed", err)
		}
		if err := r.db.WithContext(ctx).Model(&domain.MessageDO{}).
			Where("created_at >= ? AND created_at < ?", start.UnixMilli(), end.UnixMilli()).
			Count(&messages).Error; err != nil {
			return nil, pkg.Wrap(2010, "trend messages failed", err)
		}
		if err := r.db.WithContext(ctx).Model(&domain.MessageDO{}).
			Where("created_at >= ? AND created_at < ?", start.UnixMilli(), end.UnixMilli()).
			Select("COALESCE(SUM(total_tokens), 0)").
			Scan(&tokens).Error; err != nil {
			return nil, pkg.Wrap(2010, "trend tokens failed", err)
		}
		out.Days = append(out.Days, start.Format("01-02"))
		out.Sessions = append(out.Sessions, sessions)
		out.Messages = append(out.Messages, messages)
		out.Tokens = append(out.Tokens, tokens)
	}
	return out, nil
}
