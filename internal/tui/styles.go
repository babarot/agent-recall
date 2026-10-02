package tui

import (
	"charm.land/lipgloss/v2"
)

// styles holds every style the TUI renders with. They are rebuilt once the
// terminal reports whether its background is dark, so the selection and dim
// text read well on both.
type styles struct {
	dark bool

	title    lipgloss.Style // tool name in the header
	dim      lipgloss.Style
	bold     lipgloss.Style
	accent   lipgloss.Style
	id       lipgloss.Style
	warn     lipgloss.Style
	user     lipgloss.Style
	claude   lipgloss.Style
	toast    lipgloss.Style
	rule     lipgloss.Style
	selected lipgloss.Style // background of the selected row
}

func newStyles(dark bool) styles {
	ld := lipgloss.LightDark(dark)
	return styles{
		dark:     dark,
		title:    lipgloss.NewStyle().Bold(true).Foreground(ld(lipgloss.Color("#1f7a52"), lipgloss.Color("#6fd3a2"))),
		dim:      lipgloss.NewStyle().Foreground(ld(lipgloss.Color("#6b7570"), lipgloss.Color("#7d8a85"))),
		bold:     lipgloss.NewStyle().Bold(true),
		accent:   lipgloss.NewStyle().Foreground(ld(lipgloss.Color("#1f7a52"), lipgloss.Color("#6fd3a2"))),
		id:       lipgloss.NewStyle().Foreground(ld(lipgloss.Color("#2f62a8"), lipgloss.Color("#7fb4e6"))),
		warn:     lipgloss.NewStyle().Foreground(ld(lipgloss.Color("#a33a2c"), lipgloss.Color("#e8836a"))),
		user:     lipgloss.NewStyle().Bold(true).Foreground(ld(lipgloss.Color("#8a5a00"), lipgloss.Color("#e8c46a"))),
		claude:   lipgloss.NewStyle().Bold(true).Foreground(ld(lipgloss.Color("#1f7a52"), lipgloss.Color("#6fd3a2"))),
		toast:    lipgloss.NewStyle().Foreground(ld(lipgloss.Color("#1f7a52"), lipgloss.Color("#6fd3a2"))),
		rule:     lipgloss.NewStyle().Foreground(ld(lipgloss.Color("#d0d6d2"), lipgloss.Color("#2f3a37"))),
		selected: lipgloss.NewStyle().Background(ld(lipgloss.Color("#dcebe3"), lipgloss.Color("#22312c"))),
	}
}

// on returns s with the selected-row background when sel is true, so styled
// cells keep the highlight instead of resetting it.
func (st styles) on(s lipgloss.Style, sel bool) lipgloss.Style {
	if sel {
		return s.Background(st.selected.GetBackground())
	}
	return s
}
