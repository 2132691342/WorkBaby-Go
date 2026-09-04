package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"WorkBaby/internal/pkg"
)

// ErrorClass 错误分类桶：分类驱动重试/降级策略。
type ErrorClass string

const (
	ClassTransient ErrorClass = "transient" // 429/5xx/超时/连接重置：可重试
	ClassAuth      ErrorClass = "auth"      // 401/403/key 无效：配置错误，重试无义
	ClassContext   ErrorClass = "context"   // 上下文过长：需压缩，重试无义
	ClassRequest   ErrorClass = "request"   // 其余 4xx：请求本身错，重试无义
	ClassCancelled ErrorClass = "cancelled" // 调用方取消：永远不重试
	ClassUnknown   ErrorClass = "unknown"   // 未知：保守不重试
)

// ClassifyError 把错误稳定归入桶。
//
// <p>provider 层经 pkg.Wrap 把原始段位码字符串化进 Details（Wrap 不保留 cause 链），
// 故先看外层 AppError 码，再扫描文本中的内层 [code] 段位标记。
func ClassifyError(err error) ErrorClass {
	if err == nil {
		return ClassUnknown
	}
	if errors.Is(err, context.Canceled) {
		return ClassCancelled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ClassTransient
	}
	text := err.Error()
	if ae, ok := pkg.As(err); ok {
		switch {
		case ae.Code == 3003 || ae.Code == 3004 || ae.Code == 3005:
			return ClassTransient
		case ae.Code == 3001 || ae.Code == 3002:
			return ClassAuth
		case ae.Code == 3008:
			return ClassContext
		case ae.Code == 3006 || ae.Code == 3007 || ae.Code == 3009:
			return ClassRequest
		}
	} else {
		// 无 AppError 的 transport 层错误：超时/连接类可重试
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return ClassTransient
		}
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) {
			return ClassTransient
		}
	}
	// 内层段位码（被 Wrap 字符串化进 Details）
	for _, m := range []struct {
		marker string
		class  ErrorClass
	}{
		{"[3003]", ClassTransient}, {"[3004]", ClassTransient}, {"[3005]", ClassTransient},
		{"[3001]", ClassAuth}, {"[3002]", ClassAuth},
		{"[3008]", ClassContext},
		{"[3006]", ClassRequest}, {"[3007]", ClassRequest}, {"[3009]", ClassRequest},
	} {
		if strings.Contains(text, m.marker) {
			return m.class
		}
	}
	return ClassUnknown
}

// IsTransient 是否可重试（仅瞬时类）。调用方取消永远优先且不重试。
func IsTransient(err error) bool { return ClassifyError(err) == ClassTransient }

// retryAfterRe provider 层在 429 响应时把 Retry-After 嵌入错误文本（retry_after=3s）。
var retryAfterRe = regexp.MustCompile(`retry_after=(\d+)s`)

// RetryAfter 提取错误中的 Retry-After 提示；无/越界返回 0。
func RetryAfter(err error) time.Duration {
	if err == nil {
		return 0
	}
	m := retryAfterRe.FindStringSubmatch(err.Error())
	if m == nil {
		return 0
	}
	sec, perr := strconv.Atoi(m[1])
	if perr != nil || sec <= 0 || sec > 120 {
		return 0
	}
	return time.Duration(sec) * time.Second
}

// RetryPolicy 有界重试参数：指数退避 + 抖动 + Retry-After 优先 + 取消优先。
type RetryPolicy struct {
	MaxAttempts int           // 含首调的总尝试次数
	BaseDelay   time.Duration // 首次重试基准延迟
	MaxDelay    time.Duration // 延迟封顶
}

// DefaultRetryPolicy 桌面助手取保守值：3 次尝试、800ms 起、8s 封顶。
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{MaxAttempts: 3, BaseDelay: 800 * time.Millisecond, MaxDelay: 8 * time.Second}
}

// Backoff 第 attempt 次重试（0 起）的等待：Retry-After 优先，否则指数退避 + 0~25% 抖动再封顶。
func (p RetryPolicy) Backoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}
	d := p.BaseDelay << uint(attempt)
	if d <= 0 || d > p.MaxDelay {
		d = p.MaxDelay
	}
	jitter := time.Duration(rand.Int64N(int64(d)/4 + 1))
	if d+jitter > p.MaxDelay {
		return p.MaxDelay
	}
	return d + jitter
}

// Wait 等待 d 且尊重 ctx 取消（取消立即返回，不吞不重）。
func Wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// RetryAfterHint 提取 429 响应的 Retry-After 秒数后缀（retry_after=Ns），
// 供 provider 层拼进错误文本，重试策略据此尊重服务端节奏；缺失/非法返回空串。
func RetryAfterHint(status int, header func(string) string) string {
	if status != 429 || header == nil {
		return ""
	}
	v := strings.TrimSpace(header("Retry-After"))
	if v == "" {
		return ""
	}
	sec, err := strconv.Atoi(v)
	if err != nil || sec <= 0 || sec > 120 {
		return ""
	}
	return fmt.Sprintf(" retry_after=%ds", sec)
}
