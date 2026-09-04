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
		repo.NewMessageRepo(gdb),
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

// TestFormationMultiStep 多工具成功触发 MULTI_STEP_SUCCESS。
func TestFormationMultiStep(t *testing.T) {
	f := NewFormationPolicy(DefaultThresholds())
	transcript := []llm.Message{
		*llm.UserMessage("帮我查资料并整理成文档"),
		*llm.AssistantMessage("开始处理", []llm.ToolCall{
			{ID: "1", Function: llm.FunctionCall{Name: "websearch"}},
			{ID: "2", Function: llm.FunctionCall{Name: "webfetch"}},
			{ID: "3", Function: llm.FunctionCall{Name: "file_write"}},
		}),
	}
	res := f.Evaluate(context.Background(), "S2", transcript)
	if res.Episode == nil {
		t.Fatalf("multi-step should form episode")
	}
	if !contains(res.Episode.Triggers, "MULTI_STEP_SUCCESS") {
		t.Fatalf("want MULTI_STEP_SUCCESS, got %v", res.Episode.Triggers)
	}
}

// TestFormationV2Semantic 偏好/记住 → 事实条目（v2）。
func TestFormationV2Semantic(t *testing.T) {
	f := NewFormationPolicy(DefaultThresholds())
	pref := f.Evaluate(context.Background(), "S1", []llm.Message{
		*llm.UserMessage("我喜欢用简洁的回答"),
		*llm.AssistantMessage("好的。", nil),
	})
	if len(pref.Semantic) != 1 || pref.Semantic[0].Subject != "user" || pref.Semantic[0].Key != "preference" {
		t.Fatalf("want preference fact, got %+v", pref.Semantic)
	}
	rem := f.Evaluate(context.Background(), "S1", []llm.Message{
		*llm.UserMessage("请记住我住在上海"),
		*llm.AssistantMessage("已记住。", nil),
	})
	if len(rem.Semantic) != 1 || rem.Semantic[0].Key != "remembered" {
		t.Fatalf("want remembered fact, got %+v", rem.Semantic)
	}
}

// TestFormationV2Procedural ≥3 次工具调用 → 流程条目（v2）+ 步骤去重。
func TestFormationV2Procedural(t *testing.T) {
	f := NewFormationPolicy(DefaultThresholds())
	res := f.Evaluate(context.Background(), "S2", []llm.Message{
		*llm.UserMessage("帮我查资料并整理成文档"),
		*llm.AssistantMessage("开始处理", []llm.ToolCall{
			{ID: "1", Function: llm.FunctionCall{Name: "websearch"}},
			{ID: "2", Function: llm.FunctionCall{Name: "websearch"}},
			{ID: "3", Function: llm.FunctionCall{Name: "webfetch"}},
			{ID: "4", Function: llm.FunctionCall{Name: "file_write"}},
		}),
	})
	if len(res.Procedural) != 1 {
		t.Fatalf("want 1 procedure, got %d", len(res.Procedural))
	}
	want := []string{"websearch", "webfetch", "file_write"}
	if !equalStrSlice(res.Procedural[0].Steps, want) {
		t.Fatalf("want deduped steps %v, got %v", want, res.Procedural[0].Steps)
	}
}

// ===== 三层记忆写读 + 召回 =====


// TestSearchEpisodes 关键词搜索情景记忆：空 query 必须无命中而非报错。
func TestSearchEpisodes(t *testing.T) {
	s := newMemService(t)
	ctx := context.Background()
	id1, err := s.WriteEpisode(ctx, EpisodeProposal{SessionID: "SESSION_SE", Summary: "用户喜欢在下午三点喝茶"})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := s.WriteEpisode(ctx, EpisodeProposal{SessionID: "SESSION_SE", Summary: "用户使用 Go 开发桌面应用"}); err != nil {
		t.Fatalf("write: %v", err)
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
}


// ===== 长期记忆：append 顺序 / 累积 =====

// contains / equalStrSlice 小工具。
func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func equalStrSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
