package tui

import (
	"path/filepath"
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
	m = update(t, m, detailLoaded{id, d})
	s := screen(m)
	if m.detailLoading[id] || strings.Contains(s, "Loading…") || !strings.Contains(s, "first question about "+id) {
		t.Fatalf("after it came:\n%s", s)
	}
	// A detail that comes for another session is kept for later.
	m = update(t, m, detailLoaded{"other", d})
	if _, ok := m.details["other"]; !ok || !strings.Contains(screen(m), "first question about "+id) {
		t.Fatal("another session's detail should be kept and not shown")
	}
}
