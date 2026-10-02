package tui

import (
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// A column of the session list. Which columns appear, and how wide they are,
// depends only on the width available to the list, so every layout (detail
// pane below or to the right) shares this one table.
type column struct {
	header string
	// width returns the column width for a list this wide: 0 hides the
	// column, flex makes it take the space the others leave.
	width func(listWidth int) int
	right bool
	// cell renders the value. It must not exceed w cells; render truncates
	// anyway, but cells that know their content can do it more nicely.
	cell func(c cellCtx, r *row, w int) string
}

const (
	flex      = -1
	colGap    = 2
	minFlex   = 12
	ellipsis  = "…"
	worktreeM = "⌥"
)

type cellCtx struct {
	st  styles
	sel bool
	now time.Time
}

func (c cellCtx) style(s lipgloss.Style) lipgloss.Style { return c.st.on(s, c.sel) }

func at(min, w int) func(int) int {
	return func(lw int) int {
		if lw >= min {
			return w
		}
		return 0
	}
}

var columns = []column{
	{
		header: "Ended",
		width:  at(100, 11),
		cell: func(c cellCtx, r *row, w int) string {
			return c.style(c.st.dim).Render(formatEnded(r.s.EndedAt, c.now))
		},
	},
	{
		header: "Age",
		width: func(lw int) int {
			if lw < 100 {
				return 4
			}
			return 0
		},
		cell: func(c cellCtx, r *row, w int) string {
			return c.style(c.st.dim).Render(formatAge(r.s.EndedAt, c.now))
		},
	},
	{
		header: "Title",
		width:  func(int) int { return flex },
		cell: func(c cellCtx, r *row, w int) string {
			return c.style(lipgloss.NewStyle()).Render(ansi.Truncate(r.title, w, ellipsis))
		},
	},
	{
		header: "Folder",
		width: func(lw int) int {
			switch {
			case lw >= 120:
				return 32
			case lw >= 60:
				return 22
			}
			return 0
		},
		cell: func(c cellCtx, r *row, w int) string {
			base := c.style(lipgloss.NewStyle())
			if r.gone {
				base = c.style(c.st.warn)
			}
			if r.worktree == "" {
				return base.Render(ansi.Truncate(r.folder, w, ellipsis))
			}
			folder := ansi.Truncate(r.folder, w, ellipsis)
			rest := w - ansi.StringWidth(folder)
			if rest < 4 {
				return base.Render(folder)
			}
			suffix := ansi.Truncate(" "+worktreeM+" "+r.worktree, rest, ellipsis)
			return base.Render(folder) + c.style(c.st.dim).Render(suffix)
		},
	},
	{
		header: "Branch",
		width:  at(120, 24),
		cell: func(c cellCtx, r *row, w int) string {
			s := c.style(lipgloss.NewStyle())
			// A branch named after the worktree repeats the Folder column.
			if r.worktree != "" && strings.Contains(r.s.GitBranch, r.worktree) {
				s = c.style(c.st.dim)
			}
			return s.Render(ansi.Truncate(r.s.GitBranch, w, ellipsis))
		},
	},
	{
		header: "Msgs",
		width:  at(60, 5),
		right:  true,
		cell: func(c cellCtx, r *row, w int) string {
			return c.style(lipgloss.NewStyle()).Render(strconv.Itoa(r.s.MessageCount))
		},
	},
	{
		header: "Size",
		width:  at(110, 6),
		right:  true,
		cell: func(c cellCtx, r *row, w int) string {
			return c.style(lipgloss.NewStyle()).Render(formatSize(r.s.FileSize))
		},
	},
	{
		header: "ID",
		width:  func(int) int { return 8 },
		cell: func(c cellCtx, r *row, w int) string {
			return c.style(c.st.id).Render(r.s.ID[:min(8, len(r.s.ID))])
		},
	},
}

type placed struct {
	col   *column
	width int
}

// layoutColumns returns the visible columns and their widths for a list of
// the given width, one cell of padding on each side included.
func layoutColumns(listWidth int) []placed {
	inner := listWidth - 2
	var out []placed
	fixed, flexAt := 0, -1
	for i := range columns {
		w := columns[i].width(inner)
		if w == 0 {
			continue
		}
		if w == flex {
			flexAt = len(out)
		} else {
			fixed += w
		}
		out = append(out, placed{col: &columns[i], width: w})
	}
	if flexAt >= 0 {
		out[flexAt].width = max(minFlex, inner-fixed-colGap*(len(out)-1))
	}
	return out
}

// renderRow lays cells out at their widths, padded and aligned.
func renderRow(cols []placed, listWidth int, cells func(p placed) string, pad lipgloss.Style) string {
	var b strings.Builder
	b.WriteString(pad.Render(" "))
	used := 1
	for i, p := range cols {
		if i > 0 {
			b.WriteString(pad.Render(strings.Repeat(" ", colGap)))
			used += colGap
		}
		cell := ansi.Truncate(cells(p), p.width, ellipsis)
		gap := pad.Render(strings.Repeat(" ", max(0, p.width-ansi.StringWidth(cell))))
		if p.col.right {
			b.WriteString(gap)
			b.WriteString(cell)
		} else {
			b.WriteString(cell)
			b.WriteString(gap)
		}
		used += p.width
	}
	if rest := listWidth - used; rest > 0 {
		b.WriteString(pad.Render(strings.Repeat(" ", rest)))
	}
	return ansi.Truncate(b.String(), listWidth, "")
}
