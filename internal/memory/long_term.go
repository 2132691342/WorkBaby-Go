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
// 默认落在 {home}/memory/{sessionID}/MEMORY.md；装配方可注入 resolve 覆盖落点
// （如会话绑定了外部工作目录时改放 {目录}/.workbaby/memory/）。
type longTerm struct {
	root    string                        // 默认记忆根 {home}/memory
	resolve func(sessionID string) string // 可选：返回某会话 MEMORY.md 绝对路径；nil = 默认根推导
}

func newLongTerm(home string) *longTerm {
	return &longTerm{root: filepath.Join(home, "memory")}
}

// Path 返回某会话的 MEMORY.md 路径。
func (l *longTerm) Path(sessionID string) string {
	if l.resolve != nil {
		if p := l.resolve(sessionID); p != "" {
			return p
		}
	}
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
	// 超限滚动压缩：保留较新的约 60%（从「## 」行边界切开），头部插入压缩标记后继续追加。
	// 旧实现满额直接拒绝写入——记忆静默停止积累，用户无感知。
	if fi, err := os.Stat(p); err == nil && fi.Size() > maxLongTermBytes {
		if rerr := l.rotate(p); rerr != nil {
			return domain.ErrMemoryIO
		}
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

// rotate 满额滚动：保留约后 60% 条目（从「## 」行边界切开），头部插入压缩标记。
// 确定性裁剪，不调 LLM。
func (l *longTerm) rotate(p string) error {
	bs, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	lines := strings.Split(string(bs), "\n")
	start := len(lines) * 2 / 5
	for start < len(lines) && !strings.HasPrefix(lines[start], "## ") {
		start++
	}
	if start >= len(lines) {
		start = len(lines) * 2 / 5
	}
	var kept strings.Builder
	kept.WriteString("[早期记忆已滚动压缩]\n\n")
	kept.WriteString(strings.Join(lines[start:], "\n"))
	return os.WriteFile(p, []byte(kept.String()), 0o644)
}
