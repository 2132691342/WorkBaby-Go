package exec

import (
	"strings"
	"testing"
)

// TestCappedWriter 回归：输出限流必须头保留 + 丢弃计数——
// 旧实现 CombinedOutput 无上限，`find /`、`cat 大文件` 会把全量输出读进内存（OOM 风险）。
func TestCappedWriter(t *testing.T) {
	// 未超限：原样保留
	w := &cappedWriter{}
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if w.buf.String() != "hello" || w.dropped != 0 {
		t.Fatalf("under limit: buf=%q dropped=%d", w.buf.String(), w.dropped)
	}

	// 超限：保留头部 max 字节，多余字节计数不缓存
	w2 := &cappedWriter{}
	payload := strings.Repeat("a", execMaxOutputBytes+1234)
	if _, err := w2.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if w2.buf.Len() != execMaxOutputBytes {
		t.Fatalf("buf len = %d, want %d", w2.buf.Len(), execMaxOutputBytes)
	}
	if w2.dropped != 1234 {
		t.Fatalf("dropped = %d, want 1234", w2.dropped)
	}

	// 多次写入跨上限：第二次写入的剩余额度正确
	w3 := &cappedWriter{}
	_, _ = w3.Write([]byte(strings.Repeat("b", execMaxOutputBytes-10)))
	_, _ = w3.Write([]byte(strings.Repeat("c", 100)))
	if w3.buf.Len() != execMaxOutputBytes || w3.dropped != 90 {
		t.Fatalf("split write: len=%d dropped=%d", w3.buf.Len(), w3.dropped)
	}
}
