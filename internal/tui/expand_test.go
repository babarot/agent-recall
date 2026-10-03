package tui

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/babarot/claude-recall/internal/config"
)

func TestSpaceSpreadsTheConversation(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 40)
	h := m.paneHeight()
	m = press(t, m, "space")
	if !m.expanded || m.focus != focusConv || m.listHeight() != defaultExpandRows {
		t.Fatalf("expanded %v focus %v list rows %d", m.expanded, m.focus, m.listHeight())
	}
	s := screen(m)
	for _, want := range []string{"Conversation", "write the docs", "first question about bbbbbbbb-2222", "5 messages skipped", "older message 00"} {
		if !strings.Contains(s, want) {
			t.Errorf("lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "What was done") || strings.Contains(s, "Details") {
		t.Errorf("Conversation should cover the pane:\n%s", s)
	}
	// It reads from the top; the end is a scroll away.
	m = press(t, m, "G")
	if s = screen(m); !strings.Contains(s, "done, docs added") {
		t.Fatalf("G should reach the end:\n%s", s)
	}
	// Space or Esc puts the pane back.
	for _, k := range []string{"space", "esc"} {
		if !m.expanded {
			m = press(t, m, "space")
		}
		if m = press(t, m, k); m.expanded || m.paneHeight() != h || m.focus != focusList {
			t.Errorf("%s: expanded %v pane %d focus %v", k, m.expanded, m.paneHeight(), m.focus)
		}
	}
}

func TestSpreadConversationTakesTheFocusMarks(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 40)
	line := func(m Model, i int) string { return strings.Split(m.render(), "\n")[i] }
	accent := func(m Model) string { return m.st.id.Render(strings.Repeat("╌", m.listWidth())) }
	selBar := func(m Model) bool { return strings.Contains(ansi.Strip(line(m, m.listTop())), "▎") }

	// The Conversation focused: the list's rule stays plain and its row
	// loses the bar.
	m = press(t, m, "space")
	if strings.Contains(line(m, 1), accent(m)) || selBar(m) {
		t.Errorf("conversation focused, rule %q bar %v", line(m, 1), selBar(m))
	}
	// The list focused: the other way round.
	m = press(t, m, "tab")
	if !strings.Contains(line(m, 1), accent(m)) || !selBar(m) {
		t.Errorf("list focused, rule %q bar %v", line(m, 1), selBar(m))
	}
}

// quit closes the spread Conversation, as it closes the key list, and
// quits only from the list.
func TestQuitClosesTheSpreadConversation(t *testing.T) {
	quits := func(m Model, k string) (Model, bool) {
		t.Helper()
		next, cmd := m.Update(tea.KeyPressMsg{Code: []rune(k)[0], Text: k})
		if cmd == nil {
			return next.(Model), false
		}
		_, ok := cmd().(tea.QuitMsg)
		return next.(Model), ok
	}
	m, _ := newTestModel(t, config.Default().TUI, 140, 40)
	m, q := quits(press(t, m, "space"), "q")
	if q || m.expanded || m.focus != focusList {
		t.Fatalf("q on the Conversation: quit %v expanded %v focus %v", q, m.expanded, m.focus)
	}
	// A search left in it does not take a second q.
	m = settle(t, typeText(t, press(t, m, "space", "/"), "older"))
	m, q = quits(press(t, m, "enter"), "q")
	if q || m.expanded || m.conv.input.Value() != "" {
		t.Fatalf("q with a search: quit %v expanded %v search %q", q, m.expanded, m.conv.input.Value())
	}
	// From the list, still spread, q quits.
	if _, q = quits(press(t, m, "space", "tab"), "q"); !q {
		t.Fatal("q on the list should quit")
	}
	// It follows the quit key.
	m, _ = newTestModel(t, config.Default().TUI, 140, 40)
	m, err := m.WithKeys(config.Keys{"quit": config.KeyList{Keys: []string{"x"}}})
	if err != nil {
		t.Fatal(err)
	}
	if m, q = quits(press(t, m, "space"), "x"); q || m.expanded {
		t.Fatalf("x as quit: quit %v expanded %v", q, m.expanded)
	}
}

func TestReadTheNextSessionInPlace(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 40)
	m = press(t, m, "space", "tab")
	if m.focus != focusList || !m.expanded {
		t.Fatalf("tab should go to the list and keep the spread pane: focus %v", m.focus)
	}
	m = press(t, m, "j")
	if !strings.Contains(screen(m), "first question about aaaaaaaa-1111") {
		t.Fatalf("j should show the next session's conversation:\n%s", screen(m))
	}
	if m = press(t, m, "tab"); m.focus != focusConv {
		t.Fatalf("tab again goes back to the conversation: %v", m.focus)
	}
}

