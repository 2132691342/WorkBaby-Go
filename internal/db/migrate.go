// Package db 的迁移入口：GORM AutoMigrate 业务表 + 原生 SQL 建 FTS5 虚拟表。
package db

import (
	"fmt"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"gorm.io/gorm"
)

// Migrate 启动期调用；AutoMigrate 失败即 fail-fast（半残比不可用更危险）。
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&domain.AiProviderDO{},
		&domain.ChatSessionDO{},
		&domain.MessageDO{},
		&domain.MessageBlockDO{},
		&domain.TokenUsageDO{},
		&domain.SystemSettingDO{},
		&domain.MemoryEpisodeDO{},
		&domain.MemoryFactDO{},
		&domain.MemoryProcedureDO{},
		&domain.SkillDO{},
		&domain.McpServerDO{},
		&domain.KnowledgeDocDO{},
		&domain.KnowledgeChunkDO{},
		&domain.WorkflowDO{},
		&domain.WorkflowExecutionDO{},
		&domain.WorkflowNodeExecutionDO{},
		&domain.AgentCheckpointDO{},
		&domain.RunRecordDO{},
		&domain.ApprovalRecordDO{},
		&domain.ChannelConfigDO{},
		&domain.ChannelMessageLogDO{},
		&domain.CronJobDO{},
		&domain.PetConfigDO{},
		&domain.PetSpriteDO{},
		&domain.FolderDO{},
		&domain.FileDO{},
		&domain.FileChangeDO{},
		&domain.ArtifactDO{},
		&domain.WorkspaceTrustDO{},
	); err != nil {
		return pkg.Wrap(2012, "automigrate failed", err)
	}
	return nil
}

// CreateFTS5 建记忆/知识库的 FTS5 虚拟表（AutoMigrate 不覆盖，启动期单独执行）。
func CreateFTS5(db *gorm.DB) error {
	stmts := []string{
		"CREATE VIRTUAL TABLE IF NOT EXISTS memory_episodes_fts USING fts5(episode_id UNINDEXED, summary, tokenize='trigram')",
		"CREATE VIRTUAL TABLE IF NOT EXISTS memory_facts_fts USING fts5(fact_id UNINDEXED, subject, key, value, tokenize='trigram')",
		"CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_chunks_fts USING fts5(id UNINDEXED, doc_id UNINDEXED, chunk_idx UNINDEXED, title, content, tokenize='trigram')",
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			return pkg.Wrap(2013, "create fts5 failed", fmt.Errorf("%s: %w", s, err))
		}
	}
	return nil
}
