package server

// 通知通道 / 定时任务 / 桌宠路由。

import (
	"WorkBaby/internal/api"
	"github.com/gin-gonic/gin"

	"WorkBaby/internal/domain"
)

func registerChannelRoutes(v1 *gin.RouterGroup, h *api.Handler) {
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
	// 命中区域：透明区域点击穿透（前端上报可见元素包围盒，Windows 端 SetWindowRgn 生效）
	v1.POST("/pet/window/click-through", func(c *gin.Context) {
		var req domain.PetClickThroughREQ
		if err := BindJSON(c, &req); err != nil {
			Fail(c, err)
			return
		}
		v, err := h.SetPetClickThrough(req)
		unwrap(c, v, err)
	})

}
