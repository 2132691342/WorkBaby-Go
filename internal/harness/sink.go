package harness

// Sink 事件出口；Runner 发出 Event 经此推到 event.Bus / 落库 / 前端事件桥接。接口化便于单测注入 mock。
type Sink interface {
	Emit(Event)
}

// FuncSink 函数适配器，便于单测和 api/handler 简单调用。
type FuncSink func(Event)

func (f FuncSink) Emit(e Event) { f(e) }

// NopSink 空实现，单测静默。
type NopSink struct{}

func (NopSink) Emit(Event) {}
