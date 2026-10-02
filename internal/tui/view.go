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
	headerLines   = 2 // title line, column headers
	footerLines   = 2 // rule, key help
	detailLines   = 9 // bottom pane body, below its rule
	detailWidth   = 48
	minRightWidth = 100 // narrower terminals always get the pane below
	previewChrome = 4   // preview header, rule, rule, key help
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

func (m Model) listHeight() int {
	h := m.height - headerLines - footerLines
	if m.detailOpen && !m.detailRight() {
		h -= detailLines + 1
	}
	return max(1, h)
}

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "claude-recall"
	return v
}

func (m Model) render() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	if m.mode == modePreview {
		return m.renderPreview()
	}

	list := m.renderList()
	var body string
	switch {
	case m.detailRight():
		pane := lipgloss.NewStyle().
			Width(detailWidth - 1).
			Height(len(list)).
			BorderLeft(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(m.st.rule.GetForeground()).
			Render(strings.Join(m.renderDetail(detailWidth-3, true), "\n"))
		body = lipgloss.JoinHorizontal(lipgloss.Top, strings.Join(list, "\n"), pane)
	case m.detailOpen:
		lines := append(list, m.rule())
		lines = append(lines, fit(m.renderDetail(m.width-2, false), detailLines)...)
		body = strings.Join(lines, "\n")
	default:
		body = strings.Join(list, "\n")
	}
	return strings.Join([]string{m.renderHeader(), body, m.rule(), m.renderFooter()}, "\n")
}

