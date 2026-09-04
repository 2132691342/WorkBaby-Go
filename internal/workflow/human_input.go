package workflow

import (
	"context"
	"errors"
	"sync"
	"time"

	"WorkBaby/internal/event"
	"WorkBaby/internal/harness"
	"WorkBaby/internal/pkg"
)

// DefaultHumanInputResolver 内存实现的 HumanInputResolver：
//   - 推 workflow:input-required 事件给前端（api 层桥接到 Wails runtime）；
//   - 阻塞等待前端 ResolveHumanInput 回填；
//   - 24h 超时 / ctx 取消 → 9106；
//   - 决策通过 bus 同步回调（ResolveHumanInput 由 service 层调用）。
type DefaultHumanInputResolver struct {
	bus     *event.Bus
	mu      sync.Mutex
	waiters map[string]chan string // executionID+"|"+nodeID → value chan
}

// NewDefaultHumanInputResolver 构造。
func NewDefaultHumanInputResolver(bus *event.Bus) *DefaultHumanInputResolver {
	return &DefaultHumanInputResolver{bus: bus, waiters: map[string]chan string{}}
}

// key 唯一标识一次等待。
func waiterKey(executionID, nodeID string) string { return executionID + "|" + nodeID }

// Resolve 阻塞等待前端输入。
func (r *DefaultHumanInputResolver) Resolve(ctx context.Context, executionID, nodeID, prompt string, ttlSeconds int) (string, error) {
	if ttlSeconds <= 0 {
		ttlSeconds = 24 * 3600
	}
	ch := make(chan string, 1)
	r.mu.Lock()
	r.waiters[waiterKey(executionID, nodeID)] = ch
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.waiters, waiterKey(executionID, nodeID))
		r.mu.Unlock()
	}()

	// 通知前端
	r.bus.Publish("workflow:input-required", map[string]any{
		"runID":       harness.RunIDFromCtx(ctx),
		"executionID": executionID,
		"nodeID":      nodeID,
		"prompt":      prompt,
		"ttlSeconds":  ttlSeconds,
	})

	timeout := time.NewTimer(time.Duration(ttlSeconds) * time.Second)
	defer timeout.Stop()
	select {
	case v := <-ch:
		return v, nil
	case <-timeout.C:
		return "", pkg.New(9106, "人机输入超时", "")
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.Canceled) {
			return "", pkg.New(9107, "工作流已取消", "")
		}
		return "", pkg.New(9107, "ctx error: "+ctx.Err().Error(), "")
	}
}

// Deliver 把用户输入投递到等待者（service 层 ResolveHumanInput 绑定调用）。
//
// 返回 false 表示当前没有该节点在等（已超时/已取消）。
func (r *DefaultHumanInputResolver) Deliver(executionID, nodeID, value string) bool {
	r.mu.Lock()
	ch, ok := r.waiters[waiterKey(executionID, nodeID)]
	r.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- value:
		return true
	default:
		return false
	}
}
