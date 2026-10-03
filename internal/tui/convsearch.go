package tui

import (
	"fmt"
	"strings"
	"time"

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
//
// A long session takes a moment to read and search, so the whole
// conversation is read in the background as soon as the search starts, the
// search runs once typing pauses (or on Enter), and the highlighted lines
// are built once per search rather than on every draw.

const (
	// convContext is how many lines above a hit stay in view when the frame
	// scrolls to it.
	convContext = 3
	// convAround is how many messages before and after one that has the
	// search are shown with it.
	convAround = 2
	// convSearchDelay is how long typing pauses before the search runs.
	convSearchDelay = 120 * time.Millisecond
	// minConvQuery is the shortest search run: one letter is in nearly
	// every message.
	minConvQuery = 2
)

type convSearch struct {
	input  textinput.Model
	typing bool // the search has the keys
	// applied is the search the frame shows, lower-cased: the input once
	// typing paused, or "" while it is shorter than minConvQuery.
	applied string
	seq     int // the last pause scheduled
	delay   time.Duration
	hits    []int    // lines of m.read that contain applied
	cur     int      // the hit scrolled to, an index into hits
	marked  []string // m.read with the hits highlighted, nil without hits
	// full and lower hold each session's whole conversation, and its text
	// lower-cased, once read; loading has the sessions being read.
	full    map[string][]db.Message
	lower   map[string][]string
	loading map[string]bool
}

func newConvSearch() convSearch {
	return convSearch{input: newConvSearchInput(), delay: convSearchDelay,
		full: map[string][]db.Message{}, lower: map[string][]string{}, loading: map[string]bool{}}
}

// convTick ends a pause in typing the search.
type convTick struct{ seq int }

// convLoaded brings a session's whole conversation.
type convLoaded struct {
	id   string
	msgs []db.Message
	err  error
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

// convQuery is the search the frame is built for.
func (m Model) convQuery() string { return m.conv.applied }

// typedQuery is the search as typed, as it would be applied.
func (m Model) typedQuery() string {
	q := strings.ToLower(strings.TrimSpace(m.conv.input.Value()))
	if len([]rune(q)) < minConvQuery {
		return ""
	}
	return q
}

// applyConvSearch shows the search as typed; readLines rebuilds the frame
// for it.
func (m *Model) applyConvSearch() {
	m.conv.seq++ // a pause still pending is moot
	m.conv.applied = m.typedQuery()
}

// convMsg handles the pause and the background read.
func (m *Model) convMsg(msg tea.Msg) {
	switch msg := msg.(type) {
	case convTick:
		if msg.seq == m.conv.seq {
			m.applyConvSearch()
		}
	case convLoaded:
		delete(m.conv.loading, msg.id)
		if msg.err != nil {
			return
		}
		lower := make([]string, len(msg.msgs))
		for i, mm := range msg.msgs {
			lower[i] = strings.ToLower(mm.Content)
		}
		m.conv.full[msg.id], m.conv.lower[msg.id] = msg.msgs, lower
		if r := m.current(); r != nil && r.s.ID == msg.id {
			m.readFor = "" // rebuild with it
		}
	}
}

// convLoadCmd reads the shown session's whole conversation in the
// background, once a search starts, when it is not read yet.
func (m *Model) convLoadCmd() tea.Cmd {
	r := m.current()
	if r == nil || !m.expanded || (!m.conv.typing && m.conv.applied == "") || m.source == nil {
		return nil
	}
	id := r.s.ID
	if _, ok := m.conv.full[id]; ok || m.conv.loading[id] {
		return nil
	}
	m.conv.loading[id] = true
	src := m.source
	return func() tea.Msg {
		msgs, err := src.SessionMessages(id)
		return convLoaded{id, msgs, err}
	}
}

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
			return m, nil
		}
		m.applyConvSearch() // no waiting for the pause
		return m, nil
	case "esc":
		m.clearConvSearch()
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	}
	// Once typing pauses, the search is applied and readLines rebuilds the
	// frame for it, going to the first hit.
	before := m.conv.input.Value()
	var cmd tea.Cmd
	m.conv.input, cmd = m.conv.input.Update(msg)
	if m.conv.input.Value() == before {
		return m, cmd
	}
	m.conv.seq++
	seq := m.conv.seq
	return m, tea.Batch(cmd, tea.Tick(m.conv.delay, func(time.Time) tea.Msg { return convTick{seq} }))
}

// findConvHits lists the lines of the conversation that have the query,
// and highlights them.
func (m *Model) findConvHits() {
	m.conv.hits, m.conv.marked = nil, nil
	q := m.convQuery()
	if q == "" {
		return
	}
	for i, l := range m.read {
		if strings.Contains(strings.ToLower(ansi.Strip(l)), q) {
			m.conv.hits = append(m.conv.hits, i)
		}
	}
	if len(m.conv.hits) == 0 {
		return
	}
	m.conv.marked = append([]string(nil), m.read...)
	for i := range m.conv.hits {
		m.markHit(i)
	}
}

// markHit highlights hit i, in the stronger color when it is current.
func (m *Model) markHit(i int) {
	style := m.st.match
	if i == m.conv.cur {
		style = m.st.matchCur
	}
	n := m.conv.hits[i]
	m.conv.marked[n] = markRunes(m.read[n], []rune(m.convQuery()), style)
}

// nextConvHit goes delta hits on, wrapping around.
func (m *Model) nextConvHit(delta int) {
	n := len(m.conv.hits)
	if n == 0 {
		return
	}
	prev := m.conv.cur
	m.conv.cur = ((m.conv.cur+delta)%n + n) % n
	if m.conv.marked != nil {
		m.markHit(prev)
		m.markHit(m.conv.cur)
	}
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
	m.conv.seq++
	m.conv.applied = ""
	m.conv.hits, m.conv.marked = nil, nil
	m.conv.cur = 0
}

// searchParts picks from the conversation the messages that have q, with
// convAround on each side, and how many are left out after the last one;
// lower is the messages' text lower-cased. ok is false when no message has
// q.
func searchParts(all []db.Message, lower []string, q string) (parts []convPart, after int, ok bool) {
	keep := make([]bool, len(all))
	for i := range all {
		if strings.Contains(lower[i], q) {
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

// highlightConv is the conversation with its hits highlighted, as built
// when the search ran.
func (m Model) highlightConv(lines []string) []string {
	if len(m.conv.marked) != len(lines) {
		return lines
	}
	return m.conv.marked
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
	r := m.current()
	switch {
	case m.typedQuery() != m.convQuery() || (r != nil && m.conv.loading[r.s.ID]):
		count = m.st.muted.Render("  searching…")
	case len(m.conv.hits) > 0:
		count = m.st.muted.Render(fmt.Sprintf("  %d/%d", m.conv.cur+1, len(m.conv.hits)))
	case m.convQuery() == "":
		count = ""
	}
	if m.conv.typing {
		return m.conv.input.View() + count
	}
	return m.st.filter.Render("/ "+m.conv.input.Value()) + count
}
