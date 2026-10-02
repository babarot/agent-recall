package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/babarot/claude-recall/internal/config"
	"github.com/babarot/claude-recall/internal/db"
	"github.com/babarot/claude-recall/internal/theme"
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

func (f *fakePreview) SessionDetail(id string) (*db.Detail, error) {
	first := db.Message{Role: "user", Content: "first question about " + id, Timestamp: now}
	return &db.Detail{
		You: 3, Claude: 4, Tools: 9,
		TopTools: []db.Count{{Name: "Bash", N: 6}, {Name: "Edit", N: 3}},
		Files:    []db.Count{{Name: "/repo/a.go", N: 2}, {Name: "/repo/b.go", N: 1}}, FileCount: 2,
		Commands: []string{"go test ./...", "git status"},
		Activity: make([]int, 24), Version: "2.1.287",
		First:  &first,
		Tail:   longTail(),
		Hidden: 5,
	}, nil
}

// longTail is more conversation than any frame shows at once, ending with
// the two messages the tests look for.
func longTail() []db.Message {
	var out []db.Message
	for i := range 40 {
		out = append(out, db.Message{Role: "assistant", Content: fmt.Sprintf("older message %02d", i), Timestamp: now})
	}
	return append(out, db.Message{Role: "user", Content: "please also add docs", Timestamp: now},
		db.Message{Role: "assistant", Content: "done, docs added", Timestamp: now})
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
	if got := names(140); got != "Date,Title,Folder,Branch,Msgs,Size,ID" {
		t.Errorf("140: %s", got)
	}
	if got := names(80); got != "Date,Title,Folder" {
		t.Errorf("80: %s", got)
	}
	if got := names(100); got != "Date,Title,Folder,Branch,Msgs" {
		t.Errorf("100: %s", got)
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

func TestRelativeDate(t *testing.T) {
	cases := map[time.Time]string{
		now.Add(-time.Hour):                             "Today 18:00",
		now.Add(-24 * time.Hour):                        "Yesterday",
		now.Add(-3 * 24 * time.Hour):                    "3d ago",
		time.Date(2026, 3, 5, 9, 30, 0, 0, time.Local):  "Mar  5",
		time.Date(2025, 12, 31, 0, 0, 0, 0, time.Local): "2025-12-31",
		{}: "-",
	}
	for in, want := range cases {
		if got := relativeDate(in, now); got != want {
			t.Errorf("relativeDate(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestRemovedWorktree(t *testing.T) {
	home := "/Users/me"
	cases := []struct{ path, repo, name string }{
		{"/Users/me/.herdr/worktrees/dotfiles/worktree-brave-stone-cc30", "dotfiles", "brave-stone-cc30"},
		{"/Users/me/src/github.com/me/app/.claude/worktrees/fix-login", "me/app", "fix-login"},
	}
	for _, c := range cases {
		repo, name, ok := removedWorktree(c.path, home)
		if !ok || repo != c.repo || name != c.name {
			t.Errorf("removedWorktree(%s) = %q %q %v", c.path, repo, name, ok)
		}
	}
	if _, _, ok := removedWorktree("/Users/me/src/github.com/me/app", home); ok {
		t.Error("a plain folder is not a worktree")
	}
}

func TestHeaderKeepsCountsAndSort(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 120, 30)
	first := strings.Split(screen(m), "\n")[0]
	if !strings.Contains(first, "recall // claude-recall") || !strings.Contains(first, "3 / 3 sessions · sort: Ended") {
		t.Fatalf("header %q", first)
	}
}

func TestEveryThemeRenders(t *testing.T) {
	for _, name := range theme.Names() {
		cfg := config.Default().TUI
		cfg.Theme = name
		m, _ := newTestModel(t, cfg, 120, 30)
		if !strings.Contains(screen(m), "sessions") {
			t.Errorf("%s: nothing rendered", name)
		}
	}
}

func TestDetailPaneShowsThreeFrames(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 40)
	s := screen(m)
	for _, want := range []string{"Conversation", "What was done", "Details", "please also add docs", "done, docs added",
		"Bash 6", "a.go", "Claude Code 2.1.287", "WHEN", "HOW MUCH", "WHERE", "FILES", "bbbbbbbb-2222"} {
		if !strings.Contains(s, want) {
			t.Errorf("pane lacks %q:\n%s", want, s)
		}
	}
	// Commands show as how often each ran, not one by one.
	for _, want := range []string{"COMMANDS", "go test ▇▇▇▇▇▇ 1", "git status ▇▇▇▇▇▇ 1"} {
		if !strings.Contains(s, want) {
			t.Errorf("What was done lacks %q:\n%s", want, s)
		}
	}
}

func TestResizeDetailPane(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 120, 40)
	h := m.paneHeight()
	m = press(t, m, "+")
	if m.paneHeight() != h+2 {
		t.Fatalf("+ gave %d, want %d", m.paneHeight(), h+2)
	}
	for range 20 {
		m = press(t, m, "-")
	}
	if m.paneHeight() != config.MinDetailHeight {
		t.Fatalf("shrunk to %d, want the minimum %d", m.paneHeight(), config.MinDetailHeight)
	}
	for range 40 {
		m = press(t, m, "+")
	}
	if m.listHeight() < minListRows {
		t.Fatalf("the list kept %d rows", m.listHeight())
	}
}

func TestDragDetailPane(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 120, 40)
	top := m.paneTop()
	m = update(t, m, tea.MouseClickMsg{X: 10, Y: top, Button: tea.MouseLeft})
	m = update(t, m, tea.MouseMotionMsg{X: 10, Y: top - 5, Button: tea.MouseLeft})
	m = update(t, m, tea.MouseReleaseMsg{X: 10, Y: top - 5, Button: tea.MouseLeft})
	if m.paneTop() != top-5 || m.dragging {
		t.Fatalf("pane top %d, want %d (dragging %v)", m.paneTop(), top-5, m.dragging)
	}
}

func TestDetailHeightIsRemembered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	m, _ := newTestModel(t, config.Default().TUI, 120, 40)
	m = m.RememberIn(path)
	next, cmd := m.Update(tea.KeyPressMsg{Code: '+', Text: "+"})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("resizing should save the state")
	}
	cmd()
	if got := config.LoadState(path).DetailHeight; got != m.detailH {
		t.Fatalf("saved %d, want %d", got, m.detailH)
	}
	again := New(testSessions(t), &fakePreview{}, config.Default().TUI).RememberIn(path)
	if again.detailH != m.detailH {
		t.Fatalf("restored %d, want %d", again.detailH, m.detailH)
	}
}

