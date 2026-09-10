package memory

import (
	"context"
	"path/filepath"
	"testing"

	"WorkBaby/internal/db"
	"WorkBaby/internal/llm"
	"WorkBaby/internal/repo"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// memory_test.go 整合：形成策略 + 三层记忆写读 + 召回 + 长期记忆顺序。
// 三类记忆（episodic / semantic / procedural）的闭环以 TestEvaluateWriteEpisode 覆盖。

// ===== 基础设施 =====

func newMemTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "test.db") + "?_pragma=journal_mode(MEMORY)&_pragma=busy_timeout(5000)"
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.CreateFTS5(gdb); err != nil {
		t.Fatalf("fts5: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := gdb.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return gdb
}

func newMemService(t *testing.T) *Service {
	t.Helper()
	gdb := newMemTestDB(t)
	return NewService(
		repo.NewMemoryEpisodeRepo(gdb),
		repo.NewMemoryFactRepo(gdb),
		repo.NewMemoryProcedureRepo(gdb),
		t.TempDir(),
	)
}

// ===== 形成策略 =====

// TestFormationTriggers 各 trigger 单独打分（含 dislike 一票否决）。
func TestFormationTriggers(t *testing.T) {
	f := NewFormationPolicy(DefaultThresholds())
	cases := []struct {
		name        string
		transcript  []llm.Message
		wantEpisode bool
		wantTrig    string
	}{
		{"explicit-remember", []llm.Message{
			*llm.UserMessage("请记住我住在上海"),
			*llm.AssistantMessage("好的，已记住。", nil),
		}, true, "EXPLICIT_REMEMBER"},
		{"preference", []llm.Message{
			*llm.UserMessage("我喜欢用简洁的回答"),
			*llm.AssistantMessage("好的。", nil),
		}, true, "PREFERENCE_DETECTED"},
		{"dislike-suppress", []llm.Message{
			*llm.UserMessage("我不喜欢这个回答，别这样"),
			*llm.AssistantMessage("抱歉。", nil),
		}, false, "USER_DISLIKE"},
		{"empty", []llm.Message{}, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := f.Evaluate(context.Background(), "S1", c.transcript)
			if c.wantEpisode && res.Episode == nil {
				t.Fatalf("want episode, got nil")
			}
			if !c.wantEpisode && res.Episode != nil {
				t.Fatalf("want no episode, got %+v", res.Episode)
			}
			if c.wantTrig != "" && res.Episode != nil {
				found := false
				for _, tr := range res.Episode.Triggers {
					if tr == c.wantTrig {
						found = true
					}
				}
				if !found {
					t.Fatalf("want trigger %s, got %v", c.wantTrig, res.Episode.Triggers)
				}
			}
		})
	}
}

// TestDislikeRegexRegression 回归：技术高频词「错误 / wrong」不能触发负反馈一票否决，
// 否则正常排错对话会被整体抑制，排错经验完全不沉淀记忆。
func TestDislikeRegexRegression(t *testing.T) {
	nonNegative := []string{
		"这个错误需要排查",
		"错误处理逻辑在哪",
		"返回了 wrong answer，看看哪一步错了",
		"不要把报错当成故障",
	}
	for _, s := range nonNegative {
		if dislikeRe.MatchString(s) {
			t.Fatalf("技术语境不应命中 dislike: %q", s)
		}
	}
	positive := []string{"我不满意这个结果", "太难用了，换一个方案", "别这样操作"}
	for _, s := range positive {
		if !dislikeRe.MatchString(s) {
			t.Fatalf("明确负反馈应命中 dislike: %q", s)
		}
	}
}

// ===== 召回治理：时间衰减 + 近重复合并 =====

