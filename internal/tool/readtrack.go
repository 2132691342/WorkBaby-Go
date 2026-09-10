package tool

// 写前必须读：file_edit 的 old_string 一旦与磁盘内容不一致，轻则报错重来，
// 重则「匹配到别处」把文件改坏。记录本轮 run 内 file_read 过的文件，编辑前校验——
// 把「凭记忆改文件」从工具层堵掉，而不是只在 description 里劝。
//
// 作用域是 run（不是会话）：下一轮 run 不继承上一次的读态，
// 因为期间文件可能已被别处（用户 / 其他工具 / 外部编辑器）改过。
//
// 无 run 身份时（单测、脱链直调）不设限，保持裸调可用。

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
)

// maxTrackedReads 读态条数上限：超限整体清空。
// 最坏后果是模型多读一次文件，换来的是进程常驻内存在长会话里不无限增长。
const maxTrackedReads = 4096

var (
	readMu    sync.Mutex
	readFiles = map[string]struct{}{}
)

// MarkFileRead 记录本轮 run 已读取该文件（绝对路径）。
func MarkFileRead(ctx context.Context, absPath string) {
	key := readTrackKey(ctx, absPath)
	if key == "" {
		return
	}
	readMu.Lock()
	defer readMu.Unlock()
	if len(readFiles) >= maxTrackedReads {
		readFiles = map[string]struct{}{}
	}
	readFiles[key] = struct{}{}
}

// FileWasRead 本轮 run 是否读过该文件；无 run 身份返回 true（不设限）。
func FileWasRead(ctx context.Context, absPath string) bool {
	key := readTrackKey(ctx, absPath)
	if key == "" {
		return true
	}
	readMu.Lock()
	defer readMu.Unlock()
	_, ok := readFiles[key]
	return ok
}

// readTrackKey 读态键；无身份 / 空路径返回空串（调用方按「不设限」处理）。
func readTrackKey(ctx context.Context, absPath string) string {
	runID := RunIDFromCtx(ctx)
	if runID == "" || absPath == "" {
		return ""
	}
	// Windows 路径大小写不敏感：统一小写避免同一文件两种写法各记一次
	return runID + "|" + strings.ToLower(filepath.Clean(absPath))
}
