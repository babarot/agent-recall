package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/babarot/claude-recall/internal/config"
	"github.com/babarot/claude-recall/internal/db"
)

var now = time.Date(2026, 10, 2, 19, 0, 0, 0, time.Local)

type fakePreview struct{ calls []string }

func (f *fakePreview) SessionPreview(id string, head, tail int) (db.Preview, error) {
	f.calls = append(f.calls, id)
	return db.Preview{
		Head:    []db.Message{{Role: "user", Content: "first question about " + id, Timestamp: now}},
		Tail:    []db.Message{{Role: "assistant", Content: "last answer", Timestamp: now}},
		Skipped: 7,
	}, nil
}

func testSessions(t *testing.T) []db.Session {
	t.Helper()
	dir := t.TempDir()
	return []db.Session{
		{ID: "aaaaaaaa-1111", ProjectPath: dir, GitBranch: "main", FirstPrompt: "fix the login bug",
			MessageCount: 10, FileSize: 2048, StartedAt: now.Add(-3 * time.Hour), EndedAt: now.Add(-2 * time.Hour)},
		{ID: "bbbbbbbb-2222", ProjectPath: filepath.Join(dir, "gone"), GitBranch: "feature", FirstPrompt: "write the docs",
			MessageCount: 50, FileSize: 4 << 20, StartedAt: now.Add(-30 * time.Hour), EndedAt: now.Add(-1 * time.Hour)},
		{ID: "cccccccc-3333", ProjectPath: dir, GitBranch: "main", Title: "Refactor the parser",
			MessageCount: 5, FileSize: 100, StartedAt: now.Add(-5 * time.Hour), EndedAt: now.Add(-4 * time.Hour)},
	}
}

func newTestModel(t *testing.T, cfg config.TUI, w, h int) (Model, *fakePreview) {
	t.Helper()
	src := &fakePreview{}
	m := New(testSessions(t), src, cfg)
	m.now = func() time.Time { return now }
	return update(t, m, tea.WindowSizeMsg{Width: w, Height: h}), src
}

func update(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, _ := m.Update(msg)
	return next.(Model)
}

func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		case "space":
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		default:
			r := []rune(k)[0]
			msg = tea.KeyPressMsg{Code: r, Text: k}
		}
		m = update(t, m, msg)
	}
	return m
}

func screen(m Model) string { return ansi.Strip(m.render()) }

func TestListIsSortedByEndedFirst(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 30)
	if got := m.current().s.ID; got != "bbbbbbbb-2222" {
		t.Fatalf("first row %s, want the session that ended last", got)
	}
}

func TestSortCycles(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 30)
	m = press(t, m, "s", "s") // Ended -> Started -> Msgs
	if sorts[m.sortIdx].name != "Msgs" || m.current().s.MessageCount != 50 {
		t.Fatalf("sort %s, first %+v", sorts[m.sortIdx].name, m.current().s)
	}
}

func TestFilterMatchesEveryWord(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 30)
	m = press(t, m, "/", "p", "a", "r", "s", "e", "r")
	if len(m.visible) != 1 || m.current().s.ID != "cccccccc-3333" {
		t.Fatalf("visible %v", m.visible)
	}
	m = press(t, m, "esc")
	if len(m.visible) != 3 || m.mode != modeList {
		t.Fatalf("esc should clear the filter, visible %v", m.visible)
	}
}

func TestEnterResumesSelectedSession(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 30)
	m = press(t, m, "down", "enter") // second row: aaaaaaaa, whose folder exists
	if m.Result == nil || m.Result.SessionID != "aaaaaaaa-1111" {
		t.Fatalf("result %+v", m.Result)
	}
}

func TestEnterRefusesMissingFolder(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 30)
	m = press(t, m, "enter") // first row: bbbbbbbb, whose folder is gone
	if m.Result != nil || !strings.Contains(m.toast, "no longer exists") {
		t.Fatalf("result %+v toast %q", m.Result, m.toast)
	}
}

func TestCopyID(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 30)
	m = press(t, m, "y")
	if !strings.Contains(m.toast, "bbbbbbbb-2222") {
		t.Fatalf("toast %q", m.toast)
	}
}

