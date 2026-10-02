package tui

import (
	"fmt"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/babarot/claude-recall/internal/config"
	"github.com/babarot/claude-recall/internal/db"
)

const (
	tableChrome   = 4  // rule, column headers, rule, the row count line
	footerLines   = 2  // status line, key help
	detailWidth   = 56 // the pane on the right
	minRightWidth = 100
	minListRows   = 3 // the pane below never squeezes the list further
	previewChrome = 5 // header bar, rule, folder line, status, help
)

// detailRight reports whether the detail pane sits right of the list. Only
// this function knows the configured position; the rest of the view asks it.
func (m Model) detailRight() bool {
	if !m.detailOpen || m.width < minRightWidth {
		return false
	}
	switch m.cfg.DetailPosition {
	case config.DetailRight:
		return true
	case config.DetailAuto:
		return m.width >= m.cfg.DetailAutoWidth
	}
	return false
}

func (m Model) listWidth() int {
	if m.detailRight() {
		return m.width - detailWidth
	}
	return m.width
}

func (m Model) filterShown() bool { return m.mode == modeFilter || m.filter.Value() != "" }

// chromeLines is everything but the list rows and the pane below.
func (m Model) chromeLines() int {
	n := 1 + tableChrome + footerLines
	if m.filterShown() {
		n++
	}
	return n
}

// paneHeight is the height of the detail pane below the list: the chosen
// height, cut so the list keeps a few rows.
func (m Model) paneHeight() int {
	if !m.detailOpen || m.detailRight() {
		return 0
	}
	return max(0, min(m.detailH, m.height-m.chromeLines()-minListRows))
}

// listHeight is the number of session rows that fit.
func (m Model) listHeight() int {
	return max(1, m.height-m.chromeLines()-m.paneHeight())
}

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "recall"
	// Mouse: drag the detail pane's top edge to resize it, wheel to scroll.
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m Model) render() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	if m.mode == modePreview {
		return m.renderPreview()
	}

	lines := []string{m.renderHeader()}
	if m.filterShown() {
		lines = append(lines, " "+m.st.filter.Render(m.filter.View()))
	}
	table := m.renderTable()
	switch {
	case m.detailRight():
		lines = append(lines, joinColumns(strings.Join(table, "\n"), m.renderPane(), m.listWidth(), " "))
	case m.paneHeight() > 0:
		lines = append(lines, table...)
		lines = append(lines, m.renderPane())
	default:
		lines = append(lines, table...)
	}
	lines = append(lines, m.renderStatus(), m.renderHelp())
	return strings.Join(lines, "\n")
}

func (m Model) rule(w int) string { return m.st.rule.Render(strings.Repeat("╌", w)) }

// bar renders the header bar: left and right text on the surface color.
func (m Model) bar(left, right string) string {
	gap := m.width - 2 - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		right = ""
		gap = max(0, m.width-2-ansi.StringWidth(left))
	}
	s := m.st.header.Render(" ") + left + m.st.header.Render(strings.Repeat(" ", gap)) + right + m.st.header.Render(" ")
	return ansi.Truncate(s, m.width, "")
}

func (m Model) renderHeader() string {
	left := m.st.app.Render("recall") + m.st.tag.Render(" // claude-recall")
	right := m.st.tag.Render(fmt.Sprintf("%d / %d sessions · sort: %s", len(m.visible), len(m.rows), sorts[m.sortIdx].name))
	return m.bar(left, right)
}

// renderTable returns the rules, column headers, exactly listHeight rows
// and the row count line.
func (m Model) renderTable() []string {
	lw := m.listWidth()
	cols := layoutColumns(lw)
	plain := lipgloss.NewStyle()

	head := renderRow(cols, lw, "  ", func(p placed) string { return m.st.colHdr.Render(p.col.header) }, plain)
	lines := []string{m.rule(lw), head, m.rule(lw)}

	h := m.listHeight()
	now := m.now()
	end := min(len(m.visible), m.offset+h)
	for i := m.offset; i < end; i++ {
		r := &m.rows[m.visible[i]]
		sel := i == m.cursor
		ctx := cellCtx{st: m.st, sel: sel, now: now}
		pad, indent := plain, "  "
		if sel {
			pad = m.st.selected
			indent = m.st.bar.Render("▎") + m.st.selected.Render(" ")
		}
		lines = append(lines, renderRow(cols, lw, indent, func(p placed) string { return p.col.cell(ctx, r, p.width) }, pad))
	}
	if len(m.visible) == 0 {
		lines = append(lines, m.st.muted.Render("  No sessions match the filter. Esc clears it."))
	}
	for len(lines) < 3+h {
		lines = append(lines, "")
	}
	info := fmt.Sprintf("  %d sessions", len(m.visible))
	if more := len(m.visible) - end; more > 0 {
		info += fmt.Sprintf(" · ↓ %d more", more)
	}
	return append(lines[:3+h], m.st.muted.Render(info))
}

