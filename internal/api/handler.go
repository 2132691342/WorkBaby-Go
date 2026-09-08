// Package api 是 Wails 绑定层（薄；一个文件一个功能域，全平铺）；唯一直接 import wails runtime 的层。
package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"WorkBaby/internal/capability"
	"WorkBaby/internal/channel"
	"WorkBaby/internal/channel/email"
	"WorkBaby/internal/channel/webhook"
	"WorkBaby/internal/config"
	cronjob "WorkBaby/internal/cron"
	"WorkBaby/internal/db"
	"WorkBaby/internal/domain"
	"WorkBaby/internal/event"
	"WorkBaby/internal/harness"
	"WorkBaby/internal/llm/registry"
	"WorkBaby/internal/mcp"
	"WorkBaby/internal/memory"
	"WorkBaby/internal/pet"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/rag"
	"WorkBaby/internal/repo"
	"WorkBaby/internal/runtime"
	"WorkBaby/internal/service"
	"WorkBaby/internal/skill"
	"WorkBaby/internal/tool"
	archivetool "WorkBaby/internal/tool/archive"
	delegatetool "WorkBaby/internal/tool/delegate"
	doctool "WorkBaby/internal/tool/doc"
	exectool "WorkBaby/internal/tool/exec"
	filetool "WorkBaby/internal/tool/file"
	functools "WorkBaby/internal/tool/functools"
	httptool "WorkBaby/internal/tool/http"
	requestinput "WorkBaby/internal/tool/requestinput"
	skillrun "WorkBaby/internal/tool/skillrun"
	todotool "WorkBaby/internal/tool/todo"
	webfetchtool "WorkBaby/internal/tool/webfetch"
	websearchtool "WorkBaby/internal/tool/websearch"
	"WorkBaby/internal/workflow"
	wnodes "WorkBaby/internal/workflow/nodes"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Handler 聚合所有 service，对外导出方法自动成为 Wails 绑定。
// 本包允许唯一直接 import wails runtime，且仅用于 runtime.EventsEmit；
// service / repo / domain 严禁导入 wails。
type Handler struct {
	ctx          context.Context
	paths        *runtime.Paths
	runtimeMgr   *runtime.Manager
	cfg          *config.Config
	bus          *event.Bus
	eventLog     *event.RunEventLog         // run 事件日志（序号 + 断线重放缓冲），与 SSE hub 共享
	execs        *harness.ExecutionRegistry // 执行平面：全入口统一 run 登记
	cipher       *pkg.Cipher
	reg          *registry.Registry
	sessRepo     *repo.ChatSessionRepo
	msgRepo      *repo.MessageRepo
	provRepo     *repo.AiProviderRepo
	setRepo      *repo.SystemSettingRepo
	usageRepo    *repo.TokenUsageRepo
	metaSvc      *service.MetaService
	provSvc      *service.ProviderService
	chatSvc      *service.ChatService
	todoStore    *service.SessionTodoStore // 会话计划存储（todo 工具 + 前端共享）
	setSvc       *service.SettingsService
	toolSvc      *service.ToolService
	dashSvc      *service.DashboardService
	docsSvc      *service.DocsService
	approvalSvc  *service.ApprovalService
	memSvc       *memory.Service
	memProxy     *service.MemoryService
	skillSvc     *service.SkillService
	mcpSvc       *service.McpService
	knowledgeSvc *service.KnowledgeService
	workflowSvc  *service.WorkflowService
	channelSvc   *channel.Service
	cronSvc      *cronjob.Scheduler
	petSvc       *pet.Service
	petCtrl      *pet.Controller
	folderSvc    *service.FolderService
	fileSvc      *service.FileService
	workspaceSvc *service.WorkspaceService
	changeSvc    *service.FileChangeService // 文件变更追踪（快照 + diff + 回滚）
	artifactSvc  *service.ArtifactService   // 会话产出物登记
	taskSvc      *service.TaskService       // 后台任务队列
	trustSvc     *service.TrustService      // 工作目录信任
	// Files 服务本地受管文件（main.go AssetServer 转发 /files/**）。
	// 独立类型而非 Handler 方法：避免 net/http 类型泄漏进 Wails 绑定（见 fileserver.go）。
	Files        *FileServer
	petMode      bool // 当前是否桌宠形态（v1 单窗口切换）
	petX, petY   int
	mainX, mainY int
	mainW, mainH int
}

