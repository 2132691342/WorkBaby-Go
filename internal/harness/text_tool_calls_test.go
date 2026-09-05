package harness

import (
	"testing"

	"WorkBaby/internal/llm"
)

var textCallDefs = []llm.ToolDefinition{{Name: "file_read"}, {Name: "file_write"}}

func TestParseTextToolCallsFenced(t *testing.T) {
	got := parseTextToolCalls("好的，我先读文件：\n```json\n{\"name\":\"file_read\",\"input\":{\"path\":\"a.txt\"}}\n```", textCallDefs)
	if len(got) != 1 || got[0].Name != "file_read" || string(got[0].Arguments) != `{"path":"a.txt"}` {
		t.Fatalf("fenced json not parsed: %+v", got)
	}
}

func TestParseTextToolCallsXML(t *testing.T) {
	got := parseTextToolCalls(`<tool_call name="file_write">{"input":{"path":"b.md","content":"hi"}}</tool_call>`, textCallDefs)
	if len(got) != 1 || got[0].Name != "file_write" {
		t.Fatalf("xml tag not parsed: %+v", got)
	}
	// 标签内自带 name 字段的形态
	got2 := parseTextToolCalls(`<tool_call>{"name":"file_read","arguments":{"path":"c.txt"}}</tool_call>`, textCallDefs)
	if len(got2) != 1 || got2[0].Name != "file_read" || string(got2[0].Arguments) != `{"path":"c.txt"}` {
		t.Fatalf("xml inner name not parsed: %+v", got2)
	}
}

func TestParseTextToolCallsBare(t *testing.T) {
	got := parseTextToolCalls(`{"tool":"file_read","arguments":{"path":"d.txt"}}`, textCallDefs)
	if len(got) != 1 || got[0].Name != "file_read" {
		t.Fatalf("bare json not parsed: %+v", got)
	}
}

func TestParseTextToolCallsMultiple(t *testing.T) {
	body := `<tool_call name="file_read">{"input":{"path":"x"}}</tool_call>
<tool_call name="file_write">{"input":{"path":"y","content":"z"}}</tool_call>`
	got := parseTextToolCalls(body, textCallDefs)
	if len(got) != 2 {
		t.Fatalf("expected 2 calls, got %d: %+v", len(got), got)
	}
	if got[0].ID == got[1].ID {
		t.Fatalf("duplicate ids: %s", got[0].ID)
	}
}

// 安全：白名单外的名字、普通 JSON 数据回答、空正文都不得触发调用。
func TestParseTextToolCallsNoFalsePositive(t *testing.T) {
	cases := []string{
		"",
		"这是普通回答，没有调用。",
		"```json\n{\"foo\":1,\"bar\":[1,2]}\n```",
		`{"name":"rm_rf","input":{"/":""}}`,
		"```json\n{\"path\":\"a.txt\"}\n```",
	}
	for _, c := range cases {
		if got := parseTextToolCalls(c, textCallDefs); len(got) != 0 {
			t.Fatalf("false positive on %q: %+v", c, got)
		}
	}
}

func TestParseTextToolCallsNoDefs(t *testing.T) {
	if got := parseTextToolCalls(`{"name":"file_read"}`, nil); len(got) != 0 {
		t.Fatalf("must not parse when no tools exposed: %+v", got)
	}
}
