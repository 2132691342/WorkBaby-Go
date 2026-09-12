//go:build !windows

// 非 Windows 平台 stub：Acquire 返回的释放器空操作（cron/workflow 仍可调用，无副作用）。
// 桌面助手 macOS/Linux 应另接 IOMobileAssertion / inhibit systemd-logind，留待 v2。
package keepawake

// applyState 非 Windows 平台不实现；桌面助手平台另行接入电源管理。
func applyState(_ bool) {}