// NewHandler 构造壳。
func NewHandler() *Handler {
	return &Handler{
		bus:      event.New(),
		eventLog: event.NewRunEventLog(0, 0),
		execs:    harness.NewExecutionRegistry(0),
	}
}

// Events 暴露应用内事件总线（双主机：server/sse 订阅桥接）。
func (h *Handler) Events() *event.Bus { return h.bus }

// EventLog 暴露 run 事件日志（server/sse 用它做断线重放）。
func (h *Handler) EventLog() *event.RunEventLog { return h.eventLog }

// ExecutionRegistry 暴露执行平面注册表（供 chat / workflow / cron 各入口登记统一 run 拓扑）。
func (h *Handler) ExecutionRegistry() *harness.ExecutionRegistry { return h.execs }

// isRegularFile 判断路径是常规文件（非目录）。
func isRegularFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// EmitReady 在 gin server 启动后发射 app:ready（携带 serverPort 供前端建立 HTTP 连接）。
func (h *Handler) EmitReady(serverPort int) {
	if h.ctx == nil {
		return
	}
	wruntime.EventsEmit(h.ctx, "app:ready", map[string]any{
		"home":        h.paths.Home,
		"version":     h.cfg.App.Version,
		"phase":       "dual-host",
		"server_port": serverPort,
	})
}

