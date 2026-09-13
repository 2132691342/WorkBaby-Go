// Package domain 本文件：浏览器面板聚合（无独立表；进程态在内存）。
package domain

import "WorkBaby/internal/pkg"

// BrowserStatusRESP 浏览器运行态（面板轮询首字段）。
type BrowserStatusRESP struct {
	Running bool   `json:"running"`
	Browser string `json:"browser"` // 可执行文件名（msedge.exe / chrome.exe）
	URL     string `json:"url"`
	Title   string `json:"title"`
}

// BrowserScreenshotRESP 视口截图：base64 JPEG + CSS 视口尺寸（面板坐标换算用）。
type BrowserScreenshotRESP struct {
	Image string `json:"image"` // base64（无 data: 前缀）
	W     int    `json:"w"`     // CSS 视口宽
	H     int    `json:"h"`     // CSS 视口高
}

// BrowserElementRESP 快照中的可交互元素（工具 Data 与面板复用）。
type BrowserElementRESP struct {
	Index int    `json:"index"`
	Tag   string `json:"tag"`
	Text  string `json:"text"`
	X     int    `json:"x"`
	Y     int    `json:"y"`
	W     int    `json:"w"`
	H     int    `json:"h"`
}

// 包级错误变量；错误码段位 8700（浏览器面板）。
var (
	ErrBrowserNotRunning = pkg.New(8701, "managed browser is not running", "")
	ErrBrowserLaunch     = pkg.New(8702, "browser launch failed", "")
	ErrBrowserCmd        = pkg.New(8703, "browser command failed", "")
	ErrBrowserTimeout    = pkg.New(8704, "browser command timeout", "")
	ErrBrowserInvalidArg = pkg.New(8705, "browser invalid argument", "")
)
