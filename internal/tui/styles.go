package tui

import (
	"charm.land/lipgloss/v2"

	"github.com/babarot/claude-recall/internal/theme"
)

// styles holds every style the TUI renders with, built from one palette.
// The layout follows cc360: a header bar, ╌ rules around the column
// headers, an accent bar on the selected row and a rounded detail pane.
type styles struct {
	header   lipgloss.Style // the bar itself
	app      lipgloss.Style // "recall" on the bar
	tag      lipgloss.Style // the rest of the bar
	rule     lipgloss.Style
	colHdr   lipgloss.Style
	selected lipgloss.Style // background of the selected row
	bar      lipgloss.Style // ▎ on the selected row
	title    lipgloss.Style
	text     lipgloss.Style
	strong   lipgloss.Style
	subtle   lipgloss.Style
	muted    lipgloss.Style
	dim      lipgloss.Style
	id       lipgloss.Style
	worktree lipgloss.Style
	gone     lipgloss.Style // a removed folder or worktree
	key      lipgloss.Style // keys in the help line
	helpSep  lipgloss.Style
	filter   lipgloss.Style
	ok       lipgloss.Style
	warn     lipgloss.Style
	border   lipgloss.Style // the detail pane
	user     lipgloss.Style
	claude   lipgloss.Style
}

func fg(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }

func newStyles(p theme.Palette) styles {
	surface := lipgloss.Color(p.Surface)
	return styles{
		header:   lipgloss.NewStyle().Background(surface),
		app:      fg(p.Accent).Bold(true).Background(surface),
		tag:      fg(p.Dim).Background(surface),
		rule:     fg(p.Border),
		colHdr:   fg(p.Strong).Bold(true),
		selected: lipgloss.NewStyle().Foreground(lipgloss.Color(p.Text)).Background(surface),
		bar:      fg(p.Accent).Background(surface),
		title:    fg(p.Title).Bold(true),
		text:     lipgloss.NewStyle(),
		strong:   fg(p.Strong),
		subtle:   fg(p.Subtle),
		muted:    fg(p.Muted),
		dim:      fg(p.Dim),
		id:       fg(p.Accent),
		worktree: fg(p.Worktree),
		gone:     fg(p.Dim).Strikethrough(true),
		key:      fg(p.Accent).Bold(true),
		helpSep:  fg(p.Border),
		filter:   fg(p.Prompt),
		ok:       fg(p.OK),
		warn:     fg(p.Warn),
		border:   lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(p.Border)).Padding(0, 1),
		user:     fg(p.Prompt).Bold(true),
		claude:   fg(p.Title).Bold(true),
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
