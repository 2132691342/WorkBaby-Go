package server

import (
	"strconv"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"github.com/gin-gonic/gin"
)

// registerRoutes 注册全部业务路由。路径/参数风格见 CLAUDE.md §2.12：
// 仅 GET/POST；路径变量在末尾；body 与 query 字段 snake_case。
// api.Handler 方法签名 (T, error)：先取 v,err 再 unwrap（Go 多值展开不能与其他参数混用）。
func (s *Server) registerRoutes() {
	h := s.handler
	v1 := s.engine.Group("/api/v1")

	// ---- meta ----
	v1.GET("/meta/version", func(c *gin.Context) { OK(c, h.GetVersion()) })
	v1.GET("/meta/health", func(c *gin.Context) { OK(c, h.GetHealth()) })
	v1.GET("/meta/contract", func(c *gin.Context) { OK(c, h.GetContractVersion()) })
	v1.GET("/meta/runtime", func(c *gin.Context) { OK(c, h.GetRuntimeStatus()) })

	// ---- admin ----
	v1.GET("/admin/overview", func(c *gin.Context) {
		v, err := h.GetAdminOverview()
		unwrap(c, v, err)
	})

	// ---- docs (built-in) ----
	v1.GET("/docs", func(c *gin.Context) {
		v, err := h.ListDocs()
		unwrap(c, v, err)
	})
	v1.GET("/docs/:name", func(c *gin.Context) {
		v, err := h.GetDoc(c.Param("name"))
		unwrap(c, v, err)
	})

	// ---- dashboard ----
	v1.GET("/dashboard/stats", func(c *gin.Context) {
		v, err := h.GetDashboardStats()
		unwrap(c, v, err)
	})
	v1.GET("/dashboard/trend", func(c *gin.Context) {
		days := atoi(c.DefaultQuery("range", "7"), 7)
		v, err := h.GetDashboardTrend(days)
		unwrap(c, v, err)
	})
	v1.GET("/dashboard/token-trend", func(c *gin.Context) {
		req := domain.TokenTrendREQ{
			Scope:   domain.TokenTrendScope(c.DefaultQuery("scope", string(domain.TrendScopeToday))),
			StartAt: atoi64(c.Query("start_at"), 0),
			EndAt:   atoi64(c.Query("end_at"), 0),
		}
		v, err := h.GetTokenTrend(req)
		unwrap(c, v, err)
	})

	// ---- events (SSE) ----
	v1.GET("/events", s.hub.Serve)

	// ---- files (本地受管文件：media/sprites/files/workspace) ----
	// 与业务 API 同 origin，前端 <img>/fetch 在 dev 与生产均可直接加载；
	// 生产环境 Wails AssetServer 同样把 /files/** 转发到同一 FileServer，双通道行为一致。
	s.engine.GET("/files/*filepath", func(c *gin.Context) {
		if h.Files == nil {
			Fail(c, pkg.New(1404, "file service not ready", ""))
			return
		}
		h.Files.ServeHTTP(c.Writer, c.Request)
	})

	// ---- chat / session ----
	v1.GET("/chat/sessions", func(c *gin.Context) {
		page := atoi(c.DefaultQuery("page", "1"), 1)
		pageSize := atoi(c.DefaultQuery("page_size", "20"), 20)
		v, err := h.ListSessions(page, pageSize)
		unwrap(c, v, err)
	})
	v1.POST("/chat/sessions", func(c *gin.Context) {
		var req domain.ChatSessionREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.CreateSession(req)
		unwrap(c, v, err)
	})
	v1.GET("/chat/sessions/:id", func(c *gin.Context) {
		v, err := h.GetSession(c.Param("id"))
		unwrap(c, v, err)
	})
	v1.POST("/chat/sessions/:id/rename", func(c *gin.Context) {
		var req domain.ChatSessionRenameREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.RenameSession(c.Param("id"), req.Name)
		unwrap(c, v, err)
	})
	v1.GET("/chat/sessions/:id/params", func(c *gin.Context) {
		v, err := h.EffectiveParams(c.Param("id"))
		unwrap(c, v, err)
	})
	v1.POST("/chat/sessions/:id/permission", func(c *gin.Context) {
		var req domain.ChatSessionPermissionREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.SetSessionPermission(c.Param("id"), req)
		unwrap(c, v, err)
	})
	v1.POST("/chat/sessions/:id/model", func(c *gin.Context) {
		var req domain.ChatSessionModelREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.SetSessionModel(c.Param("id"), req)
		unwrap(c, v, err)
	})
	v1.POST("/chat/sessions/:id/delete", func(c *gin.Context) { Fail(c, h.DeleteSession(c.Param("id"))) })
	v1.POST("/chat/sessions/delete-batch", func(c *gin.Context) {
		var ids []string
		if err := BindJSON(c, &ids); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.DeleteSessions(ids)
		unwrap(c, v, err)
	})
	v1.POST("/chat/sessions/:id/workspace", func(c *gin.Context) {
		var req struct {
			WorkspacePath string `json:"workspace_path"`
			WorkspaceID   string `json:"workspace_id"` // 旧前端字段，仅作兜底
		}
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		p := req.WorkspacePath
		if p == "" {
			p = req.WorkspaceID
		}
		v, err := h.UpdateSessionWorkspace(c.Param("id"), p)
		unwrap(c, v, err)
	})
	v1.GET("/chat/sessions/:id/messages", func(c *gin.Context) {
		afterSeq := atoi64(c.DefaultQuery("after_seq", "0"), 0)
		limit := atoi(c.DefaultQuery("limit", "200"), 200)
		v, err := h.ListMessages(c.Param("id"), afterSeq, limit)
		unwrap(c, v, err)
	})
	v1.POST("/chat/stream", func(c *gin.Context) {
		var req domain.SendStreamREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.SendStream(req)
		unwrap(c, v, err)
	})
	v1.POST("/chat/stream/:id/cancel", func(c *gin.Context) { Fail(c, h.CancelStream(c.Param("id"))) })
	// 检查点续跑：中断/崩溃后同一 runID 恢复
	v1.POST("/chat/stream/:id/resume", func(c *gin.Context) {
		v, err := h.ResumeChat(c.Param("id"))
		unwrap(c, v, err)
	})
	// run 事件 JSONL 无头导出
	v1.GET("/chat/runs/:id/events", func(c *gin.Context) {
		v, err := h.ExportRunEvents(c.Param("id"))
		unwrap(c, v, err)
	})
	v1.POST("/chat/sessions/:id/steer", func(c *gin.Context) {
		var req domain.SteerREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.SteerSession(c.Param("id"), req)
		unwrap(c, v, err)
	})
	v1.GET("/chat/sessions/:id/todos", func(c *gin.Context) {
		OK(c, h.GetSessionTodos(c.Param("id")))
	})
	v1.POST("/chat/sessions/:id/clear", func(c *gin.Context) { Fail(c, h.ClearMessages(c.Param("id"))) })
	v1.POST("/chat/messages/:sid/delete/:id", func(c *gin.Context) {
		Fail(c, h.DeleteMessage(c.Param("sid"), c.Param("id")))
	})
	v1.POST("/chat/messages/:sid/truncate", func(c *gin.Context) {
		var req domain.TruncateMessagesREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		Fail(c, h.TruncateMessages(c.Param("sid"), req))
	})
	v1.POST("/chat/messages/:sid/fork", func(c *gin.Context) {
		var req domain.ForkSessionREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.ForkSession(c.Param("sid"), req)
		unwrap(c, v, err)
	})
	v1.POST("/chat/approval/:id/decide", func(c *gin.Context) {
		var req domain.DecideApprovalREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		Fail(c, h.DecideApproval(c.Param("id"), req))
	})
	v1.POST("/chat/approval/:id/answer", func(c *gin.Context) {
		var req domain.AnswerInputREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		Fail(c, h.AnswerInput(c.Param("id"), req))
	})
	v1.GET("/chat/approvals/pending", func(c *gin.Context) {
		OK(c, h.ListPendingApprovals())
	})
	// 斜杠命令：元数据（命令面板）+ 需后端能力的动作
	v1.GET("/chat/commands", func(c *gin.Context) { OK(c, h.ListChatCommands()) })
	v1.POST("/chat/sessions/:id/compact", func(c *gin.Context) {
		var req domain.CompactREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.CompactSession(c.Param("id"), req)
		unwrap(c, v, err)
	})
	// 跨会话快速检索（/resume）：title / content / all 三档
	v1.GET("/chat/sessions/search", func(c *gin.Context) {
		req := domain.SessionSearchREQ{
			Query: c.Query("q"),
			Scope: c.Query("scope"),
			Limit: atoi(c.DefaultQuery("limit", "20"), 20),
		}
		v, err := h.SearchSessions(req)
		unwrap(c, v, err)
	})
	// 上下文占用分段快照（/context 侧栏环）
	v1.GET("/chat/sessions/:id/usage/context", func(c *gin.Context) {
		v, err := h.ContextUsage(c.Param("id"))
		unwrap(c, v, err)
	})

	// 工作目录信任（工作目录信任三态）
	v1.GET("/trust", func(c *gin.Context) {
		v, err := h.ListTrust()
		unwrap(c, v, err)
	})
	v1.GET("/trust/resolve", func(c *gin.Context) {
		v, err := h.ResolveTrust(c.Query("path"))
		unwrap(c, v, err)
	})
	v1.POST("/trust", func(c *gin.Context) {
		var req domain.WorkspaceTrustREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.DecideTrust(req)
		unwrap(c, v, err)
	})
	v1.POST("/trust/revoke", func(c *gin.Context) {
		var req struct {
			Path string `json:"path"`
		}
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		Fail(c, h.RevokeTrust(req.Path))
	})
	v1.GET("/trust/roots", func(c *gin.Context) {
		OK(c, h.TrustRoots())
	})

	// ---- workspace files ----
	v1.GET("/chat/workspace/:id/files", func(c *gin.Context) {
		v, err := h.ListWorkspaceFiles(c.Param("id"))
		unwrap(c, v, err)
	})
	v1.GET("/chat/workspace/:id/file", func(c *gin.Context) {
		v, err := h.ReadWorkspaceFile(c.Param("id"), c.Query("path"))
		unwrap(c, v, err)
	})

	// ---- file changes ----
	v1.GET("/chat/sessions/:id/changes", func(c *gin.Context) {
		limit := atoi(c.DefaultQuery("limit", "100"), 100)
		v, err := h.ListSessionChanges(c.Param("id"), limit)
		unwrap(c, v, err)
	})
	v1.GET("/chat/changes/:cid", func(c *gin.Context) {
		v, err := h.GetSessionChange(c.Param("cid"))
		unwrap(c, v, err)
	})
	v1.POST("/chat/changes/:cid/rollback", func(c *gin.Context) {
		Fail(c, h.RollbackSessionChange(c.Param("cid")))
	})

	// ---- artifacts ----
	v1.GET("/chat/sessions/:id/artifacts", func(c *gin.Context) {
		limit := atoi(c.DefaultQuery("limit", "100"), 100)
		v, err := h.ListSessionArtifacts(c.Param("id"), limit)
		unwrap(c, v, err)
	})
	v1.POST("/chat/artifacts/:aid/delete", func(c *gin.Context) {
		Fail(c, h.DeleteSessionArtifact(c.Param("aid")))
	})

	// ---- background tasks ----
	v1.POST("/tasks", func(c *gin.Context) {
		var req domain.TaskSubmitREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.SubmitTask(req)
		unwrap(c, v, err)
	})
	v1.GET("/tasks", func(c *gin.Context) {
		limit := atoi(c.DefaultQuery("limit", "50"), 50)
		OK(c, h.ListTasks(c.Query("session_id"), limit))
	})
	v1.GET("/tasks/:id", func(c *gin.Context) {
		v, err := h.GetTask(c.Param("id"))
		unwrap(c, v, err)
	})
	v1.POST("/tasks/:id/cancel", func(c *gin.Context) { Fail(c, h.CancelTask(c.Param("id"))) })

	// ---- ai-provider ----
	v1.GET("/ai-provider", func(c *gin.Context) {
		v, err := h.ListProviders()
		unwrap(c, v, err)
	})
	v1.POST("/ai-provider", func(c *gin.Context) {
		var req domain.AiProviderREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.CreateProvider(req)
		unwrap(c, v, err)
	})
	v1.GET("/ai-provider/available", func(c *gin.Context) {
		v, err := h.ListAvailableModels()
		unwrap(c, v, err)
	})
	v1.GET("/ai-provider/kinds", func(c *gin.Context) {
		v, err := h.ListProviderKinds()
		unwrap(c, v, err)
	})
	v1.GET("/ai-provider/:id", func(c *gin.Context) {
		v, err := h.GetProvider(c.Param("id"))
		unwrap(c, v, err)
	})
	v1.POST("/ai-provider/:id/update", func(c *gin.Context) {
		var req domain.AiProviderREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.UpdateProvider(c.Param("id"), req)
		unwrap(c, v, err)
	})
	v1.POST("/ai-provider/:id/delete", func(c *gin.Context) { Fail(c, h.DeleteProvider(c.Param("id"))) })
	v1.POST("/ai-provider/:id/test", func(c *gin.Context) { Fail(c, h.TestProviderConnect(c.Param("id"))) })
	v1.POST("/ai-provider/reload", func(c *gin.Context) {
		// model.json 热重载：重新读文件 → 同步 DB → 重建 Registry
		v, err := h.ReloadProvidersFromFile()
		unwrap(c, v, err)
	})
	v1.GET("/ai-provider/circuit-status", func(c *gin.Context) {
		v, err := h.GetCircuitStatus()
		unwrap(c, v, err)
	})
	v1.POST("/ai-provider/:id/reset-circuit", func(c *gin.Context) {
		v, err := h.ResetCircuit(c.Param("id"))
		unwrap(c, v, err)
	})

	// ---- settings ----
	v1.GET("/settings", func(c *gin.Context) {
		v, err := h.ListSettings()
		unwrap(c, v, err)
	})
	v1.GET("/settings/general", func(c *gin.Context) {
		v, err := h.GetGeneralSettings()
		unwrap(c, v, err)
	})
	v1.POST("/settings/general", func(c *gin.Context) {
		var req map[string]any
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.SaveGeneralSettings(req)
		unwrap(c, v, err)
	})
	v1.GET("/settings/smtp", func(c *gin.Context) {
		v, err := h.GetSmtpConfig()
		unwrap(c, v, err)
	})
	v1.POST("/settings/smtp", func(c *gin.Context) {
		var req domain.SmtpConfigRESP
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.SaveSmtpConfig(req)
		unwrap(c, v, err)
	})
	v1.GET("/settings/websearch", func(c *gin.Context) {
		v, err := h.GetWebSearchConfig()
		unwrap(c, v, err)
	})
	v1.POST("/settings/websearch", func(c *gin.Context) {
		var req domain.WebSearchConfigRESP
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.SaveWebSearchConfig(req)
		unwrap(c, v, err)
	})
	v1.GET("/settings/exec/agent", func(c *gin.Context) {
		v, err := h.GetExecWhitelist()
		unwrap(c, v, err)
	})
	v1.POST("/settings/exec/agent", func(c *gin.Context) {
		var req []string
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.SaveExecWhitelist(req)
		unwrap(c, v, err)
	})
	v1.GET("/kv/:key", func(c *gin.Context) {
		v, err := h.GetSetting(c.Param("key"))
		unwrap(c, v, err)
	})
	v1.POST("/kv/:key", func(c *gin.Context) {
		var req struct {
			Value string `json:"value"`
		}
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.SetSetting(c.Param("key"), req.Value)
		unwrap(c, v, err)
	})

	// ---- skills ----
	v1.GET("/skills", func(c *gin.Context) {
		v, err := h.ListSkills()
		unwrap(c, v, err)
	})
	v1.POST("/skills", func(c *gin.Context) {
		var req domain.SkillREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.ImportSkill(req)
		unwrap(c, v, err)
	})
	// 技能包 zip 批量导入（zip_path 由前端原生对话框选取）
	v1.POST("/skills/import-zip", func(c *gin.Context) {
		var req struct {
			ZipPath string `json:"zip_path"`
		}
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.ImportSkillsZip(req.ZipPath)
		unwrap(c, v, err)
	})
	v1.POST("/skills/:name/enabled", func(c *gin.Context) {
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		Fail(c, h.SetSkillEnabled(c.Param("name"), req.Enabled))
	})
	v1.POST("/skills/:name/update", func(c *gin.Context) {
		var req domain.SkillREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.UpdateSkill(c.Param("name"), req)
		unwrap(c, v, err)
	})
	v1.POST("/skills/:name/delete", func(c *gin.Context) { Fail(c, h.DeleteSkill(c.Param("name"))) })

	// ---- mcp ----
	v1.GET("/mcp/servers", func(c *gin.Context) {
		v, err := h.ListMcpServers()
		unwrap(c, v, err)
	})
	v1.GET("/mcp/servers/raw", func(c *gin.Context) {
		v, err := h.GetMcpRaw()
		unwrap(c, v, err)
	})
	v1.POST("/mcp/servers/raw", func(c *gin.Context) {
		var req domain.McpRawREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.SaveMcpRaw(req)
		unwrap(c, v, err)
	})
	v1.POST("/mcp/servers", func(c *gin.Context) {
		var req domain.McpServerREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.AddMcpServer(req)
		unwrap(c, v, err)
	})
	v1.POST("/mcp/servers/:name/enabled", func(c *gin.Context) {
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		Fail(c, h.SetMcpServerEnabled(c.Param("name"), req.Enabled))
	})
	v1.POST("/mcp/servers/:name/delete", func(c *gin.Context) { Fail(c, h.RemoveMcpServer(c.Param("name"))) })
	v1.POST("/mcp/servers/reload", func(c *gin.Context) {
		v, err := h.ReloadMcpServers()
		unwrap(c, v, err)
	})
	v1.POST("/mcp/servers/reveal", func(c *gin.Context) { Fail(c, h.RevealMcpFile()) })

	// ---- tools ----
	v1.GET("/tools", func(c *gin.Context) {
		v, err := h.ListTools()
		unwrap(c, v, err)
	})
	v1.POST("/tools/:name/enabled", func(c *gin.Context) {
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		Fail(c, h.SetToolEnabled(c.Param("name"), req.Enabled))
	})

	// ---- workflows ----
	// node-types 是静态段；gin/httprouter 静态路由优先于 /workflows/:id，可放心放此处。
	v1.GET("/workflows/node-types", func(c *gin.Context) {
		unwrap(c, h.ListWorkflowNodeTypes(), nil)
	})
	v1.GET("/workflows", func(c *gin.Context) {
		v, err := h.ListWorkflows()
		unwrap(c, v, err)
	})
	v1.POST("/workflows", func(c *gin.Context) {
		var req domain.WorkflowGraphREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.SaveWorkflow("", req)
		unwrap(c, v, err)
	})
	v1.GET("/workflows/:id", func(c *gin.Context) {
		v, err := h.GetWorkflow(c.Param("id"))
		unwrap(c, v, err)
	})
	v1.GET("/workflows/:id/graph", func(c *gin.Context) {
		v, err := h.GetWorkflowGraph(c.Param("id"))
		unwrap(c, v, err)
	})
	v1.POST("/workflows/:id/update-graph", func(c *gin.Context) {
		var req domain.WorkflowDAGREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.UpdateWorkflowGraph(c.Param("id"), req)
		unwrap(c, v, err)
	})
	v1.POST("/workflows/:id/update", func(c *gin.Context) {
		var req domain.WorkflowGraphREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.SaveWorkflow(c.Param("id"), req)
		unwrap(c, v, err)
	})
	v1.POST("/workflows/:id/delete", func(c *gin.Context) { Fail(c, h.DeleteWorkflow(c.Param("id"))) })
	v1.POST("/workflows/validate", func(c *gin.Context) {
		var req struct {
			Graph string `json:"graph"`
		}
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		Fail(c, h.ValidateWorkflowGraph(req.Graph))
	})
	v1.POST("/workflows/:id/run", func(c *gin.Context) {
		var req domain.WorkflowRunREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.RunWorkflow(c.Param("id"), req)
		unwrap(c, v, err)
	})
	v1.GET("/workflows/:id/executions", func(c *gin.Context) {
		limit := atoi(c.DefaultQuery("limit", "20"), 20)
		v, err := h.ListWorkflowExecutions(c.Param("id"), limit)
		unwrap(c, v, err)
	})
	v1.GET("/executions/:id", func(c *gin.Context) {
		v, err := h.GetWorkflowExecution(c.Param("id"))
		unwrap(c, v, err)
	})
	v1.POST("/executions/:id/cancel", func(c *gin.Context) { Fail(c, h.CancelWorkflowExecution(c.Param("id"))) })
	v1.POST("/executions/:id/pause", func(c *gin.Context) { Fail(c, h.PauseWorkflowExecution(c.Param("id"))) })
	v1.POST("/executions/:id/resume", func(c *gin.Context) { Fail(c, h.ResumeWorkflowExecution(c.Param("id"))) })
	v1.POST("/executions/:id/input", func(c *gin.Context) {
		var req domain.ResolveHumanInputREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		Fail(c, h.ResolveHumanInput(c.Param("id"), req))
	})

	// ---- knowledge (kdocs) ----
	v1.GET("/kdocs", func(c *gin.Context) {
		v, err := h.ListKnowledgeDocs()
		unwrap(c, v, err)
	})
	v1.POST("/kdocs", func(c *gin.Context) {
		var req domain.KnowledgeDocREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.AddKnowledgeDoc(req)
		unwrap(c, v, err)
	})
	// 本地文件导入受管知识库：前端原生对话框选路径 → 后端复制到 {home}/knowledge 并后台索引
	v1.POST("/kdocs/import-file", func(c *gin.Context) {
		var req struct {
			Name       string  `json:"name"`
			SourcePath string  `json:"source_path"`
			FolderID   *string `json:"folder_id"`
		}
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		folderID := ""
		if req.FolderID != nil {
			folderID = *req.FolderID
		}
		v, err := h.ImportKnowledgeFile(req.Name, req.SourcePath, folderID)
		unwrap(c, v, err)
	})
	v1.GET("/kdocs/search", func(c *gin.Context) {
		topK := atoi(c.DefaultQuery("limit", "20"), 20)
		v, err := h.SearchKnowledge(c.Query("q"), topK)
		unwrap(c, v, err)
	})
	v1.GET("/kdocs/groups", func(c *gin.Context) {
		v, err := h.ListKnowledgeGroups()
		unwrap(c, v, err)
	})
	v1.GET("/kdocs/group/:group", func(c *gin.Context) {
		v, err := h.ListKnowledgeByGroup(c.Param("group"))
		unwrap(c, v, err)
	})
	v1.POST("/kdocs/:id/update", func(c *gin.Context) {
		var req domain.KnowledgeDocREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.UpdateKnowledgeDoc(c.Param("id"), req)
		unwrap(c, v, err)
	})
	v1.GET("/kdocs/:id", func(c *gin.Context) {
		v, err := h.GetKnowledgeDoc(c.Param("id"))
		unwrap(c, v, err)
	})
	v1.POST("/kdocs/:id/delete", func(c *gin.Context) { Fail(c, h.DeleteKnowledgeDoc(c.Param("id"))) })
	v1.POST("/kdocs/:id/reindex", func(c *gin.Context) { Fail(c, h.ReindexKnowledgeDoc(c.Param("id"))) })

	// ---- memory ----
	v1.GET("/memory/search", func(c *gin.Context) {
		topK := atoi(c.DefaultQuery("k", "20"), 20)
		v, err := h.SearchMemory(c.Query("q"), topK)
		unwrap(c, v, err)
	})
	v1.GET("/memory/episodes", func(c *gin.Context) {
		limit := atoi(c.DefaultQuery("limit", "50"), 50)
		v, err := h.ListMemoryEpisodes(c.Query("sessionID"), limit)
		unwrap(c, v, err)
	})
	v1.POST("/memory/episodes", func(c *gin.Context) {
		var req domain.MemoryEpisodeREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.WriteMemoryEpisode(req)
		unwrap(c, v, err)
	})
	v1.POST("/memory/episodes/:id/delete", func(c *gin.Context) { Fail(c, h.DeleteMemoryEpisode(c.Param("id"))) })
	v1.GET("/memory/stats", func(c *gin.Context) {
		v, err := h.GetMemoryStats()
		unwrap(c, v, err)
	})
	v1.GET("/memory/recall", func(c *gin.Context) {
		topK := atoi(c.DefaultQuery("k", "10"), 10)
		v, err := h.RecallMemory(c.Query("q"), topK)
		unwrap(c, v, err)
	})
	v1.GET("/memory/long-term/:id", func(c *gin.Context) {
		v, err := h.GetLongTermMemory(c.Param("id"))
		unwrap(c, v, err)
	})
	v1.GET("/memory/facts", func(c *gin.Context) {
		limit := atoi(c.DefaultQuery("limit", "50"), 50)
		v, err := h.ListMemoryFacts(limit)
		unwrap(c, v, err)
	})
	v1.POST("/memory/facts", func(c *gin.Context) {
		var req domain.MemoryFactREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.WriteMemoryFact(req)
		unwrap(c, v, err)
	})
	v1.POST("/memory/facts/:id/delete", func(c *gin.Context) { Fail(c, h.DeleteMemoryFact(c.Param("id"))) })
	v1.GET("/memory/procedures", func(c *gin.Context) {
		limit := atoi(c.DefaultQuery("limit", "50"), 50)
		v, err := h.ListMemoryProcedures(limit)
		unwrap(c, v, err)
	})
	v1.POST("/memory/procedures", func(c *gin.Context) {
		var req domain.MemoryProcedureREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.WriteMemoryProcedure(req)
		unwrap(c, v, err)
	})
	v1.POST("/memory/procedures/:id/delete", func(c *gin.Context) { Fail(c, h.DeleteMemoryProcedure(c.Param("id"))) })

	// ---- channels ----
	v1.GET("/channels", func(c *gin.Context) {
		v, err := h.ListChannels()
		unwrap(c, v, err)
	})
	v1.POST("/channels", func(c *gin.Context) {
		var req domain.ChannelConfigREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.CreateChannel(req)
		unwrap(c, v, err)
	})
	v1.POST("/channels/:id/update", func(c *gin.Context) {
		var req domain.ChannelConfigREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.UpdateChannel(c.Param("id"), req)
		unwrap(c, v, err)
	})
	v1.POST("/channels/:id/delete", func(c *gin.Context) {
		v, err := h.DeleteChannel(c.Param("id"))
		unwrap(c, v, err)
	})
	v1.POST("/channels/:id/start", func(c *gin.Context) {
		v, err := h.StartChannel(c.Param("id"))
		unwrap(c, v, err)
	})
	v1.POST("/channels/:id/stop", func(c *gin.Context) {
		v, err := h.StopChannel(c.Param("id"))
		unwrap(c, v, err)
	})
	v1.POST("/channels/:id/test", func(c *gin.Context) {
		v, err := h.TestChannel(c.Param("id"))
		unwrap(c, v, err)
	})
	v1.GET("/channels/:id/messages", func(c *gin.Context) {
		limit := atoi(c.DefaultQuery("limit", "50"), 50)
		v, err := h.ListChannelMessages(c.Param("id"), limit)
		unwrap(c, v, err)
	})

	// ---- cron ----
	v1.GET("/cron/jobs", func(c *gin.Context) {
		v, err := h.ListCronJobs()
		unwrap(c, v, err)
	})
	v1.POST("/cron/jobs", func(c *gin.Context) {
		var req domain.CronJobREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.CreateCronJob(req)
		unwrap(c, v, err)
	})
	v1.POST("/cron/jobs/:id/update", func(c *gin.Context) {
		var req domain.CronJobREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.UpdateCronJob(c.Param("id"), req)
		unwrap(c, v, err)
	})
	v1.POST("/cron/jobs/:id/delete", func(c *gin.Context) {
		v, err := h.DeleteCronJob(c.Param("id"))
		unwrap(c, v, err)
	})
	v1.POST("/cron/jobs/:id/trigger", func(c *gin.Context) {
		v, err := h.TriggerCronJob(c.Param("id"))
		unwrap(c, v, err)
	})

	// ---- pet ----
	v1.GET("/pet/state", func(c *gin.Context) {
		v, err := h.GetPetState()
		unwrap(c, v, err)
	})
	v1.GET("/pet/config", func(c *gin.Context) {
		v, err := h.GetPetConfig()
		unwrap(c, v, err)
	})
	v1.POST("/pet/config/update", func(c *gin.Context) {
		var req domain.PetConfigREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.UpdatePetConfig(req)
		unwrap(c, v, err)
	})
	v1.GET("/pet/sprites", func(c *gin.Context) {
		v, err := h.ListPetSprites()
		unwrap(c, v, err)
	})
	v1.POST("/pet/sprites", func(c *gin.Context) {
		var req domain.PetSpriteREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.CreatePetSprite(req)
		unwrap(c, v, err)
	})
	v1.POST("/pet/sprites/:id/delete", func(c *gin.Context) {
		v, err := h.DeletePetSprite(c.Param("id"))
		unwrap(c, v, err)
	})
	v1.POST("/pet/window/:mode", func(c *gin.Context) {
		mode := c.Param("mode")
		if mode == "pet" && !h.PetMode() {
			v, err := h.PetToggleMode()
			unwrap(c, v, err)
			return
		}
		if mode == "main" && h.PetMode() {
			v, err := h.PetToggleMode()
			unwrap(c, v, err)
			return
		}
		OK(c, map[string]any{"mode": petMode(h.PetMode())})
	})

	// ---- folders / files ----
	v1.GET("/folders/tree", func(c *gin.Context) {
		v, err := h.GetFolderTree(c.Query("workspaceID"))
		unwrap(c, v, err)
	})
	v1.GET("/folders", func(c *gin.Context) {
		v, err := h.ListFolders(c.Query("parentID"))
		unwrap(c, v, err)
	})
	v1.POST("/folders", func(c *gin.Context) {
		var req domain.FolderREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.CreateFolder(req)
		unwrap(c, v, err)
	})
	v1.POST("/folders/:id/update", func(c *gin.Context) {
		var req domain.FolderREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.UpdateFolder(c.Param("id"), req)
		unwrap(c, v, err)
	})
	v1.POST("/folders/:id/delete", func(c *gin.Context) {
		v, err := h.DeleteFolder(c.Param("id"))
		unwrap(c, v, err)
	})

	v1.GET("/files/search", func(c *gin.Context) {
		v, err := h.SearchFiles(c.Query("q"))
		unwrap(c, v, err)
	})
	v1.GET("/files", func(c *gin.Context) {
		v, err := h.ListFiles()
		unwrap(c, v, err)
	})
	v1.POST("/files/upload", func(c *gin.Context) {
		var req struct {
			Name       string `json:"name"`
			SourcePath string `json:"source_path"`
			SessionID  string `json:"session_id"`
			FolderID   string `json:"folder_id"`
		}
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.UploadFile(req.Name, req.SourcePath, req.SessionID, req.FolderID)
		unwrap(c, v, err)
	})
	v1.POST("/files/:id/delete", func(c *gin.Context) {
		v, err := h.DeleteFile(c.Param("id"))
		unwrap(c, v, err)
	})
	v1.GET("/files/:id/preview-url", func(c *gin.Context) {
		v, err := h.GetFilePreviewURL(c.Param("id"))
		unwrap(c, v, err)
	})
}

// ---- 小工具 ----

func atoi(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func atoi64(s string, def int64) int64 {
	if s == "" {
		return def
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func petMode(pet bool) string {
	if pet {
		return "pet"
	}
	return "main"
}
