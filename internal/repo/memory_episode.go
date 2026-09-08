// Package repo 是 GORM 持久化层；禁止引 service / api / 能力域（CLAUDE §2.2）。
package repo

import (
	"context"
	"strings"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"gorm.io/gorm"
)

// MemoryEpisodeRepo memory_episodes 表 + FTS5 同步维护。
type MemoryEpisodeRepo struct{ db *gorm.DB }

// NewMemoryEpisodeRepo 构造仓储。
func NewMemoryEpisodeRepo(db *gorm.DB) *MemoryEpisodeRepo { return &MemoryEpisodeRepo{db: db} }

// Insert 写入情景记忆并同步 FTS5 索引（同事务，DB 是真相源）。
func (r *MemoryEpisodeRepo) Insert(ctx context.Context, row *domain.MemoryEpisodeDO) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(row).Error; err != nil {
			return pkg.Wrap(6001, "insert memory episode failed", err)
		}
		// FTS5 与主表同事务（memory_episodes_fts 的 episode_id 是 UNINDEXED 列）
		if err := tx.Exec(
			"INSERT INTO memory_episodes_fts(episode_id, summary) VALUES (?, ?)",
			row.ID, row.Summary,
		).Error; err != nil {
			return pkg.Wrap(6001, "sync memory episode fts failed", err)
		}
		return nil
	})
}

// SearchFTS trigram 检索摘要；中文友好（trigram 子串匹配需双引号包裹）。
// 返回按 rank 排序的命中（最多 topK 条）。
func (r *MemoryEpisodeRepo) SearchFTS(ctx context.Context, query string, topK int) ([]domain.MemoryEpisodeDO, error) {
	if topK <= 0 || topK > 100 {
		topK = 5
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	// 按词 OR 检索（与知识库同一口径，见 pkg.BuildMatchQuery），按 rank 排序
	match := pkg.BuildMatchQuery(q)
	if match == "" {
		return r.searchEpisodeLike(ctx, q, topK)
	}
	var rows []domain.MemoryEpisodeDO
	if err := r.db.WithContext(ctx).Raw(`
		SELECT me.* FROM memory_episodes me
		JOIN memory_episodes_fts f ON f.episode_id = me.id
		WHERE memory_episodes_fts MATCH ?
		ORDER BY f.rank
		LIMIT ?`, match, topK).Scan(&rows).Error; err != nil {
		return nil, pkg.Wrap(6004, "search memory fts failed", err)
	}
	return rows, nil
}

// searchEpisodeLike 兜底：所有 token 都短于 trigram 最小窗口（如 2 字中文「部署」）时
// 走 LIKE 子串扫描，本地单库规模下代价可接受。
func (r *MemoryEpisodeRepo) searchEpisodeLike(ctx context.Context, q string, topK int) ([]domain.MemoryEpisodeDO, error) {
	like := "%" + pkg.EscapeLike(q) + "%"
	var rows []domain.MemoryEpisodeDO
	if err := r.db.WithContext(ctx).Raw(`
		SELECT me.* FROM memory_episodes me
		WHERE me.summary LIKE ? ESCAPE '\'
		ORDER BY me.created_at DESC
		LIMIT ?`, like, topK).Scan(&rows).Error; err != nil {
		return nil, pkg.Wrap(6004, "search memory like failed", err)
	}
	return rows, nil
}

// ListBySession 某会话的最近 episodes（供记忆面板展示）。
func (r *MemoryEpisodeRepo) ListBySession(ctx context.Context, sessionID string, limit int) ([]domain.MemoryEpisodeDO, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var rows []domain.MemoryEpisodeDO
	if err := r.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("created_at DESC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, pkg.Wrap(6004, "list memory episodes failed", err)
	}
	return rows, nil
}

// Delete 删除 episode（软删 + FTS5 同步删）。
func (r *MemoryEpisodeRepo) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", id).Delete(&domain.MemoryEpisodeDO{}).Error; err != nil {
			return pkg.Wrap(6005, "delete memory episode failed", err)
		}
		// FTS5 是虚拟表不支持软删概念，直接删行
		if err := tx.Exec("DELETE FROM memory_episodes_fts WHERE episode_id = ?", id).Error; err != nil {
			return pkg.Wrap(6005, "delete memory episode fts failed", err)
		}
		return nil
	})
}

// ClearAll 清空全部情景记忆（用户主动清理）。
func (r *MemoryEpisodeRepo) ClearAll(ctx context.Context) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).
			Delete(&domain.MemoryEpisodeDO{}).Error; err != nil {
			return pkg.Wrap(6005, "clear memory episodes failed", err)
		}
		if err := tx.Exec("DELETE FROM memory_episodes_fts").Error; err != nil {
			return pkg.Wrap(6005, "clear memory episodes fts failed", err)
		}
		return nil
	})
}

// Count 情景记忆总数（面板展示）。
func (r *MemoryEpisodeRepo) Count(ctx context.Context) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&domain.MemoryEpisodeDO{}).Count(&n).Error; err != nil {
		return 0, pkg.Wrap(6004, "count memory episodes failed", err)
	}
	return n, nil
}
