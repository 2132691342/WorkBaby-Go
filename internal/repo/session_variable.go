package repo

import (
	"context"
	"time"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SessionVariableRepo session_variables 表 CRUD：会话变量的持久化真相源。
type SessionVariableRepo struct{ db *gorm.DB }

// NewSessionVariableRepo 构造仓储。
func NewSessionVariableRepo(db *gorm.DB) *SessionVariableRepo { return &SessionVariableRepo{db: db} }

// List 按 key 升序返回会话变量。
func (r *SessionVariableRepo) List(ctx context.Context, sessionID string) ([]domain.SessionVariableDO, error) {
	var rows []domain.SessionVariableDO
	if err := r.db.WithContext(ctx).Where("session_id = ?", sessionID).
		Order("key ASC").Find(&rows).Error; err != nil {
		return nil, pkg.Wrap(2066, "list session variables failed", err)
	}
	return rows, nil
}

// Set 写入或覆盖一个变量。
//
// 复合主键走 OnConflict 而非 Save：Save 在「主键非零」时只发 UPDATE，
// 记录不存在就静默不插入，达不到 upsert 语义。
func (r *SessionVariableRepo) Set(ctx context.Context, sessionID, key, value string) error {
	row := domain.SessionVariableDO{
		SessionID: sessionID,
		Key:       key,
		Value:     value,
		UpdatedAt: time.Now().UnixMilli(),
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "session_id"}, {Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&row).Error; err != nil {
		return pkg.Wrap(2066, "set session variable failed", err)
	}
	return nil
}

// Delete 删除一个变量；返回是否命中。
func (r *SessionVariableRepo) Delete(ctx context.Context, sessionID, key string) (bool, error) {
	res := r.db.WithContext(ctx).
		Where("session_id = ? AND key = ?", sessionID, key).
		Delete(&domain.SessionVariableDO{})
	if res.Error != nil {
		return false, pkg.Wrap(2066, "delete session variable failed", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// Count 会话变量数量（写入前做上限校验）。
func (r *SessionVariableRepo) Count(ctx context.Context, sessionID string) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&domain.SessionVariableDO{}).
		Where("session_id = ?", sessionID).Count(&n).Error; err != nil {
		return 0, pkg.Wrap(2066, "count session variables failed", err)
	}
	return n, nil
}

// DeleteBySession 清理会话变量（会话删除时调用）。
func (r *SessionVariableRepo) DeleteBySession(ctx context.Context, sessionID string) error {
	if err := r.db.WithContext(ctx).Where("session_id = ?", sessionID).
		Delete(&domain.SessionVariableDO{}).Error; err != nil {
		return pkg.Wrap(2066, "delete session variables failed", err)
	}
	return nil
}