func TestSpreadPaneResizes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	m, _ := newTestModel(t, config.Default().TUI, 140, 40)
	m = m.RememberIn(path)
	m = press(t, m, "space")
	// Drag the top edge down: more list rows.
	top := m.paneTop()
	m = update(t, m, tea.MouseClickMsg{X: 10, Y: top, Button: tea.MouseLeft})
	m = update(t, m, tea.MouseMotionMsg{X: 10, Y: top + 4})
	next, cmd := m.Update(tea.MouseReleaseMsg{X: 10, Y: top + 4})
	m = next.(Model)
	if cmd != nil {
		cmd()
	}
	if m.listHeight() != defaultExpandRows+4 || !m.expanded {
		t.Fatalf("dragged 4 down: %d list rows", m.listHeight())
	}
	if got := config.LoadState(path).ExpandRows; got != defaultExpandRows+4 {
		t.Fatalf("saved %d rows", got)
	}
	// Neither side goes below its smallest: the list keeps 3 rows, the pane 10.
	for range 30 {
		m = press(t, m, "+")
	}
	if m.listHeight() != minListRows {
		t.Fatalf("grown to the most: %d list rows", m.listHeight())
	}
	for range 30 {
		m = press(t, m, "-")
	}
	if m.paneHeight() != config.MinDetailHeight {
		t.Fatalf("shrunk to the least: pane %d", m.paneHeight())
	}
	// The detail pane's own height is untouched.
	h := m.detailH
	if m = press(t, m, "space"); m.detailH != h || m.paneHeight() != min(h, m.height-m.chromeLines()-minListRows) {
		t.Fatal("closing should restore the pane's height")
	}
	// The rows are remembered.
	again := New(testSessions(t), &fakePreview{}, config.Default().TUI).RememberIn(path)
	if again.expandRows != defaultExpandRows+4 {
		t.Fatalf("remembered %d, want %d", again.expandRows, defaultExpandRows+4)
	}
}

func TestSpreadFitsTheTerminal(t *testing.T) {
	for _, pos := range []string{config.DetailBottom, config.DetailRight} {
		for _, w := range []int{60, 100, 200} {
			cfg := config.Default().TUI
			cfg.DetailPosition = pos
			m, _ := newTestModel(t, cfg, w, 24)
			m = press(t, m, "space")
			lines := strings.Split(m.render(), "\n")
			if len(lines) > 24 {
				t.Errorf("%s %d: %d lines", pos, w, len(lines))
			}
			for i, l := range lines {
				if got := ansi.StringWidth(l); got > w {
					t.Errorf("%s %d: line %d is %d wide", pos, w, i, got)
				}
			}
		}
	}
}

// In the background, moving onto a session does not wait for its detail:
// the pane says it is loading until the detail comes, the spread
// conversation too.
func TestDetailLoadsInBackground(t *testing.T) {
	src := &fakePreview{}
	m := New(testSessions(t), src, config.Default().TUI).LoadInBackground()
	m = update(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	id := m.current().s.ID
	if len(m.details) != 0 || !m.detailLoading[id] || !strings.Contains(screen(m), "Loading…") {
		t.Fatalf("details %v loading %v:\n%s", m.details, m.detailLoading, screen(m))
	}
	// Spread before it comes: loading there too, then the conversation.
	m = press(t, m, "space")
	if !strings.Contains(screen(m), "Loading…") {
		t.Fatalf("the spread pane should say it is loading:\n%s", screen(m))
	}
	d, _ := src.SessionDetail(id)
	m = update(t, m, detailLoaded{id: id, d: d})
	s := screen(m)
	if m.detailLoading[id] || strings.Contains(s, "Loading…") || !strings.Contains(s, "first question about "+id) {
		t.Fatalf("after it came:\n%s", s)
	}
	// A detail that comes for another session is kept for later.
	m = update(t, m, detailLoaded{id: "other", d: d})
	if _, ok := m.details["other"]; !ok || !strings.Contains(screen(m), "first question about "+id) {
		t.Fatal("another session's detail should be kept and not shown")
	}
}

// Commands are counted by program where the detail is read, once, and the
// pane shows those counts.
func TestDetailCountsCommandsWhereItIsRead(t *testing.T) {
	src := &fakePreview{}
	m := New(testSessions(t), src, config.Default().TUI).LoadInBackground()
	m = update(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	id := m.current().s.ID
	delete(m.detailLoading, id)
	msg, ok := m.loadDetail()().(detailLoaded)
	if !ok || msg.id != id || len(msg.programs) == 0 {
		t.Fatalf("got %+v", msg)
	}
	m = update(t, m, msg)
	if got, want := m.programs[id], commandCounts(m.details[id].Commands); !slices.Equal(got, want) {
		t.Fatalf("programs %v, want %v", got, want)
	}
	if !regexp.MustCompile(`git status +▇+ +1`).MatchString(screen(m)) {
		t.Fatalf("What was done lacks the counted commands:\n%s", screen(m))
	}
}

// Reading in place from the list, esc steps back: the filter first, then
// the spread conversation.
func TestEscFromTheListPutsThePaneBack(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 40)
	m = press(t, m, "/")
	m = typeText(t, m, "docs")
	m = press(t, m, "enter", "space", "tab")
	if !m.expanded || m.focus != focusList || m.filter.Value() == "" {
		t.Fatalf("expanded %v focus %v filter %q", m.expanded, m.focus, m.filter.Value())
	}
	if m = press(t, m, "esc"); !m.expanded || m.filter.Value() != "" {
		t.Fatalf("the first esc clears the filter: expanded %v filter %q", m.expanded, m.filter.Value())
	}
	if m = press(t, m, "esc"); m.expanded {
		t.Fatal("the second esc puts the pane back")
	}
}
