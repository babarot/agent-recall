package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/babarot/claude-recall/internal/config"
)

// settleSearch runs the conversation lookups the filter is waiting for, as
// the timer would once typing pauses.
func settleSearch(t *testing.T, m Model) Model {
	t.Helper()
	var run func(cmd tea.Cmd)
	run = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		switch msg := cmd().(type) {
		case tea.BatchMsg:
			for _, c := range msg {
				run(c)
			}
		case textSearchDone:
			m = update(t, m, msg)
		}
	}
	run(m.startTextSearch(textSearchTick{m.text.seq}))
	return m
}

func textModel(t *testing.T) Model {
	t.Helper()
	m, src := newTestModel(t, config.Default().TUI, 140, 40)
	src.said = map[string]string{
		"aaaaaaaa-1111": "we set up kubernetes here",
		"cccccccc-3333": "the docs are in the wiki",
	}
	return m
}

func TestPlainWordsSearchTheConversationToo(t *testing.T) {
	m := typeFilter(t, textModel(t), "kubernetes")
	if len(m.visible) != 0 || !strings.Contains(screen(m), "searching…") {
		t.Fatalf("before the lookup, nothing matches and the header says it is searching:\n%s", screen(m))
	}
	m = settleSearch(t, m)
	if got := visibleIDs(m); got != "aaaaaaaa-1111" || strings.Contains(screen(m), "searching…") {
		t.Fatalf("kubernetes, said in one session, shows %s", got)
	}
	// A word in one session's title and another's conversation finds both.
	m = settleSearch(t, typeFilter(t, textModel(t), "docs"))
	if got := visibleIDs(m); got != "bbbbbbbb-2222,cccccccc-3333" {
		t.Fatalf("docs shows %s", got)
	}
}

func TestTextSearchesOnlyTheConversation(t *testing.T) {
	m := settleSearch(t, typeFilter(t, textModel(t), "text:docs"))
	if got := visibleIDs(m); got != "cccccccc-3333" {
		t.Fatalf("text:docs should skip the title that has it, got %s", got)
	}
	// text: combines with words and in:.
	m = settleSearch(t, typeFilter(t, textModel(t), "text:wiki parser"))
	if got := visibleIDs(m); got != "cccccccc-3333" {
		t.Fatalf("text:wiki parser shows %s", got)
	}
}

func TestShortWordsAndStaleLookups(t *testing.T) {
	m := typeFilter(t, textModel(t), "k")
	if m.searching() {
		t.Fatal("a single letter should not be looked up in the conversations")
	}
	// A lookup scheduled before more typing is dropped.
	m = typeFilter(t, textModel(t), "kube")
	old := m.text.seq
	m = update(t, m, tea.KeyPressMsg{Code: 'r', Text: "r"})
	if cmd := m.startTextSearch(textSearchTick{old}); cmd != nil {
		t.Fatal("a stale tick should start nothing")
	}
	// Found words are kept: going back to one needs no new lookup.
	m = settleSearch(t, m)
	m = update(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = settleSearch(t, m)
	n := len(m.text.found)
	m = update(t, m, tea.KeyPressMsg{Code: 'r', Text: "r"})
	if m.searching() || len(m.text.found) != n {
		t.Fatal("kuber was looked up already")
	}
}
