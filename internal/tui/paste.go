package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
)

// pasteKey is the key that pastes from the clipboard. The terminal's own
// paste (cmd+v) arrives as a tea.PasteMsg instead.
const pasteKey = "ctrl+v"

// typing reports whether a field is being typed in, the one a key goes to
// in the order update gives keys out.
func (m Model) typing() bool {
	switch {
	case m.cont.open:
		return true
	case m.ask.stage != askClosed:
		return m.ask.stage == askTyping
	case m.sortMenu, m.helpOpen:
		return false
	}
	return m.mode == modeFilter || (m.focus == focusFolders && m.sideTyping) || m.conv.typing
}

// paste puts pasted text in the field being typed in, if any.
func (m Model) paste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	if !m.typing() {
		return m, nil
	}
	switch {
	case m.cont.open:
		var cmd tea.Cmd
		m.cont.input, cmd = m.cont.input.Update(msg)
		return m, cmd
	case m.ask.stage == askTyping:
		var cmd tea.Cmd
		m.ask.input, cmd = m.ask.input.Update(msg)
		return m, cmd
	case m.mode == modeFilter:
		return m.typeFilter(msg)
	case m.focus == focusFolders && m.sideTyping:
		return m.typeSideSearch(msg)
	}
	return m.typeConvSearch(msg)
}

// readClipboard reads the clipboard into a paste.
func readClipboard() tea.Msg {
	s, err := clipboard.ReadAll()
	if err != nil || s == "" {
		return nil
	}
	return tea.PasteMsg{Content: s}
}
