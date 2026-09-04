package channel

import (
	"fmt"
	"sync"

	"WorkBaby/internal/domain"
)

// Registry 通道注册中心：类型 → 实现。装配方注册 email / webhook / console。
type Registry struct {
	mu       sync.RWMutex
	channels map[domain.ChannelType]Channel
}

// NewRegistry 构造空注册中心。
func NewRegistry() *Registry {
	return &Registry{channels: make(map[domain.ChannelType]Channel)}
}

// Register 注册；类型冲突返回错误（重复注册是装配缺陷）。
func (r *Registry) Register(c Channel) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.channels[c.Type()]; ok {
		return fmt.Errorf("channel type %q already registered", c.Type())
	}
	r.channels[c.Type()] = c
	return nil
}

// Get 按类型取实现；未注册返回 nil。
func (r *Registry) Get(t domain.ChannelType) Channel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.channels[t]
}