// Startup 按序装配：路径 → 日志 → 配置 → 主密钥 → DB → 迁移 → repo/service → LLM registry → 事件桥接 → app:ready。
func (h *Handler) Startup(ctx context.Context) error {
	paths, err := runtime.Resolve()
	if err != nil {
		return err
	}
	h.paths = paths

	if err := pkg.Init(paths.Log); err != nil {
		return err
	}

	// 内置运行时（node/python/pwsh）：后台首跑解压，不阻塞启动；失败仅告警
	rt := runtime.NewManager(paths.Home)
	// 事件 JSONL 无头导出：每 run 一文件，供回放/测试/自动化消费
	h.eventLog.WithFileSink(filepath.Join(paths.Home, "runs"))
	h.runtimeMgr = rt
	// 同步等待内置运行时解压完成：MCP.Sync 在下方接着调用，子进程 PATH 注入依赖
	// rt.BinDirs()；解压异步时 MCP server 启动时 node/python 路径尚未就绪，导致
	// npx / uvx 类 server 只能依赖系统 PATH。Ensure 失败仅告警（解包失败不应阻断
	// 启动，但要让用户在日志里看到）。
	if err := rt.Ensure(); err != nil {
		pkg.L.Warn("runtime ensure failed", "err", err)
	}

	cfg, err := config.Init(paths.Cfg)
	if err != nil {
		return err
	}
	if err := config.EnsureUserConfig(paths.Home); err != nil {
		return err
	}
	// config.Init 已经确保 masterKeyB64 不空
	cipher, err := pkg.NewCipherFromB64(cfg.Security.MasterKeyB64)
	if err != nil {
		return err
	}
	h.cipher = cipher
	h.cfg = cfg
	if cfg.Database.Path == "" {
		cfg.Database.Path = paths.DB
	}

	gdb, err := db.Open(cfg.Database.Path, cfg.Database.BusyTimeoutMs)
	if err != nil {
		return err
	}
	if err := db.Migrate(gdb); err != nil {
		return err
	}
	if err := db.CreateFTS5(gdb); err != nil {
		return err
	}

	h.sessRepo = repo.NewChatSessionRepo(gdb)
	h.msgRepo = repo.NewMessageRepo(gdb)
	h.provRepo = repo.NewAiProviderRepo(gdb)
	h.setRepo = repo.NewSystemSettingRepo(gdb)
	h.usageRepo = repo.NewTokenUsageRepo(gdb)
	h.metaSvc = service.NewMetaService(cfg)
	h.provSvc = service.NewProviderService(h.provRepo, h.cipher)
	h.setSvc = service.NewSettingsService(h.setRepo, h.cipher)

	// 配置文件为源：model.json 存在则同步进 ai_providers 表
	// （文件缺失/解析失败仅告警，保留 DB 存量）
	if modelPath := service.ModelRawPath(paths.Home); isRegularFile(modelPath) {
		if rows, perr := service.ProvidersFromFile(modelPath); perr != nil {
			pkg.L.Warn("sync model.json failed", "err", perr)
		} else if serr := h.provSvc.SyncFromList(ctx, rows); serr != nil {
			pkg.L.Warn("sync model.json to db failed", "err", serr)
		}
	}

	// LLM Registry：解密已落盘 apiKey 后构建；失败单条降级（unready）
	h.reg = registry.New()
	if pwds, perr := h.provRepo.List(ctx); perr == nil {
		_ = h.reg.Build(pwds, func(encrypted string) (string, error) {
			return h.cipher.Decrypt(encrypted)
		})
		// 启动后把 enabled 但未就绪的 provider 原因打到日志，
		// 便于排查「每次重启模型都处于熔断/不可用」这类问题（多数是 apiKey 解密失败）。
		for _, it := range h.reg.Status(pwds) {
			if !it.Ready {
				pkg.L.Warn("llm registry provider not ready at startup",
					"provider", it.Name, "reason", it.Reason)
			}
		}
	}

	// 工具系统：工作区根 + 内置工具注册（exec/file/webfetch/http/websearch）
	workspace := filepath.Join(paths.Home, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return pkg.Wrap(2001, "mkdir workspace failed", err)
	}
	// 会话工作区解析：会话绑定了外部目录 → 工具沙箱/面板/exec cwd 全部跟随；
	// 未绑定时回落到 {home}/workspaces/{sessionID}（会话隔离默认区，与文件面板同源，
	// 保证「Agent 写的文件 = 面板里看到的文件」）。闭包晚绑定 chatSvc（其构造在工具注册之后）。
	wsResolver := tool.RootResolver(func(sessionID string) string {
		def := workspace
		if sessionID != "" && !strings.Contains(sessionID, "/") && !strings.Contains(sessionID, "\\") {
			def = filepath.Join(paths.Home, "workspaces", sessionID)
		}
		return h.chatSvc.WorkspaceRoot(h.ctx, sessionID, def)
	})
	// 文件变更追踪：file_write 写前落快照 + diff，前端可预览/回滚
	// 快照目录跟随会话工作区：绑定本地目录 → {dir}/.workbaby/snapshots/；默认 → {home}/snapshots/
	h.changeSvc = service.NewFileChangeService(
		repo.NewFileChangeRepo(gdb), h.bus, filepath.Join(paths.Home, "snapshots"), workspace,
	).WithSnapshotRoot(func(sessionID string) string {
		if h.chatSvc != nil {
			if _, sd := h.chatSvc.SessionDataDirs(h.ctx, sessionID); sd != "" {
				return sd
			}
		}
		return filepath.Join(paths.Home, "snapshots", sessionID)
	}).WithEventLog(h.eventLog)
	// 工件登记：产出文件只记引用，前端经 /files 预览
	h.artifactSvc = service.NewArtifactService(
		repo.NewArtifactRepo(gdb), h.bus, workspace,
	).WithEventLog(h.eventLog)
	toolReg := tool.NewRegistry()
	// 审批门：白名单外/危险命令 → 前端 chat:approval 事件确认后放行；暂停态持久化
	h.approvalSvc = service.NewApprovalService(h.bus).WithEventLog(h.eventLog).WithRecords(repo.NewApprovalRecordRepo(gdb))
	// exec 工具：白名单运行时动态读取（settings/exec/agent 设置页）；
	// cwd 缺省跟随会话工作区（绑定了外部目录时），命令与文件工具同一落点
	if err := toolReg.Register(exectool.New(tool.DefaultExecPolicy()).
		WithApprover(h.approvalSvc).
		WithRootResolver(tool.ResolveRoot(wsResolver, "")).
		WithPathDirs(rt.BinDirs).
		WithWhitelist(func() []string {
			rows, err := h.setRepo.ListAll(h.ctx)
			if err != nil {
				return []string{}
			}
			for _, r := range rows {
				if r.K == domain.SettingKeyExecWhitelist {
					var bins []string
					if json.Unmarshal([]byte(r.V), &bins) == nil {
						return bins
					}
					return []string{}
				}
			}
			return []string{}
		})); err != nil {
		return err
	}
	// Skill scripts 执行工具：SKILL.md scripts 挂成 Agent 可调用工具；
	// 脚本内容来自 skills 表，解释器固定映射，执行前过审批门
	if err := toolReg.Register(skillrun.New(
		func(skillName, scriptName string) (string, string, error) {
			return h.skillSvc.GetScript(h.ctx, skillName, scriptName)
		},
		workspace,
	).WithRootResolver(wsResolver).
		WithApprover(h.approvalSvc).
		WithPathDirs(rt.BinDirs)); err != nil {
		return err
	}
	if err := toolReg.Register(filetool.NewRead(wsResolver, workspace)); err != nil {
		return err
	}
	if err := toolReg.Register(filetool.NewWrite(wsResolver, workspace).
		WithRecorder(service.NewFileChangeRecorder(h.changeSvc, h.artifactSvc, "file_write"))); err != nil {
		return err
	}
	if err := toolReg.Register(filetool.NewList(wsResolver, workspace)); err != nil {
		return err
	}
	if err := toolReg.Register(webfetchtool.New()); err != nil {
		return err
	}
	if err := toolReg.Register(httptool.New()); err != nil {
		return err
	}
	if err := toolReg.Register(websearchtool.New()); err != nil {
		return err
	}
	// 纯函数工具（math/date/text/regex/json/csv/hash/encode/random）
	for _, ft := range functools.All() {
		if err := toolReg.Register(ft); err != nil {
			return err
		}
	}
	// 文档解析 + 归档工具（workspace 相对路径）
	if err := toolReg.Register(doctool.New(wsResolver, workspace)); err != nil {
		return err
	}
	if err := toolReg.Register(archivetool.New(wsResolver, workspace)); err != nil {
		return err
	}

	// 子 Agent 委派：上下文/预算隔离，只回传摘要
	if err := toolReg.Register(delegatetool.New()); err != nil {
		return err
	}

	// 补充输入：模型主动向用户要信息（暂停-回复-续跑）
	if err := toolReg.Register(requestinput.New()); err != nil {
		return err
	}

	// Todo 计划工具：会话内待办（长任务先列计划再逐步勾选）
	h.todoStore = service.NewSessionTodoStore()
	if err := toolReg.Register(todotool.New(h.todoStore, func(ctx context.Context) string {
		return harness.SessionIDFromCtx(ctx)
	})); err != nil {
		return err
	}

	// 知识库 RAG：FTS5 检索 + 索引；本地导入文件复制到 {home}/knowledge 受管目录。
	// knowledge_search 工具由知识库能力统一暴露（见下方能力注册表）
	knowledgeRepo := repo.NewKnowledgeDocRepo(gdb)
	retriever := rag.NewFTS5Retriever(gdb)
	h.knowledgeSvc = service.NewKnowledgeService(
		knowledgeRepo,
		rag.NewIndexer(gdb, rag.DefaultChunker()),
		retriever,
		filepath.Join(paths.Home, "knowledge"),
	)

	if err := toolReg.SelfCheckSchema(); err != nil {
		return err
	}
	h.toolSvc = service.NewToolService(toolReg, h.setRepo)
	h.dashSvc = service.NewDashboardService(repo.NewDashboardRepo(gdb), h.usageRepo, h.toolSvc)
	h.docsSvc = service.NewDocsService()

	// 记忆系统：短期(chat_messages 窗口) + 长期(MEMORY.md) + 情景(episodes + FTS5)
	memRepo := repo.NewMemoryEpisodeRepo(gdb)
	h.memSvc = memory.NewService(memRepo, repo.NewMemoryFactRepo(gdb), repo.NewMemoryProcedureRepo(gdb), paths.Home)
	// 长期记忆落点跟随会话工作区：绑定本地目录 → {dir}/.workbaby/memory/；默认 → {home}/memory/
	h.memSvc.WithMemoryPath(func(sessionID string) string {
		if h.chatSvc != nil {
			if mf, _ := h.chatSvc.SessionDataDirs(h.ctx, sessionID); mf != "" {
				return mf
			}
		}
		return filepath.Join(paths.Home, "memory", sessionID, "MEMORY.md")
	})
	h.memProxy = service.NewMemoryService(h.memSvc)

	// Skill 系统：内置 Skill upsert + Registry 装载（skills 表唯一真相源）
	skillRepo := repo.NewSkillRepo(gdb)
	h.skillSvc = service.NewSkillService(skillRepo, skill.NewRegistry())
	if err := h.skillSvc.SyncBuiltin(ctx); err != nil {
		return err
	}

	// MCP：外部工具源；启动失败只标记 unready，不阻断
	mcpRepo := repo.NewMcpServerRepo(gdb)
	h.mcpSvc = service.NewMcpService(mcpRepo, mcp.NewManager(toolReg).WithPathDirs(rt.BinDirs), h.cipher).
		WithConfigPath(service.McpRawPath(paths.Home))
	// mcp.json 文件为源：存在则同步进 mcp_servers 表后再对齐子进程
	if mcpPath := service.McpRawPath(paths.Home); isRegularFile(mcpPath) {
		if rows, merr := service.McpServersFromFile(mcpPath); merr != nil {
			pkg.L.Warn("sync mcp.json failed", "err", merr)
		} else if serr := h.mcpSvc.SyncMcpFromList(ctx, rows); serr != nil {
			pkg.L.Warn("sync mcp.json to db failed", "err", serr)
		}
	}
	if err := h.mcpSvc.Sync(ctx); err != nil {
		return err
	}

	h.chatSvc = service.NewChatService(h.sessRepo, h.msgRepo, h.provRepo, h.setRepo, h.usageRepo, h.bus, h.reg, h.toolSvc, h.memSvc).
		WithDataHome(paths.Home).
		WithCheckpointStore(service.NewSQLCheckpointStore(repo.NewAgentCheckpointRepo(gdb))).
		WithEventLog(h.eventLog).
		WithMessageBlocks(repo.NewMessageBlockRepo(gdb)).
		WithRunRecords(repo.NewRunRecordRepo(gdb)).
		WithExecutionRegistry(h.execs).
		WithApprovalService(h.approvalSvc)

	// 目录信任：恒信任根 = 全局工作区 + 会话工作区根 + 数据目录本身；
	// exec 的 cwd 不在根内时走 ask → 走审批门 → 批准后落盘 allow。
	// workspaces 作为整棵会话工作区树纳入恒信任：默认工作区是 App 自己的受管区，
	// 纳入询问只会让每次写文件都弹审批（噪声）。
	trustRoots := []string{
		filepath.Join(paths.Home, "workspace"),
		filepath.Join(paths.Home, "workspaces"),
		filepath.Join(paths.Home, "runtimes"),
		filepath.Join(paths.Home, "knowledge"),
		filepath.Join(paths.Home, "files"),
	}
	h.trustSvc = service.NewTrustService(repo.NewWorkspaceTrustRepo(gdb), trustRoots...).
		WithApprover(func(ctx context.Context, description string) bool {
			if h.approvalSvc == nil {
				return false
			}
			return h.approvalSvc.Approve(ctx, description, tool.RiskApprovalNeeds)
		})
	h.chatSvc.WithTrustService(h.trustSvc)

	// 后台任务队列：长任务不阻塞聊天，任务中心看进度
	h.taskSvc = service.NewTaskService(h.chatSvc, h.bus).WithEventLog(h.eventLog)
	h.taskSvc.Start()

	// 通知通道：email(SMTP)/webhook/console + 发送日志
	chanReg := channel.NewRegistry()
	_ = chanReg.Register(email.New(func(ctx context.Context) (email.SmtpConfig, error) {
		cfg, err := h.setSvc.GetSmtpConfig(ctx)
		if err != nil {
			return email.SmtpConfig{}, err
		}
		port, _ := strconv.Atoi(cfg.Port)
		if port == 0 {
			port = 587
		}
		return email.SmtpConfig{
			Enabled:  strings.EqualFold(cfg.Enabled, "true"),
			Host:     cfg.Host,
			Port:     port,
			Username: cfg.Username,
			Password: cfg.Password,
			From:     cfg.From,
		}, nil
	}))
	_ = chanReg.Register(webhook.New())
	_ = chanReg.Register(channel.ConsoleChannel{})
	h.channelSvc = channel.NewService(repo.NewChannelConfigRepo(gdb), repo.NewChannelMessageLogRepo(gdb), chanReg)

	// 工作流引擎：DAG 执行 + 7 节点 + HumanInput resolver。
	// ChannelSender 由 channel.Service 提供。
	wfRepo := repo.NewWorkflowRepo(gdb)
	wfExecRepo := repo.NewWorkflowExecutionRepo(gdb)
	wfNodeRepo := repo.NewWorkflowNodeExecutionRepo(gdb)
	wiResolver := workflow.NewDefaultHumanInputResolver(h.bus)
	wfExecutor := workflow.New(workflow.ExecutorConfig{
		DB:       gdb,
		WFRepo:   wfRepo,
		ExecRepo: wfExecRepo,
		NodeRepo: wfNodeRepo,
		Bus:      h.bus,
		Resolver: wiResolver,
		Sender:   h.channelSvc,
		Nodes: []wnodes.Node{
			// LLM 节点注入 ReAct 执行器：配了 tools 即走多轮工具循环（与聊天同一主循环）。
			// 计量：ReAct 与补全两条路径统一落 token_usages（Source=workflow）。
			wnodes.NewLLMNode(h.reg).
				WithReactor(service.NewWorkflowReactor(h.reg, h.toolSvc).
					WithUsageSink(service.WorkflowUsageSink(h.usageRepo)).React).
				WithUsageSink(service.WorkflowUsageSink(h.usageRepo)),
			wnodes.NewToolNode(toolReg),
			wnodes.NewCodeNode(),
			wnodes.NewConditionNode(),
			wnodes.NewHTTPNode(30 * time.Second),
			wnodes.NewHumanInputNode(wiResolver),
			wnodes.NewChannelNode(h.channelSvc),
		},
	})
	h.workflowSvc = service.NewWorkflowService(wfRepo, wfExecRepo, wfNodeRepo, wfExecutor, wiResolver)

	// 能力注册表：上下文装配（人格/工作区/记忆/知识库/Skill/工作流）、工具暴露与
	// run 后沉淀统一接入；新增能力实现 Capability 并在此注册一行，chat 侧不再改动
	caps := capability.NewRegistry()
	registerCap := func(c capability.Capability, order int) {
		if err := caps.Register(c, order); err != nil {
			pkg.L.Warn("register capability failed", "cap", c.ID(), "err", err.Error())
		}
	}
	registerCap(capability.NewPersona(), capability.OrderPersona)
	registerCap(capability.NewEnvironment(), capability.OrderEnvironment)
	registerCap(capability.NewWorkspace(), capability.OrderWorkspace)
	registerCap(capability.NewTodo(h.todoStore), capability.OrderTodo)
	registerCap(capability.NewMemory(h.memSvc, h.chatSvc.MemoryEnabled), capability.OrderMemory)
	registerCap(capability.NewKnowledge(retriever), capability.OrderKnowledge)
	registerCap(capability.NewSkill(capability.NewSkillSource(
		func(input string) string { return h.skillSvc.Match(input) },
		func(name string) (string, []string, bool) {
			sk, ok := h.skillSvc.Get(name)
			if !ok {
				return "", nil, false
			}
			return sk.Body, sk.Tools, true
		},
	)), capability.OrderSkill)
	registerCap(capability.NewWorkflow(h.workflowSvc), capability.OrderWorkflow)
	// 能力暴露的工具统一注册（knowledge_search / memory_write / run_workflow）
	for _, t := range caps.Tools() {
		if err := toolReg.Register(t); err != nil {
			return err
		}
	}
	if err := toolReg.SelfCheckSchema(); err != nil {
		return err
	}
	h.chatSvc.WithCapabilities(caps)

	// 定时任务：run_workflow 动作 → workflow service；启动失败不阻断
	cronSvc := cronjob.NewScheduler(repo.NewCronJobRepo(gdb))
	cronSvc.RegisterHandler(domain.ActionRunWorkflow, func(ctx context.Context, args json.RawMessage) error {
		var a domain.RunWorkflowArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return pkg.Wrap(9202, "parse cron action args failed", err)
		}
		if a.WorkflowID == "" {
			return pkg.New(9202, "workflowId required", "")
		}
		_, err := h.workflowSvc.Run(ctx, a.WorkflowID, nil)
		return err
	})
	h.cronSvc = cronSvc
	if err := cronSvc.Start(ctx); err != nil {
		pkg.L.Warn("cron start failed", "err", err)
	}

	// 文件系统：文件夹树 + 文件托管 + 会话工作区面板（面板与工具链共用会话目录解析）
	h.folderSvc = service.NewFolderService(repo.NewFolderRepo(gdb))
	h.fileSvc = service.NewFileService(repo.NewFileRepo(gdb), filepath.Join(paths.Home, "files"))
	h.workspaceSvc = service.NewWorkspaceService(filepath.Join(paths.Home, "workspaces"), func(sessionID string) string {
		return h.chatSvc.WorkspaceRoot(h.ctx, sessionID, filepath.Join(paths.Home, "workspaces", sessionID))
	})

	// 桌宠：配置单行 + sprite 资产 + 状态机（chat run 事件驱动）
	h.petSvc = pet.NewService(repo.NewPetConfigRepo(gdb), repo.NewPetSpriteRepo(gdb), filepath.Join(paths.Home, "sprites"))
	h.petSvc.SeedBuiltin(ctx)
	// 启动排空：上进程遗留的未决审批标 cancelled；崩溃时卡在 streaming 的消息标 interrupted（可经 Resume 续跑）
	h.approvalSvc.DrainStale(ctx)
	h.chatSvc.ReapInterrupted(ctx)
	h.petCtrl = pet.NewController(func(s pet.State) {
		if h.ctx != nil {
			wruntime.EventsEmit(h.ctx, "pet:state", map[string]any{"state": string(s)})
		}
	})
	h.bus.Subscribe(event.MatchPrefix("chat:"), func(e string, _ any) {
		if h.petCtrl == nil {
			return
		}
		switch e {
		case "chat:stream.start":
			h.petCtrl.OnEvent("agent.run.start")
		case "chat:done":
			h.petCtrl.OnEvent("agent.run.done")
		case "chat:error":
			h.petCtrl.OnEvent("agent.error")
		}
	})

	h.ctx = ctx
	h.Files = &FileServer{
		ctx:          ctx,
		fileSvc:      h.fileSvc,
		workspaceSvc: h.workspaceSvc,
		petSvc:       h.petSvc,
	}
	h.bindEventBridge()
	// app:ready 由 main.go 在 gin server 启动后发射
	sl, lerr := h.skillSvc.List(ctx)
	ml, merr := h.mcpSvc.List(ctx)
	fields := []any{"home", paths.Home, "cipherReady", true, "tools", len(toolReg.List())}
	if lerr == nil {
		fields = append(fields, "skills", len(sl))
	}
	ready := 0
	if merr == nil {
		for i := range ml {
			if ml[i].Ready {
				ready++
			}
		}
		fields = append(fields, "mcpServers", len(ml), "mcpReady", ready)
	}
	pkg.L.Info("workbaby started", fields...)
	return nil
}

// Shutdown 预留。
func (h *Handler) Shutdown(_ context.Context) {
	if h.cronSvc != nil {
		h.cronSvc.Stop() // 停止定时调度
	}
	if h.taskSvc != nil {
		h.taskSvc.Stop() // 停止后台任务 worker（在跑任务靠自身 ctx 结束）
	}
	if h.mcpSvc != nil {
		h.mcpSvc.Close() // 回收 MCP 子进程，避免残留孤儿进程
	}
	if pkg.L != nil {
		pkg.L.Info("workbaby shutting down")
	}
}

// bindEventBridge 应用内事件总线 → Wails 前端事件总线（仅系统级事件）。
//
// chat:* 不经此桥接：前端消费统一走 SSE（server/sse.go），Wails 通道零订阅者，
// 而且每条 chat:stream 增量都会在 run goroutine 上同步执行一次 EventsEmit（序列化 + IPC），
// 纯开销。保留 app:*（app:ready / app:open-file 是前端启动依赖）。
func (h *Handler) bindEventBridge() {
	h.bus.Subscribe(event.MatchPrefix("app:"), func(event string, payload any) {
		if h.ctx == nil {
			return
		}
		wruntime.EventsEmit(h.ctx, event, payload)
	})
}
