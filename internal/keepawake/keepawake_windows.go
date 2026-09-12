//go:build windows

// Windows 实现：SetThreadExecutionState(ES_SYSTEM_REQUIRED | ES_DISPLAY_REQUIRED) 引用计数。
// 每对 Acquire/Release 配对；keepAwake 状态按当前计数是否 > 0 切换。
package keepawake

import (
	"syscall"

	"WorkBaby/internal/pkg"
)

// ES_SYSTEM_REQUIRED | ES_DISPLAY_REQUIRED：阻止系统空闲睡眠与显示器熄灭。
// 只接 EXECUTION_STATE 不接 system_parameters_info，因为前者不需要权限且不被反作弊拦截。
const esSystemRequired = 0x00000001
const esDisplayRequired = 0x00000002
const esContinuous = 0x80000000

var (
	modkernel32              = syscall.NewLazyDLL("kernel32.dll")
	procSetThreadExecState    = modkernel32.NewProc("SetThreadExecutionState")
)

// applyState 切换系统防休眠态；enabled=true 时持续置位，false 时清掉。
func applyState(enabled bool) {
	proc := procSetThreadExecState
	if proc.Find() != nil {
		// 进程没运行时（懒加载失败）静默跳过；调度不会因为 keep-awake 失效
		return
	}
	if enabled {
		_, _, _ = proc.Call(uintptr(esContinuous | esSystemRequired | esDisplayRequired))
	} else {
		// 关闭防休眠：传 0 即可（设置文档：清除先前的 ES_CONTINUOUS 标志）。
		_, _, _ = proc.Call(0)
	}
	pkg.L.Debug("keep-awake applied", "enabled", enabled)
}
