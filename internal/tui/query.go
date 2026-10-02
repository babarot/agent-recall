package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// The filter is words to find in a session's title, folder, branch or ID,
// and key:value terms that narrow by one field. in:<folder> is the first
// such term; new ones get a field in query and a case in parseQuery.

const (
	inPrefix   = "in:"
	maxSuggest = 8
	// The suggestion box opens under the filter line, this far in.
	suggestX, suggestTop = 3, 2
	maxSuggestW          = 48
)

// query is the parsed filter, lower-cased.
type query struct {
	words []string
	// in are folder name fragments; a session matches when its folder
	// contains any of them. They override the folder the list is narrowed
	// to.
	in []string
}

func parseQuery(s string) query {
	var q query
	for w := range strings.FieldsSeq(strings.ToLower(s)) {
		if v, ok := strings.CutPrefix(w, inPrefix); ok {
			if v != "" {
				q.in = append(q.in, v)
			}
			continue
		}
		q.words = append(q.words, w)
	}
	return q
}

// match reports whether r passes the query, the folder scope aside.
func (q query) match(r *row) bool {
	if len(q.in) > 0 && !q.inFolder(r.groupName) {
		return false
	}
	for _, w := range q.words {
		if !strings.Contains(r.search, w) {
			return false
		}
	}
	return true
}

// inFolders lists the folders an in: query matches, most recent first.
func (m Model) inFolders(q query) []folderInfo {
	var out []folderInfo
	for _, f := range m.folders {
		if q.inFolder(f.name) {
			out = append(out, f)
		}
	}
	return out
}

// inFolder reports whether a folder name fuzzy-matches any in: fragment,
// as the folder list's search does.
func (q query) inFolder(name string) bool {
	for _, v := range q.in {
		if _, _, ok := fuzzyMatch(v, name); ok {
			return true
		}
	}
	return false
}

// completion tracks tab cycling through folder suggestions: base is what
// was typed before the first tab, idx the suggestion shown and value the
// filter after it.
type completion struct {
	active      bool
	base, value string
	idx         int
}

// inTerm returns the filter term around the cursor when it is an in: term:
// where it starts and ends, in runes, and the folder fragment typed so far.
func (m Model) inTerm() (start, end int, frag string, ok bool) {
	v := []rune(m.filter.Value())
	pos := min(m.filter.Position(), len(v))
	start, end = pos, pos
	for start > 0 && v[start-1] != ' ' {
		start--
	}
	for end < len(v) && v[end] != ' ' {
		end++
	}
	term := string(v[start:end])
	if !strings.HasPrefix(strings.ToLower(term), inPrefix) {
		return 0, 0, "", false
	}
	return start, end, strings.ToLower(term[len(inPrefix):]), true
}

// suggestions are the folders for the in: term being typed, and which one
// is highlighted.
func (m Model) suggestions() ([]sideEntry, int) {
	if m.mode != modeFilter || m.sugHidden {
		return nil, 0
	}
	_, _, frag, ok := m.inTerm()
	if !ok {
		return nil, 0
	}
	if m.comp.active && m.comp.value == m.filter.Value() {
		return m.foldersMatching(m.comp.base), m.comp.idx
	}
	list := m.foldersMatching(frag)
	return list, max(0, min(m.sugSel, len(list)-1))
}

// moveSuggestion highlights the suggestion delta away. After a tab it
// cycles the completed term instead, as tab does.
func (m *Model) moveSuggestion(delta int) {
	if m.comp.active && m.comp.value == m.filter.Value() {
		m.complete(delta)
		return
	}
	list, idx := m.suggestions()
	m.sugSel = max(0, min(idx+delta, len(list)-1))
	m.reveal(m.sugSel)
}

// reveal scrolls the suggestion box to show suggestion i.
func (m *Model) reveal(i int) {
	if i < m.sugOff {
		m.sugOff = i
	}
	if i >= m.sugOff+maxSuggest {
		m.sugOff = i - maxSuggest + 1
	}
}

// acceptSuggestion picks the highlighted suggestion.
func (m *Model) acceptSuggestion() {
	_, idx := m.suggestions()
	m.pickSuggestion(idx)
}

// complete replaces the in: term with the next (delta 1) or previous
// suggestion.
func (m *Model) complete(delta int) bool {
	start, end, frag, ok := m.inTerm()
	if !ok {
		return false
	}
	if !m.comp.active || m.comp.value != m.filter.Value() {
		// Start from the highlighted suggestion: tab takes it.
		_, sel := m.suggestions()
		m.comp = completion{active: true, base: frag, idx: sel - 1}
		if delta < 0 {
			m.comp.idx = sel + 1
		}
	}
	all := m.foldersMatching(m.comp.base)
	if len(all) == 0 {
		m.comp.active = false
		return false
	}
	m.comp.idx = (m.comp.idx + delta + len(all)) % len(all)
	m.comp.value = m.replaceTerm(start, end, inPrefix+all[m.comp.idx].name)
	m.sugSel = m.comp.idx
	m.reveal(m.comp.idx)
	return true
}

