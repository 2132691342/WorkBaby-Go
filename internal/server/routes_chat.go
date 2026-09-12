package server

// 会话与消息路由：会话 CRUD / 流式运行 / 审批 / 待办 / 会话变量 / 斜杠命令 / 后台任务。

import (
	"WorkBaby/internal/api"
	"github.com/gin-gonic/gin"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
)

func registerChatRoutes(v1 *gin.RouterGroup, h *api.Handler) {
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
	v1.POST("/chat/sessions/:id/agent", func(c *gin.Context) {
		var req domain.SetSessionAgentREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		Fail(c, h.SetSessionAgent(c.Param("id"), req))
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
	// 运行历史索引（事件明细回放走 /chat/runs/:id/events）
	v1.GET("/chat/runs", func(c *gin.Context) {
		var req domain.RunRecordListREQ
		if err := c.ShouldBindQuery(&req); err != nil {
			Fail(c, pkg.Wrap(1001, "invalid query", err))
			return
		}
		v, err := h.ListRuns(req)
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
	// 计划项勾选：变量参数在路径末尾（CLAUDE.md §2.12）
	v1.POST("/chat/sessions/:id/todos/:itemID/toggle", func(c *gin.Context) {
		v, err := h.ToggleSessionTodo(c.Param("id"), c.Param("itemID"))
		unwrap(c, v, err)
	})
	// 会话变量：跨轮次结构化状态（GET 清单 / POST 写入覆盖 / POST 删除）
	v1.GET("/chat/sessions/:id/vars", func(c *gin.Context) {
		v, err := h.SessionVars(c.Param("id"))
		unwrap(c, v, err)
	})
	v1.POST("/chat/sessions/:id/vars", func(c *gin.Context) {
		var req domain.SessionVarREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.SetSessionVar(c.Param("id"), req)
		unwrap(c, v, err)
	})
	v1.POST("/chat/sessions/:id/vars/delete", func(c *gin.Context) {
		var req domain.SessionVarREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.DeleteSessionVar(c.Param("id"), req)
		unwrap(c, v, err)
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
	v1.POST("/chat/approval/:id/skip", func(c *gin.Context) {
		Fail(c, h.SkipApproval(c.Param("id")))
	})
	v1.GET("/chat/approvals/pending", func(c *gin.Context) {
		OK(c, h.ListPendingApprovals())
	})
	// 免审授权管理（「本会话允许」的持久化授权：查看 / 撤销）
	v1.GET("/chat/approval-grants", func(c *gin.Context) {
		OK(c, h.ListApprovalGrants())
	})
	v1.POST("/chat/approval-grants/:id/delete", func(c *gin.Context) {
		Fail(c, h.RevokeApprovalGrant(c.Param("id")))
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

}