// TestRecallDecayAndDedupe 回归：旧记忆长期霸占召回头部、不同会话沉淀的重复
// 结论重复注入——两者都会稀释上下文。衰减让新记忆优先，去重保住信息密度。
func TestRecallDecayAndDedupe(t *testing.T) {
	now := int64(1_800_000_000_000)
	day := int64(86400000)

	// 衰减：30 天半衰期，同分下旧记录权重约减半
	fresh := rrfMerge([][]RecallHit{{{Kind: "episodic", Source: "a", Score: 1, Snippet: "new"}}}, 5, now, 0)
	old := rrfMerge([][]RecallHit{{{Kind: "episodic", Source: "b", Score: 1, CreatedAt: now - 30*day, Snippet: "old"}}}, 5, now, 0)
	if old[0].Score < fresh[0].Score*0.49 || old[0].Score > fresh[0].Score*0.51 {
		t.Fatalf("30 天衰减应约等于半衰: fresh=%f old=%f", fresh[0].Score, old[0].Score)
	}

	// 去重：归一化后互为包含的片段只留高分者
	merged := rrfMerge([][]RecallHit{
		{{Kind: "episodic", Source: "a", Score: 1, Snippet: "用户偏好 Go 语言 开发"}},
		{{Kind: "semantic", Source: "b", Score: 1, Snippet: "用户偏好Go语言"}},
		{{Kind: "procedural", Source: "c", Score: 1, Snippet: "完全不同的内容"}},
	}, 5, now, 0)
	if len(merged) != 2 {
		t.Fatalf("重复片段应合并, got %d: %+v", len(merged), merged)
	}

	// 无时间戳的命中不衰减
	noTime := rrfMerge([][]RecallHit{{{Kind: "episodic", Source: "d", Score: 1, Snippet: "x"}}}, 5, now, 0)
	if noTime[0].Score != 1.0/(rrfK+1) {
		t.Fatalf("无时间戳不应衰减: %f", noTime[0].Score)
	}

	// 相关性下限：单源低排名 + 极度陈旧（1 年）的命中必须被过滤，不注入噪声
	stale := rrfMerge([][]RecallHit{{{Kind: "episodic", Source: "e", Score: 1, CreatedAt: now - 365*day, Snippet: "stale"}}}, 5, now, DefaultMinRecallScore)
	if len(stale) != 0 {
		t.Fatalf("陈旧低分命中应被下限过滤, got %+v", stale)
	}
}

// ===== 三层记忆写读 + 召回 =====

// TestSearchEpisodes 关键词搜索情景记忆：空 query 必须无命中而非报错。
func TestSearchEpisodes(t *testing.T) {
	s := newMemService(t)
	ctx := context.Background()
	id1, err := s.WriteEpisode(ctx, EpisodeProposal{SessionID: "SESSION_SE", Summary: "用户喜欢在下午三点喝茶，偏好龙井"})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := s.WriteEpisode(ctx, EpisodeProposal{SessionID: "SESSION_SE", Summary: "用户使用 Go 开发桌面应用"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	// 写入侧近重复合并：同结论换措辞重写不应再落一条（否则污染 MEMORY.md 与召回排序）
	dupID, err := s.WriteEpisode(ctx, EpisodeProposal{SessionID: "SESSION_SE", Summary: "用户喜欢下午三点喝茶，偏好龙井"})
	if err != nil {
		t.Fatalf("write dup: %v", err)
	}
	if dupID != id1 {
		t.Fatalf("近重复摘要应复用已有 episode: got %s want %s", dupID, id1)
	}

	rows, err := s.SearchEpisodes(ctx, "下午三点", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != id1 {
		t.Fatalf("want 1 hit for 下午三点, got %+v", rows)
	}

	rows, err = s.SearchEpisodes(ctx, "", 5)
	if err != nil || len(rows) != 0 {
		t.Fatalf("empty query: want 0 hits, got %d (%v)", len(rows), err)
	}

	// 2 字中文查询：trigram 无 3 字窗口可用 → 必须由带打分的子串兜底召回，
	// 且无关查询不得因「按时间倒序」把最新那条（与 query 无关）塞回来。
	rows, err = s.SearchEpisodes(ctx, "下午", 5)
	if err != nil {
		t.Fatalf("search short: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != id1 {
		t.Fatalf("want 1 hit for 下午（子串兜底）, got %+v", rows)
	}
	rows, err = s.SearchEpisodes(ctx, "园艺", 5)
	if err != nil {
		t.Fatalf("search miss: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("无关查询必须零命中（禁止按 created_at 兜底返回）, got %+v", rows)
	}
}

// ===== 长期记忆：append 顺序 / 累积 =====
