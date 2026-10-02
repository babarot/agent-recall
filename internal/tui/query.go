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

// inFolder reports whether a folder name contains any in: fragment.
func (q query) inFolder(name string) bool {
	name = strings.ToLower(name)
	for _, v := range q.in {
		if strings.Contains(name, v) {
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
func (m Model) suggestions() ([]folderInfo, int) {
	if m.mode != modeFilter {
		return nil, 0
	}
	_, _, frag, ok := m.inTerm()
	if !ok {
		return nil, 0
	}
	if m.comp.active && m.comp.value == m.filter.Value() {
		return m.foldersMatching(m.comp.base), m.comp.idx
	}
	return m.foldersMatching(frag), 0
}

// complete replaces the in: term with the next (delta 1) or previous
// suggestion.
func (m *Model) complete(delta int) bool {
	start, end, frag, ok := m.inTerm()
	if !ok {
		return false
	}
	if !m.comp.active || m.comp.value != m.filter.Value() {
		m.comp = completion{active: true, base: frag, idx: -1}
		if delta < 0 {
			m.comp.idx = 0
		}
	}
	all := m.foldersMatching(m.comp.base)
	if len(all) == 0 {
		m.comp.active = false
		return false
	}
	m.comp.idx = (m.comp.idx + delta + len(all)) % len(all)
	v := []rune(m.filter.Value())
	term := []rune(inPrefix + all[m.comp.idx].name)
	next := string(v[:start]) + string(term) + string(v[end:])
	m.filter.SetValue(next)
	m.filter.SetCursor(start + len(term))
	m.comp.value = next
	return true
}

// foldersMatching lists the folders whose name contains frag, most recent
// first.
func (m Model) foldersMatching(frag string) []folderInfo {
	var out []folderInfo
	for _, f := range m.folders {
		if strings.Contains(strings.ToLower(f.name), frag) {
			out = append(out, f)
		}
	}
	return out
}

// suggestBox draws up to maxSuggest suggestions in a rounded box w cells
// wide, around the highlighted one.
func (m Model) suggestBox(list []folderInfo, idx, w int) []string {
	from := max(0, min(idx-maxSuggest/2, len(list)-maxSuggest))
	to := min(len(list), from+maxSuggest)
	inner := w - 4
	b := m.st.rule
	out := []string{b.Render("╭" + strings.Repeat("─", w-2) + "╮")}
	for i := from; i < to; i++ {
		num := fmt.Sprint(list[i].count)
		name := middleEllipsis(list[i].name, inner-len(num)-3)
		gap := strings.Repeat(" ", max(1, inner-2-ansi.StringWidth(name)-len(num)))
		line := "  " + m.st.text.Render(name) + gap + m.st.muted.Render(num)
		if i == idx {
			line = m.st.bar.Render("▎") + m.st.selected.Render(" ") + m.st.on(m.st.key, true).Render(name) +
				m.st.selected.Render(gap) + m.st.on(m.st.muted, true).Render(num)
		}
		out = append(out, b.Render("│ ")+line+b.Render(" │"))
	}
	if more := len(list) - to; more > 0 {
		label := fmt.Sprintf(" +%d more ", more)
		out = append(out, b.Render("╰─")+m.st.muted.Render(label)+b.Render(strings.Repeat("─", max(0, w-4-len(label)))+"─╯"))
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