func TestSplit(t *testing.T) {
	if a, b := split(20, 8, 6); a != 14 || b != 6 {
		t.Errorf("roomy split %d %d", a, b)
	}
	if a, b := split(16, 30, 30); a+b != 16 || b < minFrame || a < b {
		t.Errorf("tight split %d %d", a, b)
	}
}

func TestClickSelectsRow(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 120, 40)
	m = update(t, m, tea.MouseClickMsg{X: 20, Y: m.listTop() + 2, Button: tea.MouseLeft})
	if m.cursor != 2 || m.focus != focusList {
		t.Fatalf("cursor %d focus %v", m.cursor, m.focus)
	}
}

func TestClickFocusesFrameAndWheelScrollsIt(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 120, 40)
	rects, _ := m.paneRects()
	conv := rects[focusConv]
	m = update(t, m, tea.MouseClickMsg{X: conv.x + 3, Y: conv.y + 2, Button: tea.MouseLeft})
	if m.focus != focusConv {
		t.Fatalf("focus %v", m.focus)
	}
	if !strings.Contains(screen(m), "done, docs added") {
		t.Fatal("conversation should start at its newest message")
	}
	m = update(t, m, tea.MouseWheelMsg{X: conv.x + 3, Y: conv.y + 2, Button: tea.MouseWheelUp})
	if m.scroll[focusConv] != 3 {
		t.Fatalf("wheel up scrolled to %d", m.scroll[focusConv])
	}
	m = press(t, m, "g")
	if !strings.Contains(screen(m), "older message 00") {
		t.Fatalf("g should show the oldest messages:\n%s", screen(m))
	}
	m = press(t, m, "G")
	if m.scroll[focusConv] != 0 {
		t.Fatalf("G should return to the newest, offset %d", m.scroll[focusConv])
	}
	m = press(t, m, "esc")
	if m.focus != focusList {
		t.Fatalf("esc should return to the list, focus %v", m.focus)
	}
}

