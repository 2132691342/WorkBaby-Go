package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"WorkBaby/internal/db"
	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"gorm.io/gorm"
)

// MemoryFactRepo memory_facts 表（语义记忆）。
type MemoryFactRepo struct {
	db *gorm.DB
	// ftsOnce 实例级惰性建表守护：db/migrate.go CreateFTS5 启动期会建，
	// 这里兜底保证测试/独立实例写路径不因缺 FTS 虚拟表失败（CREATE IF NOT EXISTS 幂等）。
	ftsOnce sync.Once
}

func NewMemoryFactRepo(db *gorm.DB) *MemoryFactRepo { return &MemoryFactRepo{db: db} }

func (r *MemoryFactRepo) prepareFactFTS() {
	r.ftsOnce.Do(func() {
		_ = db.EnsureFTS5Table(r.db, "memory_facts_fts")
	})
}

// List 取全部事实（按 subject/key 排序）。
func (r *MemoryFactRepo) List(ctx context.Context, limit int) ([]domain.MemoryFactDO, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var rows []domain.MemoryFactDO
	if err := r.db.WithContext(ctx).Order("subject ASC, key ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, pkg.Wrap(6061, "list memory facts failed", err)
	}
	return rows, nil
}

// GetBySubjectKey 取单条（同 subject+key 视为同一事实，覆盖更新）。
func (r *MemoryFactRepo) GetBySubjectKey(ctx context.Context, subject, key string) (*domain.MemoryFactDO, error) {
	var row domain.MemoryFactDO
	err := r.db.WithContext(ctx).Where("subject = ? AND key = ?", subject, key).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, pkg.Wrap(6061, "get memory fact failed", err)
	}
	return &row, nil
}

// indexFact 在事务内同步事实行到 FTS 索引（先删后插，保证覆盖更新后无陈旧索引）。
func indexFact(tx *gorm.DB, id, subject, key, value string) error {
	if err := tx.Exec("DELETE FROM memory_facts_fts WHERE fact_id = ?", id).Error; err != nil {
		return pkg.Wrap(6063, "sync memory fact fts (delete) failed", err)
	}
	if err := tx.Exec(
		"INSERT INTO memory_facts_fts(fact_id, subject, key, value) VALUES (?, ?, ?, ?)",
		id, subject, key, value,
	).Error; err != nil {
		return pkg.Wrap(6063, "sync memory fact fts (insert) failed", err)
	}
	return nil
}

// Upsert 新增或覆盖（同 subject+key 的唯一语义）；FTS 索引与主表同事务（semantic 并入 FTS5）。
func (r *MemoryFactRepo) Upsert(ctx context.Context, row *domain.MemoryFactDO) error {
	r.prepareFactFTS()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var old domain.MemoryFactDO
		err := tx.Where("subject = ? AND key = ?", row.Subject, row.Key).First(&old).Error
		switch {
		case err == nil:
			row.ID = old.ID
			if uerr := tx.Model(&domain.MemoryFactDO{}).
				Where("id = ?", row.ID).
				Updates(map[string]any{
					"value": row.Value, "confidence": row.Confidence,
					"source": row.Source, "updated_at": row.UpdatedAt,
				}).Error; uerr != nil {
				return pkg.Wrap(6060, "update memory fact failed", uerr)
			}
			return indexFact(tx, row.ID, row.Subject, row.Key, row.Value)
		case errors.Is(err, gorm.ErrRecordNotFound):
			if cerr := tx.Create(row).Error; cerr != nil {
				return pkg.Wrap(6060, "insert memory fact failed", cerr)
			}
			return indexFact(tx, row.ID, row.Subject, row.Key, row.Value)
		default:
			return pkg.Wrap(6061, "get memory fact failed", err)
		}
	})
}

// Delete 删除单条（主表 + FTS 索引同事务）。
func (r *MemoryFactRepo) Delete(ctx context.Context, id string) error {
	r.prepareFactFTS()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", id).Delete(&domain.MemoryFactDO{}).Error; err != nil {
			return pkg.Wrap(6062, "delete memory fact failed", err)
		}
		if err := tx.Exec("DELETE FROM memory_facts_fts WHERE fact_id = ?", id).Error; err != nil {
			return pkg.Wrap(6062, "delete memory fact fts failed", err)
		}
		return nil
	})
}

// Count 总数。
func (r *MemoryFactRepo) Count(ctx context.Context) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&domain.MemoryFactDO{}).Count(&n).Error; err != nil {
		return 0, pkg.Wrap(6061, "count memory facts failed", err)
	}
	return n, nil
}

// SearchFTS 全文检索：trigram 对 subject/key/value 任一列做子串命中，BM25 排序。
// trigram 词元要求 ≥3 字符，短查询（<3 字符或纯英文短词）可能零命中 → 调用方应回退子串扫描。
func (r *MemoryFactRepo) SearchFTS(ctx context.Context, query string, topK int) ([]domain.MemoryFactDO, error) {
	r.prepareFactFTS()
	if topK <= 0 || topK > 100 {
		topK = 20
	}
	// 按词 × 按列 OR 检索（与知识库同一口径）：整串 phrase 要求逐字出现在
	// subject/key/value 里，换述即零命中，语义记忆的跨会话召回因此基本失效。
	toks := pkg.MatchTokens(query)
	if len(toks) == 0 {
		// 上层 semantic.go 已有子串扫描回退，这里直接交给它
		return nil, nil
	}
	join := func(col string) string {
		parts := make([]string, 0, len(toks))
		for _, t := range toks {
			// token 由 MatchTokens 保证只含字母数字，无双引号注入面
			parts = append(parts, fmt.Sprintf(`%s:"%s"`, col, t))
		}
		return "(" + strings.Join(parts, " OR ") + ")"
	}
	match := join("subject") + " OR " + join("key") + " OR " + join("value")
	var rows []domain.MemoryFactDO
	if err := r.db.WithContext(ctx).Raw(`
		SELECT mf.* FROM memory_facts mf
		JOIN memory_facts_fts f ON f.fact_id = mf.id
		WHERE memory_facts_fts MATCH ?
		ORDER BY f.rank
		LIMIT ?`, match, topK).Scan(&rows).Error; err != nil {
		return nil, pkg.Wrap(6063, "search memory facts fts failed", err)
	}
	return rows, nil
}