// wrap breaks s into lines at most w cells wide, at spaces where it can,
// mid-word where it must; wide characters count as two cells.
func wrap(s string, w int) []string {
	w = max(1, w)
	var out []string
	for _, l := range strings.Split(ansi.Wrap(s, w, ""), "\n") {
		// Wrap can leave the space it broke at on the line, and a break at
		// a hyphen can overshoot; trim, then hard-wrap what is still wide.
		l = strings.TrimRight(l, " ")
		if ansi.StringWidth(l) > w {
			out = append(out, strings.Split(ansi.Hardwrap(l, w, true), "\n")...)
			continue
		}
		out = append(out, l)
	}
	return out
}

var listMarker = regexp.MustCompile(`^(\s*)([-*+]|\d+[.)])\s+`)

// wrapText wraps text line by line, keeping indentation: a line that
// wraps continues under where its text starts, past any list marker.
func wrapText(s string, w int) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if m := listMarker.FindString(line); m != "" {
			indent = len(m)
		}
		if indent >= w/2 {
			indent = 0
		}
		if ansi.StringWidth(line) <= w {
			out = append(out, line)
			continue
		}
		parts := wrap(line, w)
		out = append(out, parts[0])
		if len(parts) > 1 {
			rest := strings.TrimSpace(strings.Join(parts[1:], " "))
			for _, l := range wrap(rest, w-indent) {
				out = append(out, strings.Repeat(" ", indent)+l)
			}
		}
	}
	return out
}

// fit pads or cuts lines to exactly n.
func fit(lines []string, n int) []string {
	if len(lines) > n {
		return lines[:n]
	}
	for len(lines) < n {
		lines = append(lines, "")
	}
	return lines
}

func (m Model) renderStatus() string {
	if m.toast == "" {
		return ""
	}
	s := m.st.subtle
	switch m.toastKind {
	case toastOK:
		s = m.st.ok
	case toastWarn:
		s = m.st.warn
	}
	return " " + s.Render(ansi.Truncate(m.toast, m.width-2, ellipsis))
}

func (m Model) renderHelp() string {
	var pairs [][2]string
	switch m.mode {
	case modeFilter:
		pairs = [][2]string{{"enter", "apply"}, {"esc", "clear"}, {"↑↓", "move"}}
	case modePreview:
		pairs = [][2]string{{"space", "back"}, {"↑↓", "scroll"}, {"enter", "resume"}, {"y", "copy id"}, {"Y", "copy cmd"}}
	case modeList:
		if m.focus != focusList {
			pairs = [][2]string{{"↑↓", "scroll " + strings.ToLower(frameTitles[m.focus])}, {"[ ]", "next frame"},
				{"esc", "back to list"}, {"enter", "resume"}, {"y", "copy id"}, {"q", "quit"}}
			break
		}
		fallthrough
	default:
		pairs = [][2]string{{"enter", "resume"}, {"space", "preview"}, {"y", "copy id"}, {"Y", "copy cmd"},
			{"tab", "detail"}, {"+/-", "resize"}, {"/", "filter"}, {"s", "sort"}, {"q", "quit"}}
	}
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = m.st.key.Render(p[0]) + " " + m.st.muted.Render(p[1])
	}
	return " " + ansi.Truncate(strings.Join(parts, m.st.helpSep.Render(" · ")), m.width-2, ellipsis)
}

func (m Model) previewRow() *row {
	for i := range m.rows {
		if m.rows[i].s.ID == m.previewFor {
			return &m.rows[i]
		}
	}
	return nil
}

