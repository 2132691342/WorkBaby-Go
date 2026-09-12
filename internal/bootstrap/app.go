// Package bootstrap 组合根：repo 实例的唯一装配点。
//
// api 层经 App 持有数据访问句柄，自身不再 import repo——业务数据访问必须经 service 层
// （check-boundaries 的 api-no-repo 断言）。App 只做构造与持有，不含任何业务逻辑。
package bootstrap

import (
	"WorkBaby/internal/repo"

	"gorm.io/gorm"
)

// App 持有全部 repo 实例；由 api.Handler.Startup 在数据库打开后构造一次。
type App struct {
	DB *gorm.DB

	SessRepo             *repo.ChatSessionRepo
	MsgRepo              *repo.MessageRepo
	ProvRepo             *repo.AiProviderRepo
	SetRepo              *repo.SystemSettingRepo
	UsageRepo            *repo.TokenUsageRepo
	BlocksRepo           *repo.MessageBlockRepo
	CheckpointRepo       *repo.AgentCheckpointRepo
	RunRecRepo           *repo.RunRecordRepo
	TodoRepo             *repo.SessionTodoRepo
	SessionVarRepo       *repo.SessionVariableRepo
	ApprovalRecRepo      *repo.ApprovalRecordRepo
	ApprovalGrantRepo    *repo.ApprovalGrantRepo
	TrustRepo            *repo.WorkspaceTrustRepo
	WorkflowRepo         *repo.WorkflowRepo
	WorkflowExecRepo     *repo.WorkflowExecutionRepo
	WorkflowNodeExecRepo *repo.WorkflowNodeExecutionRepo
	CronRepo             *repo.CronJobRepo
	CronRunLogRepo       *repo.CronJobRunLogRepo
	ChannelRepo          *repo.ChannelConfigRepo
	ChannelLogRepo       *repo.ChannelMessageLogRepo
	PetCfgRepo           *repo.PetConfigRepo
	PetSpriteRepo        *repo.PetSpriteRepo
	FolderRepo           *repo.FolderRepo
	FileRepo             *repo.FileRepo
	FileChangeRepo       *repo.FileChangeRepo
	ArtifactRepo         *repo.ArtifactRepo
	DashboardRepo        *repo.DashboardRepo
	KnowledgeDocRepo     *repo.KnowledgeDocRepo
	MemoryEpisodeRepo    *repo.MemoryEpisodeRepo
	MemoryFactRepo       *repo.MemoryFactRepo
	MemoryProcedureRepo  *repo.MemoryProcedureRepo
	InboxRepo            *repo.InboxRepo
	SkillRepo            *repo.SkillRepo
	McpRepo              *repo.McpServerRepo
}

// New 构造组合根：打开数据库后调用一次，全部 repo 共享同一连接。
func New(db *gorm.DB) *App {
	return &App{
		DB:                   db,
		SessRepo:             repo.NewChatSessionRepo(db),
		MsgRepo:              repo.NewMessageRepo(db),
		ProvRepo:             repo.NewAiProviderRepo(db),
		SetRepo:              repo.NewSystemSettingRepo(db),
		UsageRepo:            repo.NewTokenUsageRepo(db),
		BlocksRepo:           repo.NewMessageBlockRepo(db),
		CheckpointRepo:       repo.NewAgentCheckpointRepo(db),
		RunRecRepo:           repo.NewRunRecordRepo(db),
		TodoRepo:             repo.NewSessionTodoRepo(db),
		SessionVarRepo:       repo.NewSessionVariableRepo(db),
		ApprovalRecRepo:      repo.NewApprovalRecordRepo(db),
		ApprovalGrantRepo:    repo.NewApprovalGrantRepo(db),
		TrustRepo:            repo.NewWorkspaceTrustRepo(db),
		WorkflowRepo:         repo.NewWorkflowRepo(db),
		WorkflowExecRepo:     repo.NewWorkflowExecutionRepo(db),
		WorkflowNodeExecRepo: repo.NewWorkflowNodeExecutionRepo(db),
		CronRepo:             repo.NewCronJobRepo(db),
		CronRunLogRepo:       repo.NewCronJobRunLogRepo(db),
		ChannelRepo:          repo.NewChannelConfigRepo(db),
		ChannelLogRepo:       repo.NewChannelMessageLogRepo(db),
		PetCfgRepo:           repo.NewPetConfigRepo(db),
		PetSpriteRepo:        repo.NewPetSpriteRepo(db),
		FolderRepo:           repo.NewFolderRepo(db),
		FileRepo:             repo.NewFileRepo(db),
		FileChangeRepo:       repo.NewFileChangeRepo(db),
		ArtifactRepo:         repo.NewArtifactRepo(db),
		DashboardRepo:        repo.NewDashboardRepo(db),
		KnowledgeDocRepo:     repo.NewKnowledgeDocRepo(db),
		MemoryEpisodeRepo:    repo.NewMemoryEpisodeRepo(db),
		MemoryFactRepo:       repo.NewMemoryFactRepo(db),
		MemoryProcedureRepo:  repo.NewMemoryProcedureRepo(db),
		InboxRepo:            repo.NewInboxRepo(db),
		SkillRepo:            repo.NewSkillRepo(db),
		McpRepo:              repo.NewMcpServerRepo(db),
	}
}