func (m Model) rule() string {
	return m.st.rule.Render(strings.Repeat("─", m.width))
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

func (m Model) renderHeader() string {
	if m.mode == modeFilter || m.filter.Value() != "" {
		return " " + m.filter.View() + "  " + m.st.dim.Render(fmt.Sprintf("%d matches", len(m.visible)))
	}
	return " " + m.st.title.Render("claude-recall") + "  " +
		m.st.dim.Render(fmt.Sprintf("%d / %d sessions · sort: %s", len(m.visible), len(m.rows), sorts[m.sortIdx].name))
}

// renderList returns the column header and exactly listHeight rows.
func (m Model) renderList() []string {
	lw := m.listWidth()
	cols := layoutColumns(lw)
	plain := lipgloss.NewStyle()

	head := renderRow(cols, lw, func(p placed) string { return m.st.dim.Render(p.col.header) }, plain)
	lines := []string{head}

	h := m.listHeight()
	now := m.now()
	for i := m.offset; i < min(len(m.visible), m.offset+h); i++ {
		r := &m.rows[m.visible[i]]
		sel := i == m.cursor
		ctx := cellCtx{st: m.st, sel: sel, now: now}
		pad := plain
		if sel {
			pad = m.st.selected
		}
		lines = append(lines, renderRow(cols, lw, func(p placed) string { return p.col.cell(ctx, r, p.width) }, pad))
	}
	if len(m.visible) == 0 {
		lines = append(lines, m.st.dim.Render(" No sessions match the filter. Esc clears it."))
	}
	return fit(lines, h+1)
}

// renderDetail describes the selected session in lines at most w wide. The
// pane on the right has room to wrap the first prompt.
func (m Model) renderDetail(w int, tall bool) []string {
	r := m.current()
	if r == nil {
		return nil
	}
	const labelW = 12
	valW := max(10, w-labelW)
	kv := func(label, value string) string {
		return " " + m.st.dim.Render(fmt.Sprintf("%-*s", labelW, label)) + ansi.Truncate(value, valW, ellipsis)
	}

	folder := tildePath(r.s.ProjectPath, m.home)
	if r.gone {
		folder = m.st.warn.Render(folder + " (gone)")
	}
	lines := []string{
		" " + m.st.bold.Render(ansi.Truncate(r.title, w, ellipsis)),
		kv("ID", m.st.id.Render(r.s.ID)),
		kv("Folder", folder),
	}
	if r.mainRoot != "" {
		lines = append(lines, kv("Worktree of", tildePath(r.mainRoot, m.home)))
	}
	lines = append(lines,
		kv("Branch", r.s.GitBranch),
		kv("Time", fmt.Sprintf("%s → %s  %s", formatEnded(r.s.StartedAt, m.now()), formatEnded(r.s.EndedAt, m.now()),
			m.st.dim.Render("("+formatDuration(r.s.EndedAt.Sub(r.s.StartedAt))+")"))),
		kv("Size", fmt.Sprintf("%d msgs · %s", r.s.MessageCount, formatSize(r.s.FileSize))),
	)
	first := cleanPrompt(r.s.FirstPrompt)
	if tall {
		wrapped := strings.Split(lipgloss.NewStyle().Width(valW).Render(first), "\n")
		wrapped = fit(wrapped, min(len(wrapped), 6))
		lines = append(lines, kv("First", m.st.dim.Render(wrapped[0])))
		for _, l := range wrapped[1:] {
			lines = append(lines, " "+strings.Repeat(" ", labelW)+m.st.dim.Render(l))
		}
	} else {
		lines = append(lines, kv("First", m.st.dim.Render(ansi.Truncate(first, valW, ellipsis))))
	}
	lines = append(lines, kv("Resume", m.st.accent.Render(resumeCommand(r))))
	return lines
}

func (m Model) renderFooter() string {
	if m.toast != "" {
		return " " + m.st.toast.Render(ansi.Truncate(m.toast, m.width-2, ellipsis))
	}
	var help string
	switch m.mode {
	case modeFilter:
		help = "type to filter  ↑↓ move  enter apply  esc clear"
	case modePreview:
		help = "space/esc back  ↑↓ scroll  enter resume  y copy id  Y copy cmd"
	default:
		help = "↑↓/jk move  enter resume  space preview  y copy id  Y copy cmd  tab detail  / filter  s sort  q quit"
	}
	return " " + m.st.dim.Render(ansi.Truncate(help, m.width-2, ellipsis))
}

func (m Model) renderPreview() string {
	var r *row
	for i := range m.rows {
		if m.rows[i].s.ID == m.previewFor {
			r = &m.rows[i]
			break
		}
	}
	head := ""
	if r != nil {
		where := r.folder
		if r.worktree != "" {
			where += " " + worktreeM + " " + r.worktree
		}
		meta := fmt.Sprintf("%s · %d msgs · %s · %s", r.s.ID[:min(8, len(r.s.ID))], r.s.MessageCount, formatSize(r.s.FileSize), where)
		head = " " + m.st.title.Render(r.title) + "  " + m.st.dim.Render(meta)
		head = ansi.Truncate(head, m.width, ellipsis)
	}
	return strings.Join([]string{head, m.rule(), m.preview.View(), m.rule(), m.renderFooter()}, "\n")
}

// renderConversation formats preview messages for a terminal w cells wide.
func (m Model) renderConversation(p db.Preview, w int) string {
	const whoW = 14
	textW := max(20, w-whoW-2)
	var b strings.Builder
	one := func(msg db.Message) {
		who := m.st.claude.Render("claude")
		if msg.Role == "user" {
			who = m.st.user.Render("you")
		}
		label := who + " " + m.st.dim.Render(formatEnded(msg.Timestamp, m.now()))
		labelPad := strings.Repeat(" ", max(0, whoW-ansi.StringWidth(label)))
		body := strings.TrimSpace(msg.Content)
		lines := strings.Split(lipgloss.NewStyle().Width(textW).Render(body), "\n")
		for i, l := range lines {
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
		fmt.Fprintf(&b, " %s\n\n", m.st.dim.Render(fmt.Sprintf("··· %d messages skipped ···", p.Skipped)))
	}
	for _, msg := range p.Tail {
		one(msg)
	}
	if len(p.Head)+len(p.Tail) == 0 {
		fmt.Fprintf(&b, " %s\n", m.st.dim.Render("This session has no text messages."))
	}
	return b.String()
}
