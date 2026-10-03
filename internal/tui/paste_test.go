package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/babarot/claude-recall/internal/config"
)

func TestPasteGoesToTheFieldBeingTyped(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 40)
	paste := tea.PasteMsg{Content: "parser"}

	// Nothing is typed in, so nothing takes it.
	if m := update(t, m, paste); m.filter.Value() != "" || m.mode != modeList {
		t.Fatalf("a paste in the list should do nothing, filter %q", m.filter.Value())
	}

	// The filter takes it and narrows the list, as typing would.
	m = update(t, press(t, m, "/"), paste)
	if m.filter.Value() != "parser" || len(m.visible) != 1 {
		t.Fatalf("filter %q, visible %v", m.filter.Value(), m.visible)
	}
	m = press(t, m, "esc")

	// The conversation search takes it.
	m = update(t, press(t, m, "space", "/"), paste)
	if m.conv.input.Value() != "parser" || m.filter.Value() != "" {
		t.Fatalf("conversation search %q, filter %q", m.conv.input.Value(), m.filter.Value())
	}
}

func TestPasteGoesToTheSidebarSearch(t *testing.T) {
	m := update(t, typeKeys(t, namedFolders(t), "/"), tea.PasteMsg{Content: "stai"})
	if m.sideSearch.Value() != "stai" || len(m.sideEntries()) != 2 {
		t.Fatalf("sidebar search %q, entries %+v", m.sideSearch.Value(), m.sideEntries())
	}
}