func TestPreviewOpensAndCloses(t *testing.T) {
	m, src := newTestModel(t, config.Default().TUI, 140, 30)
	m = press(t, m, "space")
	if m.mode != modePreview || len(src.calls) != 1 {
		t.Fatalf("mode %v calls %v", m.mode, src.calls)
	}
	s := screen(m)
	for _, want := range []string{"first question about bbbbbbbb-2222", "7 messages skipped", "last answer"} {
		if !strings.Contains(s, want) {
			t.Errorf("preview lacks %q:\n%s", want, s)
		}
	}
	if m = press(t, m, "space"); m.mode != modeList {
		t.Fatalf("space should close the preview")
	}
}

func TestDetailPosition(t *testing.T) {
	cases := []struct {
		pos   string
		width int
		right bool
	}{
		{config.DetailBottom, 200, false},
		{config.DetailRight, 140, true},
		{config.DetailRight, 90, false}, // too narrow for a side pane
		{config.DetailAuto, 159, false},
		{config.DetailAuto, 160, true},
	}
	for _, c := range cases {
		cfg := config.Default().TUI
		cfg.DetailPosition = c.pos
		m, _ := newTestModel(t, cfg, c.width, 30)
		if got := m.detailRight(); got != c.right {
			t.Errorf("%s at %d: right=%v, want %v", c.pos, c.width, got, c.right)
		}
	}
}

func TestEveryLineFitsTheTerminal(t *testing.T) {
	for _, pos := range []string{config.DetailBottom, config.DetailRight} {
		for _, w := range []int{60, 80, 120, 200} {
			cfg := config.Default().TUI
			cfg.DetailPosition = pos
			m, _ := newTestModel(t, cfg, w, 24)
			lines := strings.Split(m.render(), "\n")
			if len(lines) > 24 {
				t.Errorf("%s %d: %d lines, want at most 24", pos, w, len(lines))
			}
			for i, l := range lines {
				if got := ansi.StringWidth(l); got > w {
					t.Errorf("%s %d: line %d is %d wide: %q", pos, w, i, got, ansi.Strip(l))
				}
			}
		}
	}
}

func TestColumnsAdaptToWidth(t *testing.T) {
	names := func(w int) string {
		var out []string
		for _, p := range layoutColumns(w) {
			out = append(out, p.col.header)
		}
		return strings.Join(out, ",")
	}
	if got := names(140); got != "Ended,Title,Folder,Branch,Msgs,Size,ID" {
		t.Errorf("140: %s", got)
	}
	if got := names(80); got != "Age,Title,Folder,Msgs,ID" {
		t.Errorf("80: %s", got)
	}
}

func TestCleanPrompt(t *testing.T) {
	cases := map[string]string{
		"fix the bug": "fix the bug",
		"<command-message>commit</command-message> <command-name>/commit</command-name> <command-args>staged only</command-args>": "/commit staged only",
		"<command-name>/clear</command-name>":                         "/clear",
		"<bash-input>git status</bash-input>":                         "! git status",
		"<pasted_content id=\"x\"> # Handoff\nnotes</pasted_content>": "# Handoff notes",
		"": "(no prompt)",
	}
	for in, want := range cases {
		if got := cleanPrompt(in); got != want {
			t.Errorf("cleanPrompt(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShortPath(t *testing.T) {
	home := "/Users/me"
	cases := map[string]string{
		"/Users/me/src/github.com/me/repo": "me/repo",
		"/Users/me/.herdr/worktrees/x":     "~/.herdr/worktrees/x",
		"/Users/me":                        "~",
		"/opt/work":                        "/opt/work",
	}
	for in, want := range cases {
		if got := shortPath(in, home); got != want {
			t.Errorf("shortPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatEnded(t *testing.T) {
	if got := formatEnded(now.Add(-time.Hour), now); got != "18:00" {
		t.Errorf("today: %s", got)
	}
	if got := formatEnded(time.Date(2026, 3, 5, 9, 30, 0, 0, time.Local), now); got != "03/05 09:30" {
		t.Errorf("this year: %s", got)
	}
	if got := formatEnded(time.Date(2025, 12, 31, 0, 0, 0, 0, time.Local), now); got != "2025/12/31" {
		t.Errorf("last year: %s", got)
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("/Users/me/src/repo"); got != "/Users/me/src/repo" {
		t.Errorf("plain path quoted: %s", got)
	}
	if got := shellQuote("/tmp/my dir/it's"); got != `'/tmp/my dir/it'\''s'` {
		t.Errorf("got %s", got)
	}
}

func TestMain(m *testing.M) {
	os.Setenv("HOME", "/nonexistent-home")
	os.Exit(m.Run())
}
