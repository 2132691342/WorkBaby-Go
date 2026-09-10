package repo

import (
	"context"
	"errors"

	"WorkBaby/internal/db"
	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"gorm.io/gorm"
)

// MemoryProcedureRepo memory_procedures 表（程序记忆）。
type MemoryProcedureRepo struct{ db *gorm.DB }

func NewMemoryProcedureRepo(db *gorm.DB) *MemoryProcedureRepo { return &MemoryProcedureRepo{db: db} }

// List 取全部程序（最近使用优先）。
func (r *MemoryProcedureRepo) List(ctx context.Context, limit int) ([]domain.MemoryProcedureDO, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var rows []domain.MemoryProcedureDO
	if err := r.db.WithContext(ctx).Order("last_used_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, pkg.Wrap(6061, "list memory procedures failed", err)
	}
	return rows, nil
}

// GetByName 按名取（同名视为同一程序，覆盖更新）。
func (r *MemoryProcedureRepo) GetByName(ctx context.Context, name string) (*domain.MemoryProcedureDO, error) {
	var row domain.MemoryProcedureDO
	err := r.db.WithContext(ctx).Where("name = ?", name).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, pkg.Wrap(6061, "get memory procedure failed", err)
	}
	return &row, nil
}

// Upsert 新增或覆盖；FTS 索引同事务维护（先删后插，覆盖更新后不留陈旧索引）。
func (r *MemoryProcedureRepo) Upsert(ctx context.Context, row *domain.MemoryProcedureDO) error {
	old, err := r.GetByName(ctx, row.Name)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if old != nil {
			row.ID = old.ID
			fields := map[string]any{
				"steps":         row.Steps,
				"success_count": row.SuccessCount,
				"failure_count": row.FailureCount,
				"last_used_at":  row.LastUsedAt,
				"updated_at":    row.UpdatedAt,
			}
			if err := tx.Model(&domain.MemoryProcedureDO{}).
				Where("id = ?", row.ID).Updates(fields).Error; err != nil {
				return pkg.Wrap(6060, "update memory procedure failed", err)
			}
			return indexProcedure(tx, row.ID, row.Name, row.Steps)
		}
		if err := tx.Create(row).Error; err != nil {
			return pkg.Wrap(6060, "insert memory procedure failed", err)
		}
		return indexProcedure(tx, row.ID, row.Name, row.Steps)
	})
}

// Delete 删除单条（含 FTS 行）。
func (r *MemoryProcedureRepo) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", id).
			Delete(&domain.MemoryProcedureDO{}).Error; err != nil {
			return pkg.Wrap(6062, "delete memory procedure failed", err)
		}
		if err := tx.Exec("DELETE FROM memory_procedures_fts WHERE procedure_id = ?", id).Error; err != nil {
			return pkg.Wrap(6062, "delete memory procedure fts failed", err)
		}
		return nil
	})
}

// SearchFTS 全文检索（BM25）；query 无可用 token 时返回空，由上层决定兜底策略。
func (r *MemoryProcedureRepo) SearchFTS(ctx context.Context, query string, topK int) ([]domain.MemoryProcedureDO, error) {
	match := pkg.BuildMatchQuery(query)
	if match == "" {
		return nil, nil
	}
	if topK <= 0 || topK > 100 {
		topK = 10
	}
	_ = db.EnsureFTS5Table(r.db, "memory_procedures_fts")
	var rows []domain.MemoryProcedureDO
	if err := r.db.WithContext(ctx).Raw(`
		SELECT mp.* FROM memory_procedures mp
		JOIN memory_procedures_fts f ON f.procedure_id = mp.id
		WHERE memory_procedures_fts MATCH ?
		ORDER BY f.rank
		LIMIT ?`, match, topK).Scan(&rows).Error; err != nil {
		return nil, pkg.Wrap(6063, "search memory procedures fts failed", err)
	}
	return rows, nil
}

// indexProcedure 同步单条程序到 FTS 索引。
func indexProcedure(tx *gorm.DB, id, name, steps string) error {
	if err := tx.Exec("DELETE FROM memory_procedures_fts WHERE procedure_id = ?", id).Error; err != nil {
		return pkg.Wrap(6063, "sync memory procedure fts (delete) failed", err)
	}
	if err := tx.Exec("INSERT INTO memory_procedures_fts(procedure_id, name, steps) VALUES (?, ?, ?)",
		id, name, steps).Error; err != nil {
		return pkg.Wrap(6063, "sync memory procedure fts (insert) failed", err)
	}
	return nil
}

// Count 总数。
func (r *MemoryProcedureRepo) Count(ctx context.Context) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&domain.MemoryProcedureDO{}).Count(&n).Error; err != nil {
		return 0, pkg.Wrap(6061, "count memory procedures failed", err)
	}
	return n, nil
}
