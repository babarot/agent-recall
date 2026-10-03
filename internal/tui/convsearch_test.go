package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/babarot/claude-recall/internal/config"
	"github.com/babarot/claude-recall/internal/db"
)

// typeText types s into whatever has the keys.
func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m = update(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

func TestConversationSearch(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 40)
	m = press(t, m, "space", "/")
	if !m.conv.typing || m.mode == modeFilter {
		t.Fatalf("/ in the spread conversation should search it, not filter the list")
	}
	// Typing goes to the first hit and shows which it is.
	m = typeText(t, m, "Message 05")
	s := screen(m)
	if len(m.conv.hits) != 1 || !strings.Contains(s, "older message 05") || strings.Contains(s, "older message 00") || !strings.Contains(s, "1/1") {
		t.Fatalf("hits %v:\n%s", m.conv.hits, s)
	}
	if !strings.Contains(m.View().Content, m.st.matchCur.Render("message 05")) {
		t.Fatalf("the hit should be highlighted")
	}
	// ? is typed, not the key list.
	if m = typeText(t, m, "?"); m.helpOpen || m.conv.input.Value() != "Message 05?" {
		t.Fatalf("? while typing: help %v, value %q", m.helpOpen, m.conv.input.Value())
	}

	// Enter keeps the search; n and N go through the hits, wrapping.
	m.conv.input.SetValue("")
	m = typeText(t, m, "older message 0")
	m = press(t, m, "enter")
	if m.conv.typing || len(m.conv.hits) != 10 || m.conv.cur != 0 {
		t.Fatalf("typing %v hits %d cur %d", m.conv.typing, len(m.conv.hits), m.conv.cur)
	}
	if m = press(t, m, "n", "n"); m.conv.cur != 2 || !strings.Contains(screen(m), "3/10") {
		t.Fatalf("n n: cur %d\n%s", m.conv.cur, screen(m))
	}
	if m = press(t, m, "N", "N", "N"); m.conv.cur != 9 || !strings.Contains(screen(m), "older message 09") {
		t.Fatalf("N past the first wraps to the last: cur %d", m.conv.cur)
	}

	// Esc drops the search and keeps the pane; Esc again closes it.
	if m = press(t, m, "esc"); !m.expanded || m.conv.input.Value() != "" || len(m.conv.hits) != 0 {
		t.Fatalf("esc: expanded %v value %q", m.expanded, m.conv.input.Value())
	}
	if m = press(t, m, "esc"); m.expanded {
		t.Fatal("a second esc closes the pane")
	}
}

func TestConversationSearchNoMatch(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 40)
	m = typeText(t, press(t, m, "space", "/"), "kubernetes")
	if len(m.conv.hits) != 0 || !strings.Contains(screen(m), "no match") {
		t.Fatalf("hits %v:\n%s", m.conv.hits, screen(m))
	}
	// n with no hits does nothing to the search.
	if m = press(t, m, "enter", "n"); m.conv.input.Value() != "kubernetes" {
		t.Fatalf("value %q", m.conv.input.Value())
	}
}

// Elsewhere, / is still the list's filter.
func TestSlashFiltersOutsideTheConversation(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 40)
	if m := press(t, m, "/"); m.mode != modeFilter || m.conv.typing {
		t.Fatal("/ on the list should open the filter")
	}
	if m := press(t, m, "space", "tab", "/"); m.mode != modeFilter || m.conv.typing {
		t.Fatal("/ with the list focused beside the spread pane should open the filter")
	}
}

// The search carries over to the next session read in place, and goes
// when the pane is put back.
func TestConversationSearchCarriesOver(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 40)
	m = press(t, typeText(t, press(t, m, "space", "/"), "first question"), "enter")
	if len(m.conv.hits) != 1 {
		t.Fatalf("hits %v", m.conv.hits)
	}
	m = press(t, m, "tab", "j")
	if len(m.conv.hits) != 1 || !strings.Contains(m.read[m.conv.hits[0]], "aaaaaaaa-1111") {
		t.Fatalf("the next session should be searched too: %v", m.conv.hits)
	}
	if m = press(t, m, "space"); m.conv.input.Value() != "" {
		t.Fatal("closing the pane drops the search")
	}
}

func TestMarkRunes(t *testing.T) {
	style := lipgloss.NewStyle().Reverse(true)
	for _, c := range []struct{ line, q, want string }{
		{"Fix the Login bug, login again", "login", "Login"},
		{"ログインが切れる件、ログインし直し", "ログイン", "ログイン"},
	} {
		out := markRunes(c.line, []rune(c.q), style)
		if ansi.Strip(out) != c.line || strings.Count(out, style.Render(c.want)) < 1 {
			t.Errorf("%q: %q", c.line, out)
		}
	}
}

// The search looks through the whole conversation, the messages the frame
// skips too, shows what has it with the messages around, and Esc puts the
// conversation back as it was.
func TestConversationSearchFindsSkippedMessages(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 40)
	m = press(t, m, "space")
	if s := screen(m); strings.Contains(s, "middle message") || !strings.Contains(s, "5 messages skipped") {
		t.Fatalf("the frame should skip the middle:\n%s", s)
	}
	m = press(t, typeText(t, press(t, m, "/"), "middle message 3"), "enter")
	s := screen(m)
	for _, want := range []string{"middle message 3", "middle message 1", "middle message 4", "messages skipped", "1/1"} {
		if !strings.Contains(s, want) {
			t.Errorf("lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "middle message 0") {
		t.Errorf("only two messages on each side should show:\n%s", s)
	}
	if !strings.Contains(m.View().Content, m.st.matchCur.Render("middle message 3")) {
		t.Error("the hit should be highlighted")
	}
	m = press(t, m, "esc")
	if s := screen(m); strings.Contains(s, "middle message") || !strings.Contains(s, "5 messages skipped") || m.scroll[focusConv] != 0 {
		t.Fatalf("esc should put the conversation back from the top:\n%s", s)
	}
}

func TestSearchParts(t *testing.T) {
	msgs := func(texts ...string) []db.Message {
		var out []db.Message
		for _, s := range texts {
			out = append(out, db.Message{Content: s})
		}
		return out
	}
	all := msgs("a", "b", "c", "x hit", "d", "e", "f", "g", "h", "x again", "i")
	parts, after, ok := searchParts(all, "x")
	if !ok || after != 0 || len(parts) != 2 {
		t.Fatalf("parts %d after %d ok %v", len(parts), after, ok)
	}
	// a is skipped, b c [x] d e show, f is skipped, g h [x] i show.
	if parts[0].skipped != 1 || len(parts[0].msgs) != 5 || parts[1].skipped != 1 || len(parts[1].msgs) != 4 {
		t.Fatalf("got %+v", parts)
	}
	if _, _, ok := searchParts(all, "zzz"); ok {
		t.Fatal("no message has zzz")
	}
	if parts, after, _ := searchParts(msgs("x", "a", "b", "c", "d"), "x"); parts[0].skipped != 0 || len(parts[0].msgs) != 3 || after != 2 {
		t.Fatalf("a hit at the start: %+v after %d", parts, after)
	}
}
