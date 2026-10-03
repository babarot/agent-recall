package tui

import (
	"strings"

	"github.com/babarot/claude-recall/internal/config"
	"github.com/babarot/claude-recall/internal/db"
)

// Space spreads Conversation over the whole detail pane and grows the pane,
// leaving the list a few rows, to read the selected session in full:
// messages wrapped, the user's boxed and Claude's railed. Tab back to the
// list and j k read the next session in place. Space or Esc puts the pane
// back. The rows left to the list can be changed like the pane's height,
// with + - or by dragging its top edge, and are remembered.

const defaultExpandRows = 5

// expandedPaneHeight is the pane's height while Conversation is spread: all
// but expandRows list rows, within what the terminal allows.
func (m Model) expandedPaneHeight() int {
	room := m.height - m.chromeLines()
	h := max(config.MinDetailHeight, room-m.expandRows)
	return max(0, min(h, room-minListRows))
}

// toggleExpand spreads Conversation, focused and from the top, or puts it
// back.
func (m *Model) toggleExpand() {
	m.expanded = !m.expanded
	m.scroll[focusConv] = 0
	m.clearConvSearch()
	if m.expanded {
		m.focus = focusConv
	} else if m.focus != focusFolders {
		m.focus = focusList
	}
	m.clamp()
}

// readLines renders the selected session's conversation for the spread
// frame, once per session and width.
func (m *Model) readLines() {
	r := m.current()
	if !m.expanded || r == nil {
		return
	}
	w := m.width - 4
	q := m.convQuery()
	key := r.s.ID + "\x00" + string(rune(w)) + "\x00" + q
	if key == m.readFor {
		return
	}
	m.readFor = key
	d := m.details[r.s.ID]
	if d == nil && m.detailLoading[r.s.ID] {
		m.read = []string{m.st.muted.Render("Loading…")}
		m.readFor = "" // built again once it comes
		return
	}
	if d == nil || d.First == nil {
		m.read = []string{m.st.muted.Render("No text messages.")}
		return
	}
	// With a search, the whole conversation's messages that have it, with
	// some around each; else, or when none has it, the first message and
	// the latest ones.
	rendered := ""
	if q != "" {
		// Until the whole conversation is read, the first message and the
		// latest ones; the frame is rebuilt when it comes.
		if all, ok := m.conv.full[r.s.ID]; ok {
			if parts, after, ok := searchParts(all, m.conv.lower[r.s.ID], q); ok {
				rendered = m.renderParts(parts, after, w)
			}
		}
	}
	if rendered == "" {
		p := db.Preview{Head: []db.Message{*d.First}, Tail: d.Tail, Skipped: d.Hidden}
		rendered = m.renderConversation(p, w)
	}
	m.read = strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	// A search carries over to the next session read in place.
	m.findConvHits()
	m.conv.cur = 0
	m.showConvHit()
}
