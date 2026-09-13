// Package server 浏览器面板路由：托管浏览器远程视图端点。
package server

import (
	"WorkBaby/internal/api"

	"github.com/gin-gonic/gin"
)

func registerBrowserRoutes(g *gin.RouterGroup, h *api.Handler) {
	g.GET("/browser/status", func(c *gin.Context) {
		v, err := h.BrowserStatus()
		unwrap(c, v, err)
	})
	g.POST("/browser/start", func(c *gin.Context) {
		v, err := h.BrowserStart()
		unwrap(c, v, err)
	})
	g.POST("/browser/stop", func(c *gin.Context) {
		Fail(c, h.BrowserStop())
	})
	g.POST("/browser/navigate", func(c *gin.Context) {
		var req api.BrowserNavReq
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		Fail(c, h.BrowserNavigate(req))
	})
	g.POST("/browser/click", func(c *gin.Context) {
		var req api.BrowserXYReq
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		Fail(c, h.BrowserClick(req))
	})
	g.POST("/browser/scroll", func(c *gin.Context) {
		var req api.BrowserScrollReq
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		Fail(c, h.BrowserScroll(req))
	})
	g.POST("/browser/type", func(c *gin.Context) {
		var req api.BrowserTypeReq
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		Fail(c, h.BrowserType(req))
	})
	g.POST("/browser/key", func(c *gin.Context) {
		var req api.BrowserKeyReq
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		Fail(c, h.BrowserKey(req))
	})
	// 视口截图（面板轮询 + 点击坐标换算基准）
	g.GET("/browser/screenshot", func(c *gin.Context) {
		v, err := h.BrowserScreenshot()
		unwrap(c, v, err)
	})
	// 结构化页面快照
	g.GET("/browser/snapshot", func(c *gin.Context) {
		v, err := h.BrowserSnapshot()
		unwrap(c, v, err)
	})
}
