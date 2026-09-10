package file

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// edit_test.go 覆盖 file_edit 的编辑原子性：唯一匹配校验是防「改坏文件」的核心护栏，
// diff 回执是模型确认改动的唯一信号。

func newEditForTest(t *testing.T, content string) (*EditTool, string) {
	t.Helper()
	root := t.TempDir()
	p := filepath.Join(root, "a.txt")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return NewEdit(nil, root), p
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	bs, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return bs
}

// TestFileEditReplace 与 file_edit 的三条护栏：未找到拒绝 / 多处匹配拒绝 / 唯一替换生效。
func TestFileEditReplace(t *testing.T) {
	cases := []struct {
		name    string
		content string
		old     string
		replace bool
		wantErr string
	}{
		{"not-found", "hello world", "missing text", false, "old_string not found"},
		{"ambiguous", "dup\nmid\ndup", "dup", false, "匹配了 2 处"},
		{"ok", "func A() {}\n// keep\nfunc B() {}", "// keep", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tool, p := newEditForTest(t, c.content)
			res := tool.Execute(context.Background(), mustJSON(t, editReq{
				Path: p, OldString: c.old, NewString: c.old + "-X", ReplaceAll: c.replace,
			}))
			if c.wantErr != "" {
				if res.Err == nil || !strings.Contains(res.Err.Error(), c.wantErr) {
					t.Fatalf("want err %q, got %v", c.wantErr, res.Err)
				}
				return
			}
			if res.Err != nil {
				t.Fatalf("unexpected err: %v", res.Err)
			}
			bs, _ := os.ReadFile(p)
			if !strings.Contains(string(bs), c.old+"-X") {
				t.Fatalf("replacement missing: %q", string(bs))
			}
			if !strings.Contains(res.Content, "- "+c.old) || !strings.Contains(res.Content, "+ "+c.old+"-X") {
				t.Fatalf("diff missing +/- lines: %q", res.Content)
			}
		})
	}
}

// TestFileEditReplaceAll replace_all 替换全部匹配；未开启时多处匹配必须拒绝（防误伤）。
func TestFileEditReplaceAll(t *testing.T) {
	tool, p := newEditForTest(t, "dup A\ndup B")
	res := tool.Execute(context.Background(), mustJSON(t, editReq{
		Path: p, OldString: "dup", NewString: "new", ReplaceAll: true,
	}))
	if res.Err != nil {
		t.Fatalf("unexpected err: %v", res.Err)
	}
	bs, _ := os.ReadFile(p)
	if string(bs) != "new A\nnew B" {
		t.Fatalf("content = %q", string(bs))
	}
}

// TestUnifiedDiffContext 公共前后缀裁剪：diff 只呈现差异区 + 3 行上下文。
func TestUnifiedDiffContext(t *testing.T) {
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, "line"+string(rune('a'+i%26)))
	}
	before := strings.Join(lines, "\n")
	afterLines := append(append([]string{}, lines[:25]...), "CHANGED")
	afterLines = append(afterLines, lines[25:]...)
	after := strings.Join(afterLines, "\n")
	d := unifiedDiff(before, after)
	if !strings.Contains(d, "+ CHANGED") {
		t.Fatalf("diff missing inserted line: %q", d)
	}
	if strings.Count(d, "\n") > 12 {
		t.Fatalf("diff should be compact (context trimmed), got %d lines", strings.Count(d, "\n"))
	}
}