func TestWheelOverListMovesSelection(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 120, 40)
	m = update(t, m, tea.MouseWheelMsg{X: 10, Y: m.listTop(), Button: tea.MouseWheelDown})
	if m.cursor != 1 {
		t.Fatalf("cursor %d", m.cursor)
	}
}

func TestBracketsCycleFocusAndJKScroll(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 120, 40)
	m = press(t, m, "]", "]")
	if m.focus != focusDone {
		t.Fatalf("focus %v", m.focus)
	}
	m = press(t, m, "[", "[", "[")
	if m.focus != focusDetails {
		t.Fatalf("focus %v", m.focus)
	}
	m = press(t, m, "]") // back to the list
	before := m.cursor
	m = press(t, m, "]", "k", "k")
	if m.focus != focusConv || m.cursor != before || m.scroll[focusConv] != 2 {
		t.Fatalf("focus %v cursor %d scroll %d", m.focus, m.cursor, m.scroll[focusConv])
	}
}

func TestScrollResetsOnNewSelection(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 120, 40)
	m = press(t, m, "]", "k", "esc", "j")
	if m.scroll[focusConv] != 0 {
		t.Fatalf("scroll kept across sessions: %d", m.scroll[focusConv])
	}
}

func TestDragFromRowCountLine(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 120, 40)
	top := m.paneTop()
	m = update(t, m, tea.MouseClickMsg{X: 5, Y: top - 1, Button: tea.MouseLeft})
	m = update(t, m, tea.MouseMotionMsg{X: 5, Y: top - 4, Button: tea.MouseLeft})
	m = update(t, m, tea.MouseReleaseMsg{X: 5, Y: top - 4, Button: tea.MouseLeft})
	if m.paneTop() != top-4 {
		t.Fatalf("pane top %d, want %d", m.paneTop(), top-4)
	}
}

func TestRightLayoutFramesAndClicks(t *testing.T) {
	cfg := config.Default().TUI
	cfg.DetailPosition = config.DetailRight
	m, _ := newTestModel(t, cfg, 170, 40)
	rects, ok := m.paneRects()
	if !ok || rects[focusDetails].x < m.listWidth() {
		t.Fatalf("rects %+v", rects)
	}
	m = update(t, m, tea.MouseClickMsg{X: rects[focusDone].x + 2, Y: rects[focusDone].y + 1, Button: tea.MouseLeft})
	if m.focus != focusDone {
		t.Fatalf("focus %v", m.focus)
	}
}

func TestConversationKeepsLastUserMessage(t *testing.T) {
	src := &fakePreview{}
	m := New(testSessions(t), src, config.Default().TUI)
	m.now = func() time.Time { return now }
	m = update(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	r := m.current()
	d := m.details[r.s.ID]
	// Claude spoke last, many times: the user's last message is far above.
	for i := range 10 {
		d.Tail = append(d.Tail, db.Message{Role: "assistant", Content: fmt.Sprintf("later reply %d", i), Timestamp: now})
	}
	s := screen(m)
	if !strings.Contains(s, "please also add docs") || !strings.Contains(s, "later reply 9") {
		t.Fatalf("pane should keep the last user message and the newest reply:\n%s", s)
	}
}

// manyFiles makes the fake session edit more files than fit, in two places.
func manyFiles(m Model) {
	d := m.details[m.current().s.ID]
	d.Files = nil
	for i := range 30 {
		d.Files = append(d.Files, db.Count{Name: fmt.Sprintf("/repo/pkg/file%02d.go", i), N: 1})
	}
	d.Files = append(d.Files, db.Count{Name: "/private/tmp/scratch.py", N: 1})
	d.FileCount = len(d.Files)
	for i := range 8 {
		d.Commands = append(d.Commands, fmt.Sprintf("make step%d", i))
	}
}

func TestCommandsStayPinnedWithManyFiles(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 40)
	manyFiles(m)
	s := screen(m)
	for _, want := range []string{"COMMANDS", "make step", "FILES", "file00.go"} {
		if !strings.Contains(s, want) {
			t.Errorf("pane lacks %q:\n%s", want, s)
		}
	}
}

