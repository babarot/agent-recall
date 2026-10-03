package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ? shows every key, grouped by where it works, over the screen; ?, Esc or
// q closes it.

type helpGroup struct {
	title string
	keys  [][2]string
}

var helpGroups = []helpGroup{
	{"Sessions", [][2]string{
		{"↑ ↓  j k", "move; g G top and bottom, PgUp PgDn by page"},
		{"enter", "resume the session"},
		{"space", "read the conversation over the pane"},
		{"y  Y", "copy the session ID, the resume command"},
		{"/", "filter (see below)"},
		{"a", "ask Claude to find sessions (claude -p)"},
		{"s", "choose the sort order"},
		{".", "this folder or all folders"},
		{"← h", "open the folder list, then move into it"},
		{"→ l", "close the folder list"},
		{"tab  ⇧tab", "next or previous frame ([ and ] too)"},
		{"+ -", "resize the detail pane (or drag its edge)"},
		{"q", "quit"},
	}},
	{"Folder list", [][2]string{
		{"↑ ↓  j k", "pick a folder; the sessions follow"},
		{"/", "search folders by fuzzy match"},
		{"esc", "clear the search, then back to the sessions"},
		{"→ l enter", "back to the sessions"},
	}},
	{"Detail frames", [][2]string{
		{"↑ ↓  j k", "scroll; g G, PgUp PgDn too"},
		{"esc", "back to the sessions"},
	}},
	{"Filter", [][2]string{
		{"words", "title, folder, branch, ID or what was said"},
		{"folder:  in:", "folder, fuzzy (folder:bdot)"},
		{"text:", "only what was said"},
		{"title: branch:", "only that field"},
		{"worktree: id:", "worktree name, start of the ID"},
		{"tab  →", "complete a key or a suggested value"},
		{"↑ ↓  enter", "pick a suggestion"},
		{"esc", "close suggestions, then clear the filter"},
	}},
	{"Reading (space)", [][2]string{
		{"↑ ↓  j k", "scroll; g G, PgUp PgDn too"},
		{"tab", "to the sessions: j k read the next one"},
		{"+ -", "more or fewer session rows (or drag)"},
		{"space esc", "put the pane back"},
	}},
}

const helpKeyWidth = 15

// helpBox draws the key list in a rounded box at most w cells wide and h
// lines tall.
func (m Model) helpBox(w, h int) []string {
	inner := w - 4
	b := m.st.rule
	var body []string
	for i, g := range helpGroups {
		if i > 0 {
			body = append(body, "")
		}
		body = append(body, m.section(g.title))
		for _, k := range g.keys {
			key := m.st.key.Render(k[0]) + strings.Repeat(" ", max(1, helpKeyWidth-ansi.StringWidth(k[0])))
			body = append(body, ansi.Truncate(key+m.st.subtle.Render(k[1]), inner, ellipsis))
		}
	}
	if len(body) > h-2 {
		body = append(body[:max(0, h-3)], m.st.muted.Render("… a taller terminal shows the rest"))
	}
	title := " Keys "
	hint := " ? esc q close "
	fill := max(0, w-4-len(title)-ansi.StringWidth(hint))
	out := []string{b.Render("╭─") + m.st.key.Render(title) + b.Render(strings.Repeat("─", fill)) + m.st.muted.Render(hint) + b.Render("─╮")}
	for _, l := range body {
		pad := strings.Repeat(" ", max(0, inner-ansi.StringWidth(l)))
		out = append(out, b.Render("│ ")+l+pad+b.Render(" │"))
	}
	return append(out, b.Render("╰"+strings.Repeat("─", w-2)+"╯"))
}

// withHelp lays the key list over the screen, centered.
func (m Model) withHelp(screen string) string {
	lines := strings.Split(screen, "\n")
	w := min(64, m.width-2)
	if w < 30 {
		return screen
	}
	box := m.helpBox(w, m.height-2)
	top := max(0, (len(lines)-len(box))/2)
	x := (m.width - w) / 2
	for i, l := range box {
		if j := top + i; j < len(lines) {
			lines[j] = overlay(lines[j], l, x)
		}
	}
	return strings.Join(lines, "\n")
}
