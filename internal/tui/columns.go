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
	// minWidth is the narrowest list that shows the column.
	minWidth int
	// width is the column's width; flex columns share what the fixed ones
	// leave, Title taking two thirds and Folder one third.
	width int
	flex  int // share of the leftover space, 0 for a fixed column
	right bool
	cell  func(c cellCtx, r *row, w int) string
}

const (
	colGap    = 2
	rowIndent = 2 // "▎ " on the selected row, two spaces on the others
	minFlex   = 10
	ellipsis  = "…"
	worktreeM = "⌥"
)

type cellCtx struct {
	st  styles
	sel bool
	now time.Time
}

func (c cellCtx) style(s lipgloss.Style) lipgloss.Style { return c.st.on(s, c.sel) }

var columns = []column{
	{
		header: "Date", width: 14,
		cell: func(c cellCtx, r *row, w int) string {
			return c.style(c.st.subtle).Render(relativeDate(r.s.EndedAt, c.now))
		},
	},
	{
		header: "Title", flex: 2,
		cell: func(c cellCtx, r *row, w int) string {
			return c.style(c.st.title).Render(ansi.Truncate(r.title, w, ellipsis))
		},
	},
	{
		header: "Folder", flex: 1,
		cell: func(c cellCtx, r *row, w int) string {
			name, badge := c.style(c.st.text), c.style(c.st.worktree)
			if r.gone {
				name, badge = c.style(c.st.gone), c.style(c.st.gone)
			}
			folder := ansi.Truncate(r.folder, w, ellipsis)
			if r.worktree == "" {
				return name.Render(folder)
			}
			rest := w - ansi.StringWidth(folder) - 1
			if rest < 4 {
				return name.Render(folder)
			}
			return name.Render(folder) + c.style(c.st.text).Render(" ") +
				badge.Render(ansi.Truncate(worktreeM+" "+r.worktree, rest, ellipsis))
		},
	},
	{
		header: "Branch", width: 16, minWidth: 90,
		cell: func(c cellCtx, r *row, w int) string {
			return c.style(c.st.dim).Render(ansi.Truncate(r.s.GitBranch, w, ellipsis))
		},
	},
	{
		header: "Msgs", width: 5, minWidth: 100, right: true,
		cell: func(c cellCtx, r *row, w int) string {
			return c.style(c.st.text).Render(strconv.Itoa(r.s.MessageCount))
		},
	},
	{
		header: "Size", width: 6, minWidth: 110, right: true,
		cell: func(c cellCtx, r *row, w int) string {
			return c.style(c.st.subtle).Render(formatSize(r.s.FileSize))
		},
	},
	{
		header: "ID", width: 8, minWidth: 120,
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
// the given width.
func layoutColumns(listWidth int) []placed {
	inner := listWidth - rowIndent - 1
	var out []placed
	fixed, shares := 0, 0
	for i := range columns {
		c := &columns[i]
		if listWidth < c.minWidth {
			continue
		}
		fixed += c.width
		shares += c.flex
		out = append(out, placed{col: c, width: c.width})
	}
	rest := max(minFlex*shares, inner-fixed-colGap*(len(out)-1))
	given := 0
	for i := range out {
		if f := out[i].col.flex; f > 0 {
			out[i].width = max(minFlex, rest*f/shares)
			given += out[i].width
		}
	}
	// Rounding leftovers go to the first flex column (Title).
	for i := range out {
		if out[i].col.flex > 0 {
			out[i].width += max(0, rest-given)
			break
		}
	}
	return out
}

// renderRow lays cells out at their widths, padded and aligned, after the
// row indent. pad styles the spaces between and around cells.
func renderRow(cols []placed, listWidth int, indent string, cells func(p placed) string, pad lipgloss.Style) string {
	var b strings.Builder
	b.WriteString(indent)
	used := ansi.StringWidth(indent)
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
