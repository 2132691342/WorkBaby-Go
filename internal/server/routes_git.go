// Package server Git 面板路由：会话工作区维度（session_id 定位仓库）。
package server

import (
	"WorkBaby/internal/api"

	"github.com/gin-gonic/gin"
)

func registerGitRoutes(g *gin.RouterGroup, h *api.Handler) {
	// 概览：分支 + 变更文件 + 本地分支 + 最近提交（面板首屏一次取齐）
	g.GET("/git/overview", func(c *gin.Context) {
		v, err := h.GitOverview(c.Query("session_id"))
		unwrap(c, v, err)
	})
	// 分支切换（create=true 时不存在即新建）
	g.POST("/git/switch", func(c *gin.Context) {
		var req api.GitSwitchReq
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		Fail(c, h.GitSwitch(req))
	})
	// unified diff（path 空 = 全部变更；staged=true 暂存区 vs HEAD）
	g.GET("/git/diff", func(c *gin.Context) {
		v, err := h.GitDiff(c.Query("session_id"), c.Query("path"), c.Query("staged") == "true")
		unwrap(c, v, err)
	})
	g.POST("/git/stage", func(c *gin.Context) {
		var req api.GitWriteReq
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		Fail(c, h.GitStage(req))
	})
	g.POST("/git/unstage", func(c *gin.Context) {
		var req api.GitWriteReq
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		Fail(c, h.GitUnstage(req))
	})
	g.POST("/git/discard", func(c *gin.Context) {
		var req api.GitWriteReq
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		Fail(c, h.GitDiscard(req))
	})
	// 提交（message 必填；stage_all 先 add -A）
	g.POST("/git/commit", func(c *gin.Context) {
		var req api.GitCommitReq
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		Fail(c, h.GitCommit(req))
	})
}
