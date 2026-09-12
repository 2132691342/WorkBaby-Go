package server

// 工作流与执行实例路由。

import (
	"WorkBaby/internal/api"
	"github.com/gin-gonic/gin"

	"WorkBaby/internal/domain"
)

func registerWorkflowRoutes(v1 *gin.RouterGroup, h *api.Handler) {
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

}
