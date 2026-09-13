//go:build windows

package service

import (
	"context"
	"os/exec"
	"syscall"
)

// newShellCmd 构造外壳子进程：Windows 下走 SysProcAttr.CmdLine 原样透传命令行。
// os/exec 的参数转义会破坏用户命令里的引号（`cmd /c "echo ok"`、`node "C:\Program Files\x.js"` 直接执行失败），
// 故绕过转义，由 cmd 自己解析。
func newShellCmd(ctx context.Context, command string) *exec.Cmd {
	exe, err := exec.LookPath("cmd")
	if err != nil {
		exe = "cmd"
	}
	c := exec.CommandContext(ctx, exe)
	c.SysProcAttr = &syscall.SysProcAttr{CmdLine: exe + " /c " + command}
	return c
}
