package tui

import (
	"fmt"
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
	detailContent = 7  // lines inside the bottom detail pane
	detailFrame   = 2  // its top and bottom border
	detailWidth   = 52 // the pane on the right, border included
	minRightWidth = 100
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

// listHeight is the number of session rows that fit.
func (m Model) listHeight() int {
	h := m.height - 1 - tableChrome - footerLines
	if m.filterShown() {
		h--
	}
	if m.detailOpen && !m.detailRight() {
		h -= detailContent + detailFrame
	}
	return max(1, h)
}

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "recall"
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
		pane := m.renderDetail(detailWidth, len(table)-detailFrame)
		lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top, strings.Join(table, "\n"), pane))
	case m.detailOpen:
		lines = append(lines, table...)
		lines = append(lines, m.renderDetail(m.width, detailContent))
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

// wrap breaks s into lines at most w cells wide.
func wrap(s string, w int) []string {
	return strings.Split(lipgloss.NewStyle().Width(w).Render(s), "\n")
}

// renderDetail draws the selected session in a rounded pane width cells wide
// with exactly content lines inside.
func (m Model) renderDetail(width, content int) string {
	inner := max(10, width-m.st.border.GetHorizontalFrameSize())
	var lines []string
	if r := m.current(); r != nil {
		lines = append(lines, m.st.title.Render(ansi.Truncate(r.title, inner, ellipsis)))

		// The first prompt gets what the fixed lines below leave.
		promptLines := max(1, content-4)
		if first := cleanPrompt(r.s.FirstPrompt); first != r.title {
			wrapped := wrap(first, inner)
			if len(wrapped) > promptLines {
				wrapped = wrapped[:promptLines]
				wrapped[promptLines-1] = ansi.Truncate(wrapped[promptLines-1]+ellipsis, inner, ellipsis)
			}
			for _, l := range wrapped {
				lines = append(lines, m.st.strong.Render(l))
			}
		}
		for len(lines) < 1+promptLines {
			lines = append(lines, "")
		}

		name, badge := m.st.strong, m.st.worktree
		if r.gone {
			name, badge = m.st.gone, m.st.gone
		}
		folder := m.st.subtle.Render("Folder: ") + name.Render(r.folder)
		if r.worktree != "" {
			folder += " " + badge.Render(worktreeM+" "+r.worktree)
		}
		path := tildePath(r.s.ProjectPath, m.home)
		if r.gone {
			path += ", removed"
		}
		folder += m.st.muted.Render("  (" + path + ")")
		if r.mainRoot != "" {
			folder += m.st.muted.Render("  worktree of " + tildePath(r.mainRoot, m.home))
		}
		lines = append(lines, ansi.Truncate(folder, inner, ellipsis))

		meta := fmt.Sprintf("Branch: %s  Msgs: %d  Size: %s  Started: %s  Ended: %s", r.s.GitBranch, r.s.MessageCount,
			formatSize(r.s.FileSize), r.s.StartedAt.Local().Format("2006-01-02 15:04"), r.s.EndedAt.Local().Format("2006-01-02 15:04"))
		lines = append(lines, ansi.Truncate(m.st.muted.Render(meta), inner, ellipsis))
		lines = append(lines, m.st.muted.Render("ID: ")+m.st.id.Render(r.s.ID))
	}
	lines = fit(lines, content)
	return m.st.border.Width(width).Render(strings.Join(lines, "\n"))
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
	default:
		pairs = [][2]string{{"enter", "resume"}, {"space", "preview"}, {"y", "copy id"}, {"Y", "copy cmd"},
			{"tab", "detail"}, {"/", "filter"}, {"s", "sort"}, {"q", "quit"}}
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

// renderConversation formats preview messages for a terminal w cells wide.
func (m Model) renderConversation(p db.Preview, w int) string {
	// "claude 10/02 17:11" is the widest label.
	whoW := ansi.StringWidth("claude ") + ansi.StringWidth(formatEnded(m.now().AddDate(0, 0, -1), m.now())) + 1
	textW := max(20, w-whoW-2)
	var b strings.Builder
	one := func(msg db.Message) {
		who := m.st.claude.Render("claude")
		if msg.Role == "user" {
			who = m.st.user.Render("you")
		}
		label := who + " " + m.st.dim.Render(formatEnded(msg.Timestamp, m.now()))
		labelPad := strings.Repeat(" ", max(0, whoW-ansi.StringWidth(label)))
		for i, l := range wrap(strings.TrimSpace(msg.Content), textW) {
			l = m.st.strong.Render(l)
			if i == 0 {
				fmt.Fprintf(&b, " %s%s %s\n", label, labelPad, l)
			} else {
				fmt.Fprintf(&b, " %*s %s\n", whoW, "", l)
			}
		}
		b.WriteString("\n")
	}
	for _, msg := range p.Head {
		one(msg)
	}
	if p.Skipped > 0 {
		fmt.Fprintf(&b, " %s\n\n", m.st.muted.Render(fmt.Sprintf("··· %d messages skipped ···", p.Skipped)))
	}
	for _, msg := range p.Tail {
		one(msg)
	}
	if len(p.Head)+len(p.Tail) == 0 {
		fmt.Fprintf(&b, " %s\n", m.st.muted.Render("This session has no text messages."))
	}
	return b.String()
}
