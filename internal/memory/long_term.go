package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"WorkBaby/internal/domain"
)

// maxLongTermBytes MEMORY.md 超限阈值。
const maxLongTermBytes = 100 << 10

// longTerm 长期记忆：每会话一个 MEMORY.md 文件。
type longTerm struct {
	root string // {home}/workspaces
}

func newLongTerm(home string) *longTerm {
	return &longTerm{root: filepath.Join(home, "workspaces")}
}

// Path 返回某会话的 MEMORY.md 路径。
func (l *longTerm) Path(sessionID string) string {
	return filepath.Join(l.root, sessionID, "MEMORY.md")
}

// Load 全量读取；不存在或读失败返回空串（记忆缺失不阻断对话）。
func (l *longTerm) Load(_ context.Context, sessionID string) string {
	bs, err := os.ReadFile(l.Path(sessionID))
	if err != nil {
		return ""
	}
	return string(bs)
}

// Append 追加一条带日期的 delta。
func (l *longTerm) Append(_ context.Context, sessionID string, delta string) error {
	delta = strings.TrimSpace(delta)
	if delta == "" {
		return nil
	}
	p := l.Path(sessionID)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return domain.ErrMemoryIO
	}
	// 超限保护：提示但不写入
	if fi, err := os.Stat(p); err == nil && fi.Size() > maxLongTermBytes {
		return domain.ErrMemoryClean
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return domain.ErrMemoryIO
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "\n## %s\n\n%s\n", time.Now().Format("2006-01-02"), delta)
	if err != nil {
		return domain.ErrMemoryIO
	}
	return nil
}
