// Package keepawake 无人值守防系统空闲休眠（仅防空闲、允许屏幕熄灭）。
// cron / workflow 长任务运行中持有，运行结束释放；引用计数保证并发任务只算一次。
//
// Windows 走 SetThreadExecutionState；其他平台为 no-op。
package keepawake

import (
	"sync"

	"WorkBaby/internal/pkg"
)

// Setter Windows 实现（构建标签 windows）。
// 其他平台定义 stub.go：方法空操作、计数始终 ≥ 0。
type Setter interface {
	// Acquire 防系统进入空闲睡眠，返回 release；多次 Acquire 必须配对释放。
	Acquire(reason string) releaseFn
}

// releaseFn 释放器（idempotent：重复释放不报错）。
type releaseFn func()

// referenceCounted 线程安全的 Setter 包装：自动维护 kernel32 引用计数。
type referenceCounted struct {
	mu sync.Mutex
	n  int
}

func (r *referenceCounted) Acquire(reason string) releaseFn {
	r.mu.Lock()
	r.n++
	count := r.n
	r.mu.Unlock()
	applyState(count > 0)
	pkg.L.Debug("keep-awake acquire", "reason", reason, "count", count)
	return func() {
		r.mu.Lock()
		r.n--
		count := r.n
		r.mu.Unlock()
		applyState(count > 0)
		pkg.L.Debug("keep-awake release", "reason", reason, "count", count)
	}
}

// NewSetter 构造一个引用计数安全的 Setter（多任务并发场景安全）。
func NewSetter() Setter { return &referenceCounted{} }