// replaceTerm puts term in place of the filter's runes start to end, with
// the cursor after it, and returns the new filter.
func (m *Model) replaceTerm(start, end int, term string) string {
	v := []rune(m.filter.Value())
	next := string(v[:start]) + term + string(v[end:])
	m.filter.SetValue(next)
	m.filter.SetCursor(start + len([]rune(term)))
	return next
}

// suggestRect is where the suggestion box is drawn, and the first
// suggestion it shows; ok is false when there is none.
func (m Model) suggestRect() (r rect, list []sideEntry, idx, from int, ok bool) {
	list, idx = m.suggestions()
	if len(list) == 0 {
		return rect{}, nil, 0, 0, false
	}
	from = max(0, min(m.sugOff, len(list)-maxSuggest))
	rows := min(maxSuggest, len(list))
	return rect{suggestX, suggestTop, min(maxSuggestW, m.width-suggestX-1), rows + 2}, list, idx, from, true
}

// suggestionAt returns the suggestion under a screen cell, or -1.
func (m Model) suggestionAt(x, y int) int {
	r, list, _, from, ok := m.suggestRect()
	if !ok || !r.contains(x, y) || y == r.y || y == r.y+r.h-1 {
		return -1
	}
	if i := from + y - r.y - 1; i < len(list) {
		return i
	}
	return -1
}

// pickSuggestion completes the in: term with suggestion i and a space, so
// the box closes and the next word can follow.
func (m *Model) pickSuggestion(i int) {
	list, _ := m.suggestions()
	start, end, _, ok := m.inTerm()
	if !ok || i < 0 || i >= len(list) {
		return
	}
	v := []rune(m.filter.Value())
	if end < len(v) && v[end] == ' ' {
		end++
	}
	m.replaceTerm(start, end, inPrefix+list[i].name+" ")
	m.comp.active = false
	m.sugOff, m.sugSel = 0, 0
}

// scrollSuggestions moves the suggestion box's view by delta.
func (m *Model) scrollSuggestions(delta int) {
	list, _ := m.suggestions()
	m.sugOff = max(0, min(m.sugOff+delta, len(list)-maxSuggest))
}

// foldersMatching lists the folders an in: fragment matches, best first.
func (m Model) foldersMatching(frag string) []sideEntry { return m.rankFolders(frag) }

// suggestBox draws up to maxSuggest suggestions from from in a rounded box
// w cells wide, with how many more lie above and below on its edges.
func (m Model) suggestBox(list []sideEntry, idx, from, w int) []string {
	to := min(len(list), from+maxSuggest)
	inner := w - 4
	b := m.st.rule
	out := []string{b.Render("╭" + strings.Repeat("─", w-2) + "╮")}
	if from > 0 {
		label := fmt.Sprintf(" ↑ %d more ", from)
		out[0] = b.Render("╭─") + m.st.muted.Render(label) + b.Render(strings.Repeat("─", max(0, w-4-ansi.StringWidth(label)))+"─╮")
	}
	for i := from; i < to; i++ {
		num := fmt.Sprint(list[i].count)
		e := list[i]
		name := middleEllipsis(e.name, inner-len(num)-3)
		gap := strings.Repeat(" ", max(1, inner-2-ansi.StringWidth(name)-len(num)))
		line := "  " + m.highlight(e.name, name, e.hits, m.st.text, m.st.filter.Bold(true)) + gap + m.st.muted.Render(num)
		if i == idx {
			line = m.st.bar.Render("▎") + m.st.selected.Render(" ") + m.highlight(e.name, name, e.hits, m.st.on(m.st.key, true), m.st.on(m.st.filter.Bold(true), true)) +
				m.st.selected.Render(gap) + m.st.on(m.st.muted, true).Render(num)
		}
		out = append(out, b.Render("│ ")+line+b.Render(" │"))
	}
	if more := len(list) - to; more > 0 {
		label := fmt.Sprintf(" ↓ %d more ", more)
		out = append(out, b.Render("╰─")+m.st.muted.Render(label)+b.Render(strings.Repeat("─", max(0, w-4-ansi.StringWidth(label)))+"─╯"))
	} else {
		out = append(out, b.Render("╰"+strings.Repeat("─", w-2)+"╯"))
	}
	return out
}

// overlay draws over on top of base starting at cell x, keeping what is
// left and right of it.
func overlay(base, over string, x int) string {
	left := ansi.Truncate(base, x, "")
	left += strings.Repeat(" ", max(0, x-ansi.StringWidth(left)))
	end := x + ansi.StringWidth(over)
	right := ansi.TruncateLeft(base, end, "")
	// A wide character cut in half leaves a gap.
	if gap := ansi.StringWidth(base) - end - ansi.StringWidth(right); gap > 0 {
		right = strings.Repeat(" ", gap) + right
	}
	return left + "\x1b[m" + over + "\x1b[m" + right
}
