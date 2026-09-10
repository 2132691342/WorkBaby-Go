package capability

import (
	"context"
	"runtime"
	"time"

	"WorkBaby/internal/harness"
	"WorkBaby/internal/tool"
)

// environmentCap 注入运行环境事实（操作系统 / 架构 / 当前时间 / exec 调用形态）。
//
// 模型不知道自己站在哪台机器上，就会写出必然跑不通的命令：exec 直接创建进程、
// 不过 shell，cmd 内建命令与管道语法都要显式包 cmd /c。这类事实只能由环境段告知，
// 靠模型猜必然失败——这也是长任务第一轮就卡死的高频原因。
type environmentCap struct{}

// NewEnvironment 构造环境能力。
func NewEnvironment() Capability { return &environmentCap{} }

func (c *environmentCap) ID() string { return "environment" }

func (c *environmentCap) Tools() []tool.Tool { return nil }

func (c *environmentCap) Capture(_ context.Context, _ *CaptureCtx) error { return nil }

func (c *environmentCap) Preload(_ context.Context, _ *PreloadCtx) ([]harness.ContextPiece, error) {
	return []harness.ContextPiece{{
		Key:   "environment",
		Title: "运行环境",
		Body: "操作系统：" + runtime.GOOS + " / " + runtime.GOARCH + "\n" +
			"当前时间：" + time.Now().Format("2006-01-02 15:04:05") + "\n" +
			"命令执行：" + shellHint(),
		Priority: harness.PriorityEssential,
	}}, nil
}
