package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Searching the conversation: / while the spread Conversation has the keys
// types a search. Every line that has it is a hit, shown highlighted; the
// frame scrolls to the first one, n and N go to the next and previous, and
// Esc drops the search. It looks through what the frame shows, the first
// message and the latest ones, and carries over to the next session read
// in place.

// convContext is how many lines above a hit stay in view when the frame
// scrolls to it.
const convContext = 3

type convSearch struct {
	input  textinput.Model
	typing bool  // the search has the keys
	hits   []int // lines of m.read that contain the query
	cur    int   // the hit scrolled to, an index into hits
}

func newConvSearchInput() textinput.Model {
	in := textinput.New()
	in.Prompt = "/ "
	in.Placeholder = "search the conversation"
	in.SetWidth(40)
	return in
}

// convSearching reports whether / and n reach the conversation search: the
// spread Conversation has the keys.
func (m Model) convSearching() bool { return m.expanded && m.focus == focusConv }

func (m Model) convQuery() string { return strings.ToLower(strings.TrimSpace(m.conv.input.Value())) }

// startConvSearch starts typing a search.
func (m *Model) startConvSearch() tea.Cmd {
	m.conv.typing = true
	return m.conv.input.Focus()
}

// updateConvSearch handles a key while the search is typed: Enter keeps the
// search, Esc drops it, and every change goes to the first hit.
func (m Model) updateConvSearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.conv.typing = false
		m.conv.input.Blur()
		if m.conv.input.Value() == "" {
			m.clearConvSearch()
		}
		return m, nil
	case "esc":
		m.clearConvSearch()
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	}
	before := m.conv.input.Value()
	var cmd tea.Cmd
	m.conv.input, cmd = m.conv.input.Update(msg)
	if m.conv.input.Value() != before {
		m.findConvHits()
		m.conv.cur = 0
		m.showConvHit()
	}
	return m, cmd
}

// findConvHits lists the lines of the conversation that have the query.
func (m *Model) findConvHits() {
	m.conv.hits = nil
	q := m.convQuery()
	if q == "" {
		return
	}
	for i, l := range m.read {
		if strings.Contains(strings.ToLower(ansi.Strip(l)), q) {
			m.conv.hits = append(m.conv.hits, i)
		}
	}
}

// nextConvHit goes delta hits on, wrapping around.
func (m *Model) nextConvHit(delta int) {
	n := len(m.conv.hits)
	if n == 0 {
		return
	}
	m.conv.cur = ((m.conv.cur+delta)%n + n) % n
	m.showConvHit()
}

// showConvHit scrolls the frame to the current hit, a few lines below the
// top.
func (m *Model) showConvHit() {
	if len(m.conv.hits) == 0 {
		return
	}
	target := max(0, m.conv.hits[m.conv.cur]-convContext)
	m.scrollFrame(focusConv, target-m.scroll[focusConv])
}

// clearConvSearch drops the search.
func (m *Model) clearConvSearch() {
	m.conv.typing = false
	m.conv.input.Blur()
	m.conv.input.SetValue("")
	m.conv.hits = nil
	m.conv.cur = 0
}

// highlightConv marks the query in the lines that have it, the current
// hit's line in a stronger color.
func (m Model) highlightConv(lines []string) []string {
	q := []rune(m.convQuery())
	if len(q) == 0 || len(m.conv.hits) == 0 {
		return lines
	}
	out := append([]string(nil), lines...)
	for i, n := range m.conv.hits {
		style := m.st.match
		if i == m.conv.cur {
			style = m.st.matchCur
		}
		out[n] = markRunes(out[n], q, style)
	}
	return out
}

// markRunes styles every case-insensitive occurrence of q in the styled
// line s.
func markRunes(s string, q []rune, style lipgloss.Style) string {
	plain := []rune(ansi.Strip(s))
	lower := []rune(strings.ToLower(string(plain)))
	if len(lower) != len(plain) { // lower-casing changed the length; match as is
		lower = plain
	}
	var ranges []lipgloss.Range
	for i := 0; i+len(q) <= len(lower); {
		if string(lower[i:i+len(q)]) != string(q) {
			i++
			continue
		}
		start := ansi.StringWidth(string(plain[:i]))
		end := start + ansi.StringWidth(string(plain[i:i+len(q)]))
		ranges = append(ranges, lipgloss.NewRange(start, end, style))
		i += len(q)
	}
	return lipgloss.StyleRanges(s, ranges...)
}

// convSearchLine is the search, under the conversation's title: the input
// while it is typed, then what was searched and which hit is shown. "" with
// no search.
func (m Model) convSearchLine() string {
	if !m.conv.typing && m.conv.input.Value() == "" {
		return ""
	}
	count := m.st.muted.Render("  no match")
	if n := len(m.conv.hits); n > 0 {
		count = m.st.muted.Render(fmt.Sprintf("  %d/%d", m.conv.cur+1, n))
	} else if m.convQuery() == "" {
		count = ""
	}
	if m.conv.typing {
		return m.conv.input.View() + count
	}
	return m.st.filter.Render("/ "+m.conv.input.Value()) + count
}
