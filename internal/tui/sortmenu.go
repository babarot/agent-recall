package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// s opens a menu of the sort orders over the screen:
// the arrows, j k or the mouse pick one and Enter applies it, as does its
// number; Esc, s or q closes the menu as it was.

// sortNotes say what each order puts first.
var sortNotes = map[string]string{
	"Ended":   "latest activity first",
	"Started": "newest session first",
	"Msgs":    "most messages first",
	"Size":    "largest transcript first",
}

const sortMenuWidth = 44

// openSortMenu shows the menu with the current order highlighted.
func (m *Model) openSortMenu() {
	m.sortMenu = true
	m.sortSel = m.sortIdx
}

// applySort switches to order i and closes the menu.
func (m *Model) applySort(i int) {
	m.sortMenu = false
	if i == m.sortIdx {
		return
	}
	m.sortIdx = i
	m.refresh()
	m.cursor, m.offset = 0, 0
}

// updateSortMenu handles a key while the menu shows.
func (m Model) updateSortMenu(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k := msg.String(); k {
	case "down", "j", "ctrl+n", "tab":
		m.sortSel = (m.sortSel + 1) % len(sorts)
	case "up", "k", "ctrl+p", "shift+tab":
		m.sortSel = (m.sortSel + len(sorts) - 1) % len(sorts)
	case "enter", "space":
		m.applySort(m.sortSel)
	case "esc", "q":
		m.sortMenu = false
	default:
		if n := int(k[0] - '1'); len(k) == 1 && n >= 0 && n < len(sorts) {
			m.applySort(n)
			break
		}
		// It also closes on the key that opened it.
		if key.Matches(msg, m.km.Global.Sort) {
			m.sortMenu = false
		}
	}
	return m, nil
}

// sortMenuRect is where the menu is drawn: centered, a line per order
// between its borders.
func (m Model) sortMenuRect() rect {
	w := min(sortMenuWidth, m.width-2)
	h := len(sorts) + 2
	return rect{(m.width - w) / 2, max(0, (m.height-h)/2), w, h}
}

// sortMenuAt returns the order under a screen cell, or -1.
func (m Model) sortMenuAt(x, y int) int {
	r := m.sortMenuRect()
	if !r.contains(x, y) || y == r.y || y == r.y+r.h-1 {
		return -1
	}
	return y - r.y - 1
}

// withSortMenu lays the menu over the screen.
func (m Model) withSortMenu(screen string) string {
	r := m.sortMenuRect()
	if r.w < 24 {
		return screen
	}
	b := m.st.rule
	inner := r.w - 4
	title, hint := " Sort by ", " enter esc "
	fill := max(0, r.w-4-len(title)-len(hint))
	box := []string{b.Render("╭─") + m.st.key.Render(title) + b.Render(strings.Repeat("─", fill)) + m.st.muted.Render(hint) + b.Render("─╮")}
	for i, s := range sorts {
		mark := "  "
		if i == m.sortIdx {
			mark = "● "
		}
		name := fmt.Sprintf("%d %s%-8s", i+1, mark, s.name)
		line := " " + m.st.text.Render(name) + " " + m.st.muted.Render(sortNotes[s.name])
		if i == m.sortSel {
			line = m.st.bar.Render("▎") + m.st.on(m.st.key, true).Render(name) + m.st.selected.Render(" ") + m.st.on(m.st.muted, true).Render(sortNotes[s.name])
		}
		line = ansi.Truncate(line, inner, ellipsis)
		pad := strings.Repeat(" ", max(0, inner-ansi.StringWidth(line)))
		if i == m.sortSel {
			pad = m.st.selected.Render(pad)
		}
		box = append(box, b.Render("│ ")+line+pad+b.Render(" │"))
	}
	box = append(box, b.Render("╰"+strings.Repeat("─", r.w-2)+"╯"))
	lines := strings.Split(screen, "\n")
	for i, l := range box {
		if j := r.y + i; j < len(lines) {
			lines[j] = overlay(lines[j], l, r.x)
		}
	}
	return strings.Join(lines, "\n")
}
