package parser

import (
	"encoding/json"
	"strings"
	"testing"
)

func line(v map[string]any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func userMsg(text, uuid, ts string) string {
	return line(map[string]any{
		"type": "user", "uuid": uuid, "sessionId": "sess-001", "timestamp": ts,
		"cwd": "/home/user/project", "version": "2.1.87", "gitBranch": "main", "isSidechain": false,
		"message": map[string]any{"role": "user", "content": text},
	})
}

func assistantMsg(blocks []any, uuid, ts string) string {
	return line(map[string]any{
		"type": "assistant", "uuid": uuid, "sessionId": "sess-001", "timestamp": ts, "isSidechain": false,
		"message": map[string]any{"role": "assistant", "content": blocks},
	})
}

func metaMsg(text, uuid, ts string) string {
	return line(map[string]any{
		"type": "user", "uuid": uuid, "sessionId": "sess-001", "timestamp": ts, "isMeta": true,
		"message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": text}}},
	})
}

func taskNotification(xml, uuid, ts string) string {
	return line(map[string]any{
		"type": "user", "uuid": uuid, "sessionId": "sess-001", "timestamp": ts,
		"origin":  map[string]any{"kind": "task-notification"},
		"message": map[string]any{"role": "user", "content": xml},
	})
}

func text(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

var (
	systemMsg   = line(map[string]any{"type": "system", "subtype": "turn_duration", "timestamp": "2026-01-01T00:00:05Z", "sessionId": "sess-001"})
	snapshotMsg = line(map[string]any{"type": "file-history-snapshot", "messageId": "msg-1", "snapshot": map[string]any{}})
)

func jsonl(lines ...string) string { return strings.Join(lines, "\n") }

func mustParse(t *testing.T, content string) *Session {
	t.Helper()
	s := Parse(content, "test", nil)
	if s == nil {
		t.Fatal("Parse returned nil")
	}
	return s
}

func TestParseUserAndAssistantText(t *testing.T) {
	s := mustParse(t, jsonl(userMsg("Hello world", "u1", "2026-01-01T00:00:00Z"),
		assistantMsg([]any{text("Hi there!")}, "a1", "2026-01-01T00:00:01Z")))
	if len(s.Messages) != 2 || s.Messages[0].Content != "Hello world" || s.Messages[1].Role != "assistant" || s.Messages[1].Content != "Hi there!" {
		t.Fatalf("%+v", s.Messages)
	}
}

func TestParseMetadata(t *testing.T) {
	s := Parse(userMsg("test", "u1", "2026-01-01T00:00:00Z"), "my-project", nil)
	m := s.Meta
	if m.SessionID != "sess-001" || m.Project != "my-project" || m.ProjectPath != "/home/user/project" ||
		m.GitBranch != "main" || m.ClaudeVersion != "2.1.87" || m.FirstPrompt != "test" {
		t.Fatalf("%+v", m)
	}
}

func TestParseSkipsSystemAndSnapshot(t *testing.T) {
	s := mustParse(t, jsonl(userMsg("hello", "u1", "2026-01-01T00:00:00Z"), systemMsg, snapshotMsg,
		assistantMsg([]any{text("response")}, "a1", "2026-01-01T00:00:02Z")))
	if len(s.Messages) != 2 {
		t.Fatalf("%+v", s.Messages)
	}
}

func TestParseBlockTypes(t *testing.T) {
	s := mustParse(t, jsonl(userMsg("do something", "u1", "2026-01-01T00:00:00Z"),
		assistantMsg([]any{
			map[string]any{"type": "thinking", "thinking": "let me think..."},
			map[string]any{"type": "tool_use", "id": "t1", "name": "Bash", "input": map[string]any{"command": "ls"}},
			text("Done!"),
		}, "a1", "2026-01-01T00:00:01Z")))
	got := []string{}
	for _, m := range s.Messages {
		got = append(got, m.BlockType)
	}
	if strings.Join(got, ",") != "text,thinking,tool_use,text" {
		t.Fatalf("block types %v", got)
	}
	tool := s.Messages[2]
	if *tool.ToolName != "Bash" || tool.Content != "Bash" || *tool.ToolInput != `{"command":"ls"}` {
		t.Fatalf("tool_use %+v", tool)
	}
}

func TestParseToolUseWithoutInput(t *testing.T) {
	s := mustParse(t, assistantMsg([]any{map[string]any{"type": "tool_use", "name": "Read"},
		map[string]any{"type": "tool_use"}}, "a1", "2026-01-01T00:00:00Z")+"\n"+userMsg("x", "u1", "2026-01-01T00:00:01Z"))
	if *s.Messages[0].ToolInput != "" || *s.Messages[1].ToolName != "unknown" {
		t.Fatalf("%+v %+v", s.Messages[0], s.Messages[1])
	}
}

func TestParseSkipsSidechain(t *testing.T) {
	side := line(map[string]any{"type": "user", "uuid": "u2", "sessionId": "sess-001", "timestamp": "2026-01-01T00:00:01Z",
		"isSidechain": true, "message": map[string]any{"role": "user", "content": "sidechain message"}})
	s := mustParse(t, jsonl(userMsg("hello", "u1", "2026-01-01T00:00:00Z"), side,
		assistantMsg([]any{text("reply")}, "a1", "2026-01-01T00:00:02Z")))
	if len(s.Messages) != 2 || s.Messages[1].Content != "reply" {
		t.Fatalf("%+v", s.Messages)
	}
}

func TestParseTurnIndexAndEndedAt(t *testing.T) {
	s := mustParse(t, jsonl(
		userMsg("first", "u1", "2026-01-01T00:00:00Z"),
		assistantMsg([]any{text("reply")}, "a1", "2026-01-01T00:05:00Z"),
		userMsg("second", "u2", "2026-01-01T00:10:00Z"),
		assistantMsg([]any{text("last reply")}, "a2", "2026-01-01T00:15:00Z"),
	))
	for i, m := range s.Messages {
		if m.TurnIndex != i {
			t.Fatalf("message %d has turn %d", i, m.TurnIndex)
		}
	}
	if s.Meta.StartedAt != "2026-01-01T00:00:00Z" || s.Meta.EndedAt != "2026-01-01T00:10:00Z" {
		t.Fatalf("%+v", s.Meta)
	}
}

func TestParseReturnsNil(t *testing.T) {
	for _, c := range []string{"", "\n\n", jsonl(systemMsg, snapshotMsg)} {
		if Parse(c, "test", nil) != nil {
			t.Errorf("Parse(%q) should be nil", c)
		}
	}
}

func TestParseIndexEnrichment(t *testing.T) {
	s := Parse(userMsg("hello", "u1", "2026-01-01T00:00:00Z"), "test", &IndexEntry{
		SessionID: "sess-001", FirstPrompt: "enriched prompt", Summary: "a summary from index",
		GitBranch: "feature-branch", ProjectPath: "/enriched/path",
	})
	if s.Meta.FirstPrompt != "enriched prompt" || *s.Meta.Summary != "a summary from index" ||
		s.Meta.GitBranch != "main" || s.Meta.ProjectPath != "/home/user/project" {
		t.Fatalf("%+v", s.Meta)
	}
}

func TestParseFirstPromptTruncation(t *testing.T) {
	s := mustParse(t, userMsg(strings.Repeat("a", 600), "u1", "2026-01-01T00:00:00Z"))
	if len(s.Meta.FirstPrompt) != 500 {
		t.Fatalf("len %d", len(s.Meta.FirstPrompt))
	}
	// 500 UTF-16 units: 250 emoji, not 500.
	s = mustParse(t, userMsg(strings.Repeat("😀", 300), "u1", "2026-01-01T00:00:00Z"))
	if got := len([]rune(s.Meta.FirstPrompt)); got != 250 {
		t.Fatalf("runes %d", got)
	}
}

func TestParseSkipsMalformedLines(t *testing.T) {
	s := mustParse(t, jsonl("not valid json", userMsg("valid message", "u1", "2026-01-01T00:00:00Z"), "{ broken"))
	if len(s.Messages) != 1 {
		t.Fatalf("%+v", s.Messages)
	}
}

func TestParseImages(t *testing.T) {
	img := func(data string) map[string]any {
		return map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": data}}
	}
	l := line(map[string]any{"type": "user", "uuid": "u1", "sessionId": "sess-001", "timestamp": "2026-01-01T00:00:00Z",
		"message": map[string]any{"role": "user", "content": []any{text("[Image #1] check this"), img("aGVsbG8="), img("d29ybGQ=")}}})
	s := mustParse(t, l)
	if len(s.Images) != 2 || s.Images[1].ImageIndex != 1 || s.Images[0].MediaType != "image/png" || s.Images[0].Data != "aGVsbG8=" {
		t.Fatalf("%+v", s.Images)
	}
	if len(s.Messages) != 1 {
		t.Fatalf("images must not become messages: %+v", s.Messages)
	}
}

func TestParseMetaMessages(t *testing.T) {
	r := parseLines(jsonl(
		metaMsg("Base directory for this skill: /x\n\n# Skill", "m1", "2026-01-01T00:00:00Z"),
		userMsg("actual user question", "u1", "2026-01-01T00:00:01Z"),
		taskNotification("<task-notification>done</task-notification>", "t1", "2026-01-01T00:10:00Z"),
		assistantMsg([]any{text("b")}, "a1", "2026-01-01T00:00:02Z"),
	), 10)
	types := []string{}
	for i, m := range r.messages {
		types = append(types, m.BlockType)
		if m.TurnIndex != 10+i {
			t.Errorf("message %d has turn %d", i, m.TurnIndex)
		}
	}
	if strings.Join(types, ",") != "meta,text,meta,text" {
		t.Fatalf("types %v", types)
	}
	if r.firstUserText != "actual user question" {
		t.Errorf("firstUserText %q", r.firstUserText)
	}
	if r.lastTimestamp != "2026-01-01T00:00:01Z" {
		t.Errorf("meta lines must not advance lastTimestamp: %s", r.lastTimestamp)
	}
	if r.messages[2].Content != "<task-notification>done</task-notification>" {
		t.Errorf("task notification content %q", r.messages[2].Content)
	}
}

func TestParseBlockIndexIsArrayPosition(t *testing.T) {
	r := parseLines(assistantMsg([]any{
		map[string]any{"type": "thinking", "thinking": ""},
		map[string]any{"type": "tool_use", "id": "t1", "name": "Read", "input": map[string]any{}},
		text("Done!"),
	}, "a1", "2026-01-01T00:00:00Z"), 0)
	if len(r.messages) != 2 || r.messages[0].BlockIndex != 1 || r.messages[1].BlockIndex != 2 {
		t.Fatalf("%+v", r.messages)
	}
}

func TestParseToolResult(t *testing.T) {
	long := strings.Repeat("x", 10001)
	r := parseLines(line(map[string]any{"type": "user", "uuid": "u1", "sessionId": "s", "timestamp": "t",
		"message": map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "content": long},
			map[string]any{"type": "tool_result", "content": []any{text("a"), map[string]any{"type": "image"}, text("b")}},
			map[string]any{"type": "tool_result"},
		}}}), 0)
	if len(r.messages) != 3 {
		t.Fatalf("%+v", r.messages)
	}
	if want := strings.Repeat("x", 10000) + "\n... (truncated)"; r.messages[0].Content != want {
		t.Errorf("truncation: %d chars", len(r.messages[0].Content))
	}
	if r.messages[1].Content != "a\nb" || r.messages[2].Content != "" {
		t.Errorf("%q %q", r.messages[1].Content, r.messages[2].Content)
	}
}

func TestParseTitle(t *testing.T) {
	ai := func(s string) string {
		return line(map[string]any{"type": "ai-title", "aiTitle": s, "sessionId": "sess-001"})
	}
	custom := func(s string) string {
		return line(map[string]any{"type": "custom-title", "customTitle": s, "sessionId": "sess-001"})
	}
	u := userMsg("hello", "u1", "2026-01-01T00:00:00Z")
	cases := []struct {
		lines []string
		want  string
	}{
		{[]string{u}, ""},
		{[]string{u, ai("First"), ai("Second")}, "Second"},
		{[]string{u, custom("renamed"), ai("Later AI title")}, "renamed"},
		{[]string{u, ai(" padded ")}, "padded"},
	}
	for _, c := range cases {
		if got := mustParse(t, jsonl(c.lines...)).Meta.Title; got != c.want {
			t.Errorf("title %q, want %q", got, c.want)
		}
	}
}
