package llm

import "WorkBaby/internal/pkg"

// LLM 段错误码：3000-3999。
// Provider 适配层在转换错误时统一映射。
var (
	ErrLLMGeneric      = pkg.New(3000, "LLM generic error", "")
	ErrProviderUnready = pkg.New(3001, "provider not ready", "")
	ErrAPIKeyInvalid   = pkg.New(3002, "API key invalid (401/403)", "")
	ErrRateLimited     = pkg.New(3003, "rate limited (429)", "")
	ErrProvider5xx     = pkg.New(3004, "provider service error (5xx)", "")
	ErrTimeout         = pkg.New(3005, "response timeout", "")
	ErrBadRequest      = pkg.New(3006, "request error (4xx non-401/403/429)", "")
	ErrModelNotFound   = pkg.New(3007, "model not found or no permission", "")
	ErrContextTooLong  = pkg.New(3008, "context too long (400/413)", "")
	ErrNotImplemented  = pkg.New(3009, "provider does not support this feature", "")
)

// MapHTTPStatus 把 HTTP 状态码映射到错误码；Transport 错误统一归 3005。
func MapHTTPStatus(status int) *pkg.AppError {
	switch {
	case status == 401 || status == 403:
		return ErrAPIKeyInvalid
	case status == 404:
		return ErrModelNotFound
	case status == 408:
		return ErrTimeout
	case status == 413:
		return ErrContextTooLong
	case status == 429:
		return ErrRateLimited
	case status >= 500:
		return ErrProvider5xx
	case status >= 400:
		return ErrBadRequest
	default:
		return nil
	}
}
