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
	// 提交历史分页（limit / skip / all）
	g.GET("/git/log", func(c *gin.Context) {
		v, err := h.GitLog(c.Query("session_id"), atoi(c.Query("limit"), 20), atoi(c.Query("skip"), 0), c.Query("all") == "true")
		unwrap(c, v, err)
	})
	// 提交详情：元信息 + 变更文件清单
	g.GET("/git/commit", func(c *gin.Context) {
		v, err := h.GitCommitDetail(c.Query("session_id"), c.Query("hash"))
		unwrap(c, v, err)
	})
	// 提交内单文件 diff
	g.GET("/git/commit/diff", func(c *gin.Context) {
		v, err := h.GitCommitFileDiff(c.Query("session_id"), c.Query("hash"), c.Query("path"))
		unwrap(c, v, err)
	})
	// 提交图谱（只读文本）
	g.GET("/git/graph", func(c *gin.Context) {
		v, err := h.GitGraph(c.Query("session_id"), atoi(c.Query("limit"), 120))
		unwrap(c, v, err)
	})
	// 删除本地分支（当前分支被拒）
	g.POST("/git/branch/delete", func(c *gin.Context) {
		var req api.GitBranchReq
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		Fail(c, h.GitDeleteBranch(req))
	})
}
