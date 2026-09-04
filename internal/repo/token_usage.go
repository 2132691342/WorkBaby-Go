package repo

import (
	"context"
	"time"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"gorm.io/gorm"
)

// TokenUsageRepo token 明细持久化 + 区间聚合。
type TokenUsageRepo struct{ db *gorm.DB }

// NewTokenUsageRepo 构造。
func NewTokenUsageRepo(db *gorm.DB) *TokenUsageRepo { return &TokenUsageRepo{db: db} }

// BatchCreate 批量写入单次 run 的全部调用明细；空切片直接返回。
func (r *TokenUsageRepo) BatchCreate(ctx context.Context, rows []domain.TokenUsageDO) error {
	if len(rows) == 0 {
		return nil
	}
	if err := r.db.WithContext(ctx).Create(&rows).Error; err != nil {
		return pkg.Wrap(2010, "save token usage failed", err)
	}
	return nil
}

// ListBySession 按会话查明细（消息详情/导出用）。
func (r *TokenUsageRepo) ListBySession(ctx context.Context, sessionID string, limit int) ([]domain.TokenUsageDO, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows []domain.TokenUsageDO
	if err := r.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("created_at ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, pkg.Wrap(2010, "list token usage failed", err)
	}
	return rows, nil
}

// Summarize 区间合计。
func (r *TokenUsageRepo) Summarize(ctx context.Context, startMs, endMs int64) (domain.TokenSummaryRESP, error) {
	var out domain.TokenSummaryRESP
	row := struct {
		Input  int64 `gorm:"column:input"`
		Output int64 `gorm:"column:output"`
		Cache  int64 `gorm:"column:cache"`
		Calls  int64 `gorm:"column:calls"`
	}{}
	if err := r.db.WithContext(ctx).Model(&domain.TokenUsageDO{}).
		Where("created_at >= ? AND created_at < ?", startMs, endMs).
		Select(`COALESCE(SUM(input_tokens),0) AS input,
		        COALESCE(SUM(output_tokens),0) AS output,
		        COALESCE(SUM(cache_read_tokens),0) AS cache,
		        COUNT(1) AS calls`).
		Scan(&row).Error; err != nil {
		return out, pkg.Wrap(2010, "summarize token usage failed", err)
	}
	out.InputTokens = row.Input
	out.OutputTokens = row.Output
	out.CacheReadTokens = row.Cache
	// 总消耗 = 输入 + 输出。缓存命中是输入的一个拆解维度（OpenAI 的 cached_tokens
	// 本身已计入 prompt_tokens），再加一次会重复计数。
	out.TotalTokens = row.Input + row.Output
	out.Calls = row.Calls
	return out, nil
}

// Aggregate 按桶聚合：
//   - granularity=hour：桶起点按「小时」取整（created_at / 3600000）
//   - granularity=day：桶起点按「自然日零点」取整（SQLite 无时区，用偏移修正到东八以外的本地日界）
//
// 返回稀疏桶（无数据的桶不出现），由 service 补齐空桶。
func (r *TokenUsageRepo) Aggregate(ctx context.Context, startMs, endMs int64, gran domain.TokenTrendBucket, zoneOffsetMs int64) ([]domain.TokenBucketDTO, error) {
	stepMs := int64(time.Hour.Milliseconds())
	if gran == domain.TrendBucketDay {
		stepMs = int64(24 * time.Hour.Milliseconds())
	}
	var rows []domain.TokenBucketDTO
	if err := r.db.WithContext(ctx).Model(&domain.TokenUsageDO{}).
		Where("created_at >= ? AND created_at < ?", startMs, endMs).
		Select("((created_at + ?) / ? * ?) AS bucket_ms, "+
			"COALESCE(SUM(input_tokens),0) AS input_tokens, "+
			"COALESCE(SUM(output_tokens),0) AS output_tokens, "+
			"COALESCE(SUM(cache_read_tokens),0) AS cache_read_tokens",
			zoneOffsetMs, stepMs, stepMs).
		Group("bucket_ms").Order("bucket_ms ASC").Scan(&rows).Error; err != nil {
		return nil, pkg.Wrap(2010, "aggregate token usage failed", err)
	}
	return rows, nil
}
