package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// The filter is words to find in a session's title, folder, branch, ID or
// conversation, and key:value terms that look in one field only: in:
// (folder), text: (conversation), title:, branch:, worktree: and id:. A new
// one gets a field in query, a line in query.fields and a case in match;
// one whose values are few enough to list also gets a line in completers.

const (
	inPrefix       = "in:"
	textPrefix     = "text:"
	titlePrefix    = "title:"
	branchPrefix   = "branch:"
	worktreePrefix = "worktree:"
	idPrefix       = "id:"
	maxSuggest     = 8
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
	// text are words to find only in the conversation.
	text []string
	// title, branch and worktree are parts of those fields, id the start of
	// the session ID. Several of one key match any of them.
	title, branch, worktree, id []string
}

// fields maps each key to where its values go.
func (q *query) fields() map[string]*[]string {
	return map[string]*[]string{
		inPrefix: &q.in, textPrefix: &q.text, titlePrefix: &q.title,
		branchPrefix: &q.branch, worktreePrefix: &q.worktree, idPrefix: &q.id,
	}
}

func parseQuery(s string) query {
	var q query
	fields := q.fields()
	for w := range strings.FieldsSeq(strings.ToLower(s)) {
		if key, v, ok := strings.Cut(w, ":"); ok {
			if dst, known := fields[key+":"]; known {
				if v != "" {
					*dst = append(*dst, v)
				}
				continue
			}
		}
		q.words = append(q.words, w)
	}
	return q
}

// anyOf reports whether test holds for one of vs, or vs is empty.
func anyOf(vs []string, test func(v string) bool) bool {
	if len(vs) == 0 {
		return true
	}
	for _, v := range vs {
		if test(v) {
			return true
		}
	}
	return false
}

// match reports whether r passes the query, the folder scope aside. A
// plain word may be in the session's fields or its conversation; a text:
// word only in the conversation. Words still being looked up in the
// conversation match only the fields until they are found.
func (m Model) match(q query, r *row) bool {
	if len(q.in) > 0 && !q.inFolder(r.groupName) {
		return false
	}
	in := func(field string) func(string) bool {
		field = strings.ToLower(field)
		return func(v string) bool { return strings.Contains(field, v) }
	}
	if !anyOf(q.title, in(r.title)) || !anyOf(q.branch, in(r.s.GitBranch)) || !anyOf(q.worktree, in(r.worktree)) ||
		!anyOf(q.id, func(v string) bool { return strings.HasPrefix(strings.ToLower(r.s.ID), v) }) {
		return false
	}
	for _, w := range q.words {
		if !strings.Contains(r.search, w) && !m.said(w, r.s.ID) {
			return false
		}
	}
	for _, w := range q.text {
		if !m.said(w, r.s.ID) {
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

// completers are the keys whose values the filter suggests while one is
// typed, and how: folders fuzzily, as in: matches; branches and worktrees
// by the part typed, most recently used first.
func (m Model) completers() map[string]func(frag string) []sideEntry {
	return map[string]func(string) []sideEntry{
		inPrefix:       m.rankFolders,
		branchPrefix:   func(f string) []sideEntry { return containing(m.branches, f) },
		worktreePrefix: func(f string) []sideEntry { return containing(m.worktrees, f) },
	}
}

// containing lists the values that contain frag, with where it matched.
func containing(values []sideEntry, frag string) []sideEntry {
	var out []sideEntry
	for _, v := range values {
		i := strings.Index(strings.ToLower(v.name), frag)
		if i < 0 {
			continue
		}
		start := len([]rune(v.name[:i]))
		var hits []int
		for j := range len([]rune(frag)) {
			hits = append(hits, start+j)
		}
		v.hits = hits
		out = append(out, v)
	}
	return out
}

// values lists the distinct non-empty values of a field over rows, most
// recently used first, with how many sessions have each.
func values(rows []row, field func(*row) string) []sideEntry {
	at := map[string]int{}
	var out []sideEntry
	var last []time.Time
	for i := range rows {
		v := field(&rows[i])
		if v == "" {
			continue
		}
		j, ok := at[v]
		if !ok {
			j = len(out)
			at[v] = j
			out = append(out, sideEntry{key: v, name: v})
			last = append(last, rows[i].s.EndedAt)
		}
		out[j].count++
		if rows[i].s.EndedAt.After(last[j]) {
			last[j] = rows[i].s.EndedAt
		}
	}
	order := make([]int, len(out))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return last[b].Compare(last[a]) })
	sorted := make([]sideEntry, len(out))
	for i, j := range order {
		sorted[i] = out[j]
	}
	return sorted
}

// completion tracks tab cycling through suggestions: base is what was
// typed before the first tab, idx the suggestion shown and value the
// filter after it.
type completion struct {
	active      bool
	base, value string
	idx         int
}

// keyTerm returns the filter term around the cursor when it is a key with
// suggestions: where it starts and ends, in runes, the key with its colon,
// and the value typed so far.
func (m Model) keyTerm() (start, end int, key, frag string, ok bool) {
	v := []rune(m.filter.Value())
	pos := min(m.filter.Position(), len(v))
	start, end = pos, pos
	for start > 0 && v[start-1] != ' ' {
		start--
	}
	for end < len(v) && v[end] != ' ' {
		end++
	}
	k, val, found := strings.Cut(strings.ToLower(string(v[start:end])), ":")
	if _, ok := m.completers()[k+":"]; !found || !ok {
		return 0, 0, "", "", false
	}
	return start, end, k + ":", val, true
}

// candidates are the suggestions for a key and the value typed so far.
func (m Model) candidates(key, frag string) []sideEntry { return m.completers()[key](frag) }

// suggestions are the folders for the in: term being typed, and which one
// is highlighted.
func (m Model) suggestions() ([]sideEntry, int) {
	if m.mode != modeFilter || m.sugHidden {
		return nil, 0
	}
	_, _, key, frag, ok := m.keyTerm()
	if !ok {
		return nil, 0
	}
	if m.comp.active && m.comp.value == m.filter.Value() {
		return m.candidates(key, m.comp.base), m.comp.idx
	}
	list := m.candidates(key, frag)
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

// complete replaces the key's term with the next (delta 1) or previous
// suggestion.
func (m *Model) complete(delta int) bool {
	start, end, key, frag, ok := m.keyTerm()
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
	all := m.candidates(key, m.comp.base)
	if len(all) == 0 {
		m.comp.active = false
		return false
	}
	m.comp.idx = (m.comp.idx + delta + len(all)) % len(all)
	m.comp.value = m.replaceTerm(start, end, key+all[m.comp.idx].name)
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

// pickSuggestion completes the key's term with suggestion i and a space,
// so the box closes and the next word can follow.
func (m *Model) pickSuggestion(i int) {
	list, _ := m.suggestions()
	start, end, key, _, ok := m.keyTerm()
	if !ok || i < 0 || i >= len(list) {
		return
	}
	v := []rune(m.filter.Value())
	if end < len(v) && v[end] == ' ' {
		end++
	}
	m.replaceTerm(start, end, key+list[i].name+" ")
	m.comp.active = false
	m.sugOff, m.sugSel = 0, 0
}

// scrollSuggestions moves the suggestion box's view by delta.
func (m *Model) scrollSuggestions(delta int) {
	list, _ := m.suggestions()
	m.sugOff = max(0, min(m.sugOff+delta, len(list)-maxSuggest))
}

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
