// Package api 浏览器面板 HTTP 接口：启停 / 导航 / 交互 / 截图 / 快照。
// 面板远程视图与 browser_* 工具共用同一 BrowserService。
package api

import (
	"WorkBaby/internal/domain"
	"WorkBaby/internal/service"
)

// BrowserStatus 运行态。
func (h *Handler) BrowserStatus() (*domain.BrowserStatusRESP, error) {
	return h.browserSvc.Status()
}

// BrowserStart 拉起托管浏览器。
func (h *Handler) BrowserStart() (*domain.BrowserStatusRESP, error) {
	return h.browserSvc.Start()
}

// BrowserStop 关闭托管浏览器。
func (h *Handler) BrowserStop() error {
	return h.browserSvc.Stop()
}

// BrowserNavReq 导航入参。
type BrowserNavReq struct {
	URL string `json:"url"`
}

// BrowserNavigate 导航当前页。
func (h *Handler) BrowserNavigate(req BrowserNavReq) error {
	return h.browserSvc.Navigate(h.ctx, req.URL)
}

// BrowserXYReq 坐标交互入参（CSS 像素，面板截图坐标系）。
type BrowserXYReq struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// BrowserClick 坐标点击。
func (h *Handler) BrowserClick(req BrowserXYReq) error {
	return h.browserSvc.Click(h.ctx, req.X, req.Y)
}

// BrowserScrollReq 滚动入参。
type BrowserScrollReq struct {
	DX int `json:"dx"`
	DY int `json:"dy"`
}

// BrowserScroll 滚轮滚动。
func (h *Handler) BrowserScroll(req BrowserScrollReq) error {
	return h.browserSvc.Scroll(h.ctx, req.DX, req.DY)
}

// BrowserTypeReq 输入入参。
type BrowserTypeReq struct {
	Text   string `json:"text"`
	Submit bool   `json:"submit"`
}

// BrowserType 向焦点元素输入文本（submit 时补回车）。
func (h *Handler) BrowserType(req BrowserTypeReq) error {
	return h.browserSvc.Type(h.ctx, req.Text, req.Submit)
}

// BrowserKeyReq 按键入参。
type BrowserKeyReq struct {
	Key string `json:"key"`
}

// BrowserKey 按键（Enter/Tab/Escape/方向键等）。
func (h *Handler) BrowserKey(req BrowserKeyReq) error {
	return h.browserSvc.Key(h.ctx, req.Key)
}

// BrowserScreenshot 视口截图。
func (h *Handler) BrowserScreenshot() (*domain.BrowserScreenshotRESP, error) {
	return h.browserSvc.Screenshot(h.ctx)
}

// BrowserSnapshot 结构化页面快照（面板「读取页面」按钮）。
func (h *Handler) BrowserSnapshot() (*service.BrowserSnapshot, error) {
	return h.browserSvc.Snapshot(h.ctx)
}
