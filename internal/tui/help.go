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

// move is the key hint of the four arrows' keys, as ↑ ↓  j k.
const move = "{up.0} {down.0}  {down.1} {up.1}"

// helpGroups are the key list's rows: a key hint template (see hintKeys)
// and what it does, which may name keys the same way. Rows that are not
// keys (the filter's syntax) and fixed keys (esc, a field's own keys) are
// written as they are.
var helpGroups = []helpGroup{
	{"Sessions", [][2]string{
		{move, "move; {top.1} {bottom.1} top and bottom, {page_up.0} {page_down.0} by page"},
		{"{resume.0}", "resume the session"},
		{"{read.0}", "read the conversation over the pane"},
		{"{copy_id.0}  {copy_command.0}", "copy the session ID, the resume command"},
		{"{search.0}", "filter (see below)"},
		{"{ask.0}", "ask Claude to find sessions (claude -p)"},
		{"{sort.0}", "choose the sort order"},
		{"{scope.0}", "this folder or all folders"},
		{"{list.folders_open.0} {list.folders_open.1}", "open the folder list, then move into it"},
		{"{list.folders_close.0} {list.folders_close.1}", "close the folder list"},
		{"{focus_next.0}  {focus_prev.0}", "next or previous frame ({focus_prev.1} and {focus_next.1} too)"},
		{"{grow.0} {shrink.0}", "resize the detail pane (or drag its edge)"},
		{"{quit.0}", "quit"},
	}},
	{"Folder list", [][2]string{
		{move, "pick a folder; the sessions follow"},
		{"{search.0}", "search folders by fuzzy match"},
		{"esc", "clear the search, then back to the sessions"},
		{"{folders.back.0} {folders.back.1} {folders.back.2}", "back to the sessions"},
	}},
	{"Detail frames", [][2]string{
		{move, "scroll; {top.1} {bottom.1}, {page_up.0} {page_down.0} too"},
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
		{move, "scroll; {top.1} {bottom.1}, {page_up.0} {page_down.0} too"},
		{"{focus_next.0}", "to the sessions: {down.1} {up.1} read the next one"},
		{"{grow.0} {shrink.0}", "more or fewer session rows (or drag)"},
		{"{search.0}", "search the conversation"},
		{"{next_match.0} {prev_match.0}", "next, previous match; esc clears"},
		{"{read.0} {quit.0} esc", "put the pane back"},
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
			keys := m.hintKeys(k[0])
			if keys == "" {
				continue // remapped away
			}
			key := m.st.key.Render(keys) + strings.Repeat(" ", max(1, helpKeyWidth-ansi.StringWidth(keys)))
			body = append(body, ansi.Truncate(key+m.st.subtle.Render(m.hintKeys(k[1])), inner, ellipsis))
		}
	}
	if len(body) > h-2 {
		body = append(body[:max(0, h-3)], m.st.muted.Render("… a taller terminal shows the rest"))
	}
	title := " Keys "
	hint := " " + m.hintKeys("{help.0} esc q close") + " "
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