func TestConversationMarksTheGap(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 40)
	s := screen(m)
	if !strings.Contains(s, "⋮") {
		t.Fatalf("a marker should separate the first request from the latest messages:\n%s", s)
	}
	rects, _ := m.paneRects()
	conv := rects[focusConv]
	m = update(t, m, tea.MouseClickMsg{X: conv.x + 3, Y: conv.y + 2, Button: tea.MouseLeft})
	m = press(t, m, "g")
	// Scrolled to the top, only the 5 messages not loaded remain hidden.
	if s = screen(m); !strings.Contains(s, "⋮    5 messages") || !strings.Contains(s, "older message 00") {
		t.Fatalf("at the top the marker counts what was not loaded:\n%s", s)
	}
}

func TestPreviewBoldsTheUser(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 30)
	m = press(t, m, "space")
	out := m.render()
	bold := func(text string) bool {
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(ansi.Strip(l), text) {
				// The style right before the text decides.
				i := strings.Index(l, text)
				esc := l[strings.LastIndex(l[:i], "\x1b["):i]
				return strings.HasPrefix(esc, "\x1b[1;") || strings.HasPrefix(esc, "\x1b[1m")
			}
		}
		t.Fatalf("%q not in preview", text)
		return false
	}
	if !bold("first question about") {
		t.Error("the user's message should be bold")
	}
	if bold("last answer") {
		t.Error("Claude's message should not be bold")
	}
}

func TestPreviewBoxesUserAndRailsClaude(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 100, 30)
	m = press(t, m, "space")
	lines := strings.Split(screen(m), "\n")
	var boxTop, boxBody, rail bool
	for _, l := range lines {
		switch {
		case strings.Contains(l, "╭─ you"):
			boxTop = true
		case strings.Contains(l, "│ first question about"):
			boxBody = true
		case strings.Contains(l, "▎ last answer"):
			rail = true
		}
		if w := ansi.StringWidth(l); w > 100 {
			t.Errorf("line %d wide: %q", w, l)
		}
	}
	if !boxTop || !boxBody || !rail {
		t.Fatalf("box top %v, box body %v, rail %v:\n%s", boxTop, boxBody, rail, strings.Join(lines, "\n"))
	}
}

func TestWrapTextKeepsListIndent(t *testing.T) {
	got := wrapText("intro\n   - a list item that is long enough to wrap onto the next line", 30)
	if len(got) < 3 || got[0] != "intro" || !strings.HasPrefix(got[2], "     ") {
		t.Fatalf("got %q", got)
	}
	for _, l := range got {
		if ansi.StringWidth(l) > 30 {
			t.Errorf("too wide: %q", l)
		}
	}
}

func TestWrapStaysWithinWidth(t *testing.T) {
	s := "だけして push していないうちに、仕事用の Mac が push したケースです。main が分岐しているので、1 の `pull --ff-only` が失敗します。"
	for _, w := range []int{20, 37, 50, 102, 104, 106} {
		for _, l := range wrap(s, w) {
			if ansi.StringWidth(l) > w {
				t.Errorf("w=%d: %d wide: %q", w, ansi.StringWidth(l), l)
			}
		}
	}
}
