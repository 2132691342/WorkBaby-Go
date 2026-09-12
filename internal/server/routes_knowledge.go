package server

// 知识库与记忆路由。

import (
	"WorkBaby/internal/api"
	"github.com/gin-gonic/gin"

	"WorkBaby/internal/domain"
)

func registerKnowledgeRoutes(v1 *gin.RouterGroup, h *api.Handler) {
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

	// ---- inbox（回写收件箱：run 终局候选 → 人审 → 合入记忆/技能） ----
	v1.GET("/memory/inbox", func(c *gin.Context) {
		limit := atoi(c.DefaultQuery("limit", "100"), 100)
		v, err := h.ListInbox(c.Query("status"), limit)
		unwrap(c, v, err)
	})
	v1.GET("/memory/inbox/stats", func(c *gin.Context) {
		v, err := h.GetInboxStats()
		unwrap(c, v, err)
	})
	v1.POST("/memory/inbox/:id/approve", func(c *gin.Context) {
		v, err := h.ApproveInboxItem(c.Param("id"))
		unwrap(c, v, err)
	})
	v1.POST("/memory/inbox/:id/reject", func(c *gin.Context) { Fail(c, h.RejectInboxItem(c.Param("id"))) })

}
