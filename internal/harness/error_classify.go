package harness

import (
	"context"
	"errors"
	"strings"

	"WorkBaby/internal/pkg"
)

// 运行错误分类：机器可读 kind + 可操作提示（hint 追加到用户可见消息尾部，
// 用户直接看到「去哪调配置」）。只做关键词模式匹配，不引入新依赖。

// 错误类别（ErrorPayload.Kind）。
const (
	ErrKindTimeout        = "timeout"
	ErrKindRateLimited    = "rate_limited"
	ErrKindAuth           = "auth"
	ErrKindContextLength  = "context_length"
	ErrKindConnection     = "connection"
	ErrKindApprovalDenied = "approval_denied"
	ErrKindUpstream       = "upstream"
	ErrKindUnknown        = ""
)

// classifyRunError 归类 run 失败原因并给出可操作提示（hint 非空时追加到用户可见消息尾部）。
func classifyRunError(err error) (kind, hint string) {
	if err == nil {
		return ErrKindUnknown, ""
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ErrKindTimeout, "响应超时：可在 设置→模型 调大超时时间，或更换响应更快的模型后重试"
	}
	// AppError 优先按码归类；否则按消息关键词
	if ae, ok := pkg.As(err); ok {
		switch {
		case ae.Code >= 3000 && ae.Code < 4000: // LLM / Provider 域
			return classifyByText(ae.Message + " " + ae.Details)
		}
	}
	msg := ""
	if ae, ok := pkg.As(err); ok {
		msg = ae.Message + " " + ae.Details
	} else {
		msg = err.Error()
	}
	return classifyByText(msg)
}

// classifyByText 按错误文本关键词归类（上游报错形态各异，关键词覆盖主流厂商）。
func classifyByText(msg string) (string, string) {
	lower := strings.ToLower(msg)
	switch {
	case containsAny(lower, "429", "rate limit", "ratelimit", "too many requests", "限流", "throttl"):
		return ErrKindRateLimited, "触发限流：稍等片刻重试，或在 设置→模型 更换供应商/降低并发"
	case containsAny(lower, "401", "403", "unauthorized", "invalid api key", "authentication", "鉴权", "api key"):
		return ErrKindAuth, "鉴权失败：请到 设置→模型 检查 API Key 是否有效、是否有对应模型权限"
	case containsAny(lower, "context length", "maximum context", "token limit", "too many tokens", "context window", "上下文超"):
		return ErrKindContextLength, "上下文超限：请新开会话，或在 设置→对话 调低压缩比例让历史更早被压缩"
	case containsAny(lower, "timeout", "deadline", "超时", "timed out"):
		return ErrKindTimeout, "响应超时：可在 设置→模型 调大超时时间，或更换响应更快的模型后重试"
	case containsAny(lower, "connection", "connect", "eof", "broken pipe", "reset by peer", "network", "dns", "connection refused"):
		return ErrKindConnection, "网络连接失败：检查网络或代理设置后重试"
	case containsAny(lower, "not found", "404", "model not exist", "不存在"):
		return ErrKindUpstream, "上游返回资源不存在：确认模型名拼写与供应商是否支持该模型"
	case containsAny(lower, "invalid params", "bad request", "400"):
		return ErrKindUpstream, "上游拒绝请求参数：若反复出现，尝试新开会话（历史中可能残留无效上下文）"
	default:
		return ErrKindUnknown, ""
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
