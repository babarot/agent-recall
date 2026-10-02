package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// The filter also looks in what was said: a plain word matches a session
// whose title, folder, branch or ID has it, or whose conversation does;
// text:<word> matches only the conversation. Reading every message takes
// a moment, so it runs in the background once typing pauses, and each
// word's sessions are kept for the rest of the run.

const (
	textSearchDelay = 200 * time.Millisecond
	// minBodyWord is the shortest plain word looked up in the conversation;
	// a single letter is in nearly every session.
	minBodyWord = 2
)

// textSearch holds the conversation lookups: the sessions each word was
// found in, the words being looked up, and the filter last scheduled.
type textSearch struct {
	found   map[string]map[string]bool
	pending map[string]bool
	seq     int
	for_    string
	delay   time.Duration
}

type textSearchTick struct{ seq int }

type textSearchDone struct {
	word string
	ids  []string
	err  error
}

// bodyWords are the words of q to look up in the conversation.
func bodyWords(q query) []string {
	var out []string
	for _, w := range q.words {
		if len([]rune(w)) >= minBodyWord {
			out = append(out, w)
		}
	}
	return append(out, q.text...)
}

// scheduleTextSearch starts the delay before looking up the filter's new
// words, when the filter changed.
func (m *Model) scheduleTextSearch() tea.Cmd {
	v := m.filter.Value()
	if v == m.text.for_ {
		return nil
	}
	m.text.for_ = v
	m.text.seq++
	seq := m.text.seq
	return tea.Tick(m.text.delay, func(time.Time) tea.Msg { return textSearchTick{seq} })
}

// startTextSearch looks up the words not yet known, once the filter has
// settled.
func (m *Model) startTextSearch(t textSearchTick) tea.Cmd {
	if t.seq != m.text.seq {
		return nil // typed on since
	}
	var cmds []tea.Cmd
	for _, w := range bodyWords(parseQuery(m.filter.Value())) {
		if _, ok := m.text.found[w]; ok || m.text.pending[w] {
			continue
		}
		m.text.pending[w] = true
		src := m.source
		cmds = append(cmds, func() tea.Msg {
			ids, err := src.SessionsWithText(w)
			return textSearchDone{w, ids, err}
		})
	}
	return tea.Batch(cmds...)
}

func (m *Model) finishTextSearch(d textSearchDone) tea.Cmd {
	delete(m.text.pending, d.word)
	if d.err != nil {
		// Found nowhere, so the filter does not wait on it forever.
		m.text.found[d.word] = map[string]bool{}
		m.refresh()
		return m.showToast(toastWarn, "Could not search the conversations: "+d.err.Error())
	}
	set := make(map[string]bool, len(d.ids))
	for _, id := range d.ids {
		set[id] = true
	}
	m.text.found[d.word] = set
	m.refresh()
	return nil
}

// said reports whether word was found in the conversation of session id.
func (m Model) said(word, id string) bool { return m.text.found[word][id] }

// searching reports whether a word of the filter is still being looked up.
func (m Model) searching() bool {
	for _, w := range bodyWords(parseQuery(m.filter.Value())) {
		if _, ok := m.text.found[w]; !ok {
			return true
		}
	}
	return false
}
