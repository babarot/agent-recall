package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/babarot/claude-recall/internal/config"
	"github.com/babarot/claude-recall/internal/db"
)

func TestParseQuery(t *testing.T) {
	cases := []struct {
		in        string
		words, at []string
	}{
		{"login bug", []string{"login", "bug"}, nil},
		{"in:Dotfiles nix", []string{"nix"}, []string{"dotfiles"}},
		{"in:a in:b", nil, []string{"a", "b"}},
		{"in: http://x", []string{"http://x"}, nil}, // an empty in: is nothing, other keys are words
	}
	for _, c := range cases {
		q := parseQuery(c.in)
		if !reflect.DeepEqual(q.words, c.words) || !reflect.DeepEqual(q.in, c.at) {
			t.Errorf("parseQuery(%q) = %q %q", c.in, q.words, q.in)
		}
	}
}

// typeFilter opens the filter and types s.
func typeFilter(t *testing.T, m Model, s string) Model {
	t.Helper()
	m = press(t, m, "/")
	for _, r := range s {
		m = update(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

func TestInNarrowsToFolders(t *testing.T) {
	f := newFolderFixture(t)
	// Narrowed to the repository, in: still reaches the other folder.
	m := typeFilter(t, folderModel(t, config.Default().TUI, f, 140, 40).StartIn(f.repo), "in:note")
	if got := visibleIDs(m); got != "other-1,other-2" {
		t.Fatalf("in:note shows %s", got)
	}
	if head := strings.Split(screen(m), "\n")[0]; !strings.Contains(head, "2 / 6 sessions · in ") || !strings.Contains(head, "notes · sort") {
		t.Errorf("header should name the folder: %q", head)
	}
	// Words and in: combine; several in: are any of them.
	m = typeFilter(t, folderModel(t, config.Default().TUI, f, 140, 40), "in:app wt")
	if got := visibleIDs(m); got != "wt-1" {
		t.Fatalf("in:app wt shows %s", got)
	}
	m = typeFilter(t, folderModel(t, config.Default().TUI, f, 140, 40), "in:app in:notes")
	if len(m.visible) != 6 || !strings.Contains(screen(m), "in 2 folders") {
		t.Fatalf("two in: terms show %s:\n%s", visibleIDs(m), screen(m))
	}
}

func TestInSuggestsAndCompletes(t *testing.T) {
	f := newFolderFixture(t)
	m := typeFilter(t, folderModel(t, config.Default().TUI, f, 140, 40), "in:")
	s := screen(m)
	if !strings.Contains(s, "╭") || !strings.Contains(s, "notes") || !strings.Contains(s, "↑↓ folder · enter pick · tab complete · esc close") {
		t.Fatalf("in: should list folders to complete:\n%s", s)
	}
	m = press(t, m, "tab")
	first := inPrefix + m.folders[0].name
	if m.filter.Value() != first {
		t.Fatalf("tab gave %q, want %q", m.filter.Value(), first)
	}
	m = press(t, m, "tab")
	if m.filter.Value() != inPrefix+m.folders[1].name {
		t.Fatalf("second tab gave %q", m.filter.Value())
	}
	m = update(t, m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.filter.Value() != first {
		t.Fatalf("shift+tab gave %q", m.filter.Value())
	}
	if got := visibleIDs(m); got != "repo-1,sub-1,wt-1,herdr-1" {
		t.Fatalf("completed in: shows %s", got)
	}
	// A plain word shows no suggestions; tab leaves it alone.
	m = typeFilter(t, folderModel(t, config.Default().TUI, f, 140, 40), "wt")
	if m = press(t, m, "tab"); m.filter.Value() != "wt" || strings.Contains(screen(m), "╭─╮") {
		t.Fatalf("got %q", m.filter.Value())
	}
}

func TestSuggestionsFitTheTerminal(t *testing.T) {
	f := newFolderFixture(t)
	for _, w := range []int{60, 100, 160} {
		m := typeFilter(t, folderModel(t, config.Default().TUI, f, w, 24), "in:")
		for i, l := range strings.Split(m.render(), "\n") {
			if got := ansi.StringWidth(l); got > w {
				t.Errorf("%d: line %d is %d wide: %q", w, i, got, ansi.Strip(l))
			}
		}
	}
}

func TestOverlayKeepsBothSides(t *testing.T) {
	if got := ansi.Strip(overlay("abcdefghij", "XY", 3)); got != "abcXYfghij" {
		t.Fatalf("got %q", got)
	}
	if got := ansi.Strip(overlay("ab", "XY", 4)); got != "ab  XY" {
		t.Fatalf("past the end: %q", got)
	}
	if got := ansi.Strip(overlay("あいう", "X", 1)); ansi.StringWidth(got) != 6 {
		t.Fatalf("a wide character cut in half: %q", got)
	}
}

// manyFolders is a model with 12 folders, proj-00 the most recent.
func manyFolders(t *testing.T) Model {
	t.Helper()
	base := t.TempDir()
	var ss []db.Session
	for i := range 12 {
		dir := filepath.Join(base, fmt.Sprintf("proj-%02d", i))
		os.MkdirAll(dir, 0o755)
		ss = append(ss, db.Session{ID: fmt.Sprintf("s%02d", i), ProjectPath: dir, Title: "t", EndedAt: now.Add(time.Duration(-i) * time.Hour)})
	}
	m := New(ss, &fakePreview{}, config.Default().TUI)
	m.now = func() time.Time { return now }
	return update(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
}

func TestSuggestionsScrollAndClick(t *testing.T) {
	m := typeFilter(t, manyFolders(t), "in:proj")
	if s := screen(m); !strings.Contains(s, "proj-07") || strings.Contains(s, "proj-08") || !strings.Contains(s, "↓ 4 more") {
		t.Fatalf("the box should show 8 of 12:\n%s", s)
	}
	r, _, _, _, _ := m.suggestRect()
	wheel := tea.MouseWheelMsg{X: r.x + 5, Y: r.y + 2, Button: tea.MouseWheelDown}
	for range 10 {
		m = update(t, m, wheel)
	}
	s := screen(m)
	if !strings.Contains(s, "proj-11") || strings.Contains(s, "proj-03") || !strings.Contains(s, "↑ 4 more") {
		t.Fatalf("scrolled to the end, the box should show proj-04 to proj-11:\n%s", s)
	}
	// The first row shown is proj-04; clicking it completes the term.
	m = update(t, m, tea.MouseClickMsg{X: r.x + 5, Y: r.y + 1, Button: tea.MouseLeft})
	if v := m.filter.Value(); !strings.HasPrefix(v, "in:") || !strings.HasSuffix(v, "proj-04 ") {
		t.Fatalf("click gave %q", v)
	}
	if got := visibleIDs(m); got != "s04" {
		t.Fatalf("after the click the list shows %s", got)
	}
	if _, _, _, _, open := m.suggestRect(); open {
		t.Fatal("the box should close once a folder is picked")
	}
	// Typing again starts at the top.
	m = typeFilter(t, manyFolders(t), "in:proj")
	for range 3 {
		m = update(t, m, wheel)
	}
	m = update(t, m, tea.KeyPressMsg{Code: '-', Text: "-"})
	if m.sugOff != 0 {
		t.Fatalf("typing should reset the scroll, got %d", m.sugOff)
	}
}

func TestTabKeepsTheHighlightInView(t *testing.T) {
	m := typeFilter(t, manyFolders(t), "in:proj")
	for range 10 {
		m = press(t, m, "tab")
	}
	if s := screen(m); !strings.Contains(m.filter.Value(), "proj-09") || !strings.Contains(s, "▎ ") || !strings.Contains(s, "↑ 2 more") {
		t.Fatalf("after 10 tabs, %q:\n%s", m.filter.Value(), s)
	}
}

func TestKeysGoToTheSuggestions(t *testing.T) {
	m := typeFilter(t, manyFolders(t), "in:proj")
	cursor := m.cursor
	for range 9 {
		m = press(t, m, "down")
	}
	if m.cursor != cursor || m.filter.Value() != "in:proj" {
		t.Fatalf("down should move the highlight, not the list or the text: cursor %d, %q", m.cursor, m.filter.Value())
	}
	if _, idx := m.suggestions(); idx != 9 || !strings.Contains(screen(m), "↑ 2 more") {
		t.Fatalf("highlight %d:\n%s", idx, screen(m))
	}
	m = update(t, m, tea.KeyPressMsg{Code: tea.KeyUp})
	m = press(t, m, "enter")
	if v := m.filter.Value(); !strings.HasSuffix(v, "proj-08 ") || m.mode != modeFilter {
		t.Fatalf("enter should pick proj-08 and keep filtering: %q", v)
	}
	if got := visibleIDs(m); got != "s08" {
		t.Fatalf("list shows %s", got)
	}
	// With the box closed, the keys are the list's again.
	m = press(t, m, "enter")
	if m.mode != modeList {
		t.Fatal("a second enter applies the filter")
	}

	// Tab starts from the highlighted suggestion.
	m = typeFilter(t, manyFolders(t), "in:proj")
	m = press(t, m, "down", "down", "tab")
	if v := m.filter.Value(); !strings.HasSuffix(v, "proj-02") {
		t.Fatalf("tab after two downs gave %q", v)
	}

	// Esc closes the box first, then clears the filter.
	m = typeFilter(t, manyFolders(t), "in:proj")
	m = press(t, m, "esc")
	if _, _, _, _, open := m.suggestRect(); open || m.filter.Value() != "in:proj" {
		t.Fatalf("esc should close the box and keep the text: %q", m.filter.Value())
	}
	if m = press(t, m, "down"); m.cursor != 1 {
		t.Fatalf("with the box closed, down moves the list: %d", m.cursor)
	}
	if m = press(t, m, "esc"); m.filter.Value() != "" || m.mode != modeList {
		t.Fatal("a second esc clears the filter")
	}
}

func TestInMatchesFuzzily(t *testing.T) {
	right := tea.KeyPressMsg{Code: tea.KeyRight}
	m := update(t, update(t, namedFolders(t), right), right) // back on the sessions, folder list closed
	m = typeFilter(t, m, "in:srv")
	list, _ := m.suggestions()
	if len(list) != 1 || list[0].name != "~/stailer-server" {
		t.Fatalf("in:srv suggests %+v", list)
	}
	if got := visibleIDs(m); got != "stailer-server" {
		t.Fatalf("in:srv shows %s", got)
	}
	if !strings.Contains(m.render(), m.st.on(m.st.filter.Bold(true), true).Render("v")) {
		t.Error("the matched letters should be highlighted in the suggestions")
	}
	// Best match first: stai puts stailer before stailer-server.
	m = typeFilter(t, update(t, update(t, namedFolders(t), right), right), "in:stai")
	if list, _ := m.suggestions(); len(list) != 2 || list[0].name != "~/stailer" {
		t.Fatalf("in:stai suggests %+v", list)
	}
}
