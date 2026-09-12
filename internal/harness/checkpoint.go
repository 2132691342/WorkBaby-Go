package harness

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"WorkBaby/internal/llm"
	"WorkBaby/internal/pkg"
)

// Checkpoint 单轮结束后的运行快照。
//
// 语义是「可覆盖的最新一轮」：每 run 只留最后一个 turn（Resume 只认最后一轮），
// 逐轮累积会让一次 30 轮 run 放大成 30× 全文。
type Checkpoint struct {
	RunID          string         `json:"runID"`
	SessionID      string         `json:"sessionID"`
	Turn           int            `json:"turn"`
	Messages       []*llm.Message `json:"messages"`
	State          RunState       `json:"state"`
	AssistantMsgID string         `json:"assistantMsgID,omitempty"` // Resume 续跑落回同一条 assistant 消息
	Content        string         `json:"content,omitempty"`        // 已累积正文
	Thinking       string         `json:"thinking,omitempty"`       // 已累积推理
	Usage          llm.TokenUsage `json:"usage,omitempty"`
	// StepRecords 幂等恢复：已完成（成功）工具调用按 tool:{name}:{args} 记结果，Resume 命中复用不重放副作用。
	StepRecords map[string]StepRecord `json:"stepRecords,omitempty"`
	CreatedAt   int64                 `json:"createdAt"`
}

// StepRecord 单条已完成工具调用的结果快照。
type StepRecord struct {
	Content    string `json:"content,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`
}

// CheckpointStore 检查点存储抽象：Append（每轮工具回填后追加）、LoadLast（取最新一轮供
// Resume 续跑）、Cleanup（每会话保留最近 keep 个 run）。
//
// 内置 FileCheckpointStore（JSONL）；service 层注入 SQL 实现（agent_checkpoints 表，
// 跨进程重启可恢复）。
type CheckpointStore interface {
	Append(cp *Checkpoint) error
	LoadLast(sessionID, runID string) (*Checkpoint, error)
	Cleanup(sessionID string, keep int) error
}

// FileCheckpointStore JSONL 文件实现：{root}/{sessionId}/{runId}.jsonl，每轮一行追加。
type FileCheckpointStore struct {
	root string
}

// NewCheckpointStore 构造 JSONL 文件检查点存储。
func NewCheckpointStore(root string) *FileCheckpointStore { return &FileCheckpointStore{root: root} }

// Append 追加一行检查点。
func (s *FileCheckpointStore) Append(cp *Checkpoint) error {
	if s == nil || s.root == "" {
		return nil
	}
	if !safeRunPath(cp.SessionID) || !safeRunPath(cp.RunID) {
		return pkg.New(5005, "检查点路径非法", cp.SessionID+"/"+cp.RunID)
	}
	cp.CreatedAt = time.Now().UnixMilli()
	dir := filepath.Join(s.root, cp.SessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return pkg.Wrap(5005, "检查点目录创建失败", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, cp.RunID+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return pkg.Wrap(5005, "检查点文件打开失败", err)
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(cp); err != nil {
		return pkg.Wrap(5005, "检查点写入失败", err)
	}
	return nil
}

// LoadLast 读取最后一个检查点；文件不存在 / 无记录返回 5007。
func (s *FileCheckpointStore) LoadLast(sessionID, runID string) (*Checkpoint, error) {
	if s == nil || s.root == "" {
		return nil, pkg.New(5007, "检查点未启用", "")
	}
	f, err := os.Open(filepath.Join(s.root, sessionID, runID+".jsonl"))
	if err != nil {
		return nil, pkg.Wrap(5005, "检查点加载失败", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var last *Checkpoint
	for sc.Scan() {
		var cp Checkpoint
		if err := json.Unmarshal(sc.Bytes(), &cp); err != nil {
			return nil, pkg.Wrap(5005, "检查点解析失败", err)
		}
		last = &cp
	}
	if err := sc.Err(); err != nil {
		return nil, pkg.Wrap(5005, "检查点读取失败", err)
	}
	if last == nil {
		return nil, pkg.New(5007, "检查点不存在", runID)
	}
	return last, nil
}

// Cleanup 每个会话保留最近 keep 个 run 的检查点文件，更早的删除。
func (s *FileCheckpointStore) Cleanup(sessionID string, keep int) error {
	if s == nil || s.root == "" || keep <= 0 {
		return nil
	}
	dir := filepath.Join(s.root, sessionID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // 目录不存在 = 无检查点，非错误
	}
	// 先一次性取回 (文件名, mtime) 再排序：比较函数里逐次 os.Stat 会放大成
	// O(n log n) 次系统调用，ReadDir 的 DirEntry 已带元信息，取一次即可。
	type runFile struct {
		name string
		mod  time.Time
	}
	runs := make([]runFile, 0, len(entries))
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // 竞态删除：跳过而非中断清理
		}
		runs = append(runs, runFile{name: e.Name(), mod: info.ModTime()})
	}
	if len(runs) <= keep {
		return nil
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].mod.After(runs[j].mod) })
	for _, r := range runs[keep:] {
		_ = os.Remove(filepath.Join(dir, r.name))
	}
	return nil
}

// safeRunPath 校验 sessionID / runID 是单段安全文件名（防路径穿越）。
func safeRunPath(s string) bool {
	return s != "" && filepath.Base(s) == s && !strings.ContainsAny(s, `/\:`)
}
