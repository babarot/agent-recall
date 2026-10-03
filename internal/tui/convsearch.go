package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/babarot/claude-recall/internal/db"
)

// Searching the conversation: / while the spread Conversation has the keys
// types a search. It looks through the whole conversation, not only the
// first message and the latest ones the frame shows: while there is a
// search, the frame shows the messages that have it with a few around each,
// and markers for the ones between. Every line that has it is a hit, shown
// highlighted; the frame scrolls to the first one, n and N go to the next
// and previous, and Esc drops the search and puts the conversation back as
// it was. It carries over to the next session read in place.

const (
	// convContext is how many lines above a hit stay in view when the frame
	// scrolls to it.
	convContext = 3
	// convAround is how many messages before and after one that has the
	// search are shown with it.
	convAround = 2
)

type convSearch struct {
	input  textinput.Model
	typing bool  // the search has the keys
	hits   []int // lines of m.read that contain the query
	cur    int   // the hit scrolled to, an index into hits
	// full holds each session's whole conversation once it is searched.
	full map[string][]db.Message
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
	// A change of the search rebuilds the frame (readLines), which goes to
	// the first hit.
	var cmd tea.Cmd
	m.conv.input, cmd = m.conv.input.Update(msg)
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

// clearConvSearch drops the search; the conversation is shown as it was,
// from the top.
func (m *Model) clearConvSearch() {
	if m.conv.input.Value() != "" {
		m.scroll[focusConv] = 0
	}
	m.conv.typing = false
	m.conv.input.Blur()
	m.conv.input.SetValue("")
	m.conv.hits = nil
	m.conv.cur = 0
}

// fullConversation is a session's whole conversation, read once; nil when
// it cannot be read.
func (m *Model) fullConversation(id string) []db.Message {
	if msgs, ok := m.conv.full[id]; ok {
		return msgs
	}
	if m.source == nil {
		return nil
	}
	msgs, err := m.source.SessionMessages(id)
	if err != nil {
		return nil
	}
	m.conv.full[id] = msgs
	return msgs
}

// searchParts picks from the conversation the messages that have q, with
// convAround on each side, and how many are left out after the last one.
// ok is false when no message has q.
func searchParts(all []db.Message, q string) (parts []convPart, after int, ok bool) {
	keep := make([]bool, len(all))
	for i, msg := range all {
		if strings.Contains(strings.ToLower(msg.Content), q) {
			ok = true
			for j := max(0, i-convAround); j <= min(len(all)-1, i+convAround); j++ {
				keep[j] = true
			}
		}
	}
	if !ok {
		return nil, 0, false
	}
	skipped := 0
	for i, msg := range all {
		if !keep[i] {
			skipped++
			continue
		}
		if skipped > 0 || len(parts) == 0 {
			parts = append(parts, convPart{skipped: skipped})
			skipped = 0
		}
		parts[len(parts)-1].msgs = append(parts[len(parts)-1].msgs, msg)
	}
	return parts, skipped, true
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
