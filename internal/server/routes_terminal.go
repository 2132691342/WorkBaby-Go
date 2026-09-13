// Package server 终端面板路由：开/跑/拉/关四动作。
package server

import (
	"WorkBaby/internal/api"

	"github.com/gin-gonic/gin"
)

func registerTerminalRoutes(g *gin.RouterGroup, h *api.Handler) {
	// 打开（幂等复用同会话终端）
	g.POST("/terminal/open", func(c *gin.Context) {
		var req api.TerminalOpenReq
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.TerminalOpen(req)
		unwrap(c, v, err)
	})
	// 写入一行命令
	g.POST("/terminal/:id/run", func(c *gin.Context) {
		var req api.TerminalRunReq
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		Fail(c, h.TerminalRun(req))
	})
	// 增量输出轮询（since = 上次 seq）
	g.GET("/terminal/:id/output", func(c *gin.Context) {
		v, err := h.TerminalOutput(c.Param("id"), atoi64(c.DefaultQuery("since", "0"), 0))
		unwrap(c, v, err)
	})
	// 关闭终端
	g.POST("/terminal/:id/stop", func(c *gin.Context) {
		Fail(c, h.TerminalStop(c.Param("id")))
	})
}