func (m Model) renderPreview() string {
	r := m.previewRow()
	if r == nil {
		return ""
	}
	left := m.st.app.Render("recall") + m.st.tag.Render(" // ") + m.st.title.Background(m.st.header.GetBackground()).Render(r.title)
	right := m.st.tag.Render(fmt.Sprintf("%s · %d msgs · %s", r.s.ID[:min(8, len(r.s.ID))], r.s.MessageCount, formatSize(r.s.FileSize)))
	where := " " + m.st.subtle.Render(r.folder)
	if r.worktree != "" {
		where += " " + m.st.worktree.Render(worktreeM+" "+r.worktree)
	}
	where += m.st.dim.Render("  " + r.s.GitBranch)
	return strings.Join([]string{m.bar(ansi.Truncate(left, m.width-2-ansi.StringWidth(right)-1, ellipsis), right),
		m.rule(m.width), ansi.Truncate(where, m.width, ellipsis), m.preview.View(), m.renderStatus(), m.renderHelp()}, "\n")
}

// skipLine marks the messages the preview leaves out: a rule across w
// cells with the count, and the time span skipped, in its middle.
func (m Model) skipLine(p db.Preview, w int) string {
	label := m.st.id.Bold(true).Render(fmt.Sprintf("%d messages skipped", p.Skipped))
	if len(p.Head) > 0 && len(p.Tail) > 0 {
		from, to := p.Head[len(p.Head)-1].Timestamp, p.Tail[0].Timestamp
		if !from.IsZero() && !to.IsZero() {
			label += m.st.muted.Render(fmt.Sprintf(" · %s → %s (%s)",
				from.Local().Format("15:04"), to.Local().Format("15:04"), durationText(to.Sub(from))))
		}
	}
	label = " " + label + " "
	rest := max(0, w-ansi.StringWidth(label))
	return m.st.rule.Render(strings.Repeat("─", rest/2)) + label + m.st.rule.Render(strings.Repeat("─", rest-rest/2))
}

// renderConversation formats preview messages for a terminal w cells wide.
// The user's messages sit in a box with the speaker and time on its top
// edge, in bold; Claude's carry a rail down their left side, in a softer
// color, so long replies read as one block.
func (m Model) renderConversation(p db.Preview, w int) string {
	var b strings.Builder
	when := func(msg db.Message) string { return m.st.dim.Render(formatEnded(msg.Timestamp, m.now())) }
	user := func(msg db.Message) {
		bw := max(20, w-2) // box width, one cell of margin each side
		inner := bw - 4
		title := m.st.user.Render("you") + " " + when(msg)
		fill := max(0, bw-5-ansi.StringWidth(title))
		border := m.st.filter
		fmt.Fprintf(&b, " %s%s%s\n", border.Render("╭─ "), title, border.Render(" "+strings.Repeat("─", fill)+"╮"))
		for _, l := range wrapText(strings.TrimSpace(msg.Content), inner) {
			pad := strings.Repeat(" ", max(0, inner-ansi.StringWidth(l)))
			fmt.Fprintf(&b, " %s %s%s %s\n", border.Render("│"), m.st.strong.Bold(true).Render(l), pad, border.Render("│"))
		}
		fmt.Fprintf(&b, " %s\n", border.Render("╰"+strings.Repeat("─", bw-2)+"╯"))
	}
	claude := func(msg db.Message) {
		rail := m.st.claude.UnsetBold().Render("▎")
		fmt.Fprintf(&b, " %s %s %s\n", rail, m.st.claude.Render("claude"), when(msg))
		for _, l := range wrapText(strings.TrimSpace(msg.Content), max(20, w-4)) {
			fmt.Fprintf(&b, " %s %s\n", rail, m.st.subtle.Render(l))
		}
	}
	one := func(msg db.Message) {
		if msg.Role == "user" {
			user(msg)
		} else {
			claude(msg)
		}
		b.WriteString("\n")
	}
	for _, msg := range p.Head {
		one(msg)
	}
	if p.Skipped > 0 {
		fmt.Fprintf(&b, " %s\n\n", m.skipLine(p, w-2))
	}
	for _, msg := range p.Tail {
		one(msg)
	}
	if len(p.Head)+len(p.Tail) == 0 {
		fmt.Fprintf(&b, " %s\n", m.st.muted.Render("This session has no text messages."))
	}
	return b.String()
}
