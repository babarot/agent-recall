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
)

// detailRight reports whether the detail pane sits right of the list. Only
// this function knows the configured position; the rest of the view asks it.
func (m Model) detailRight() bool {
	// A spread Conversation always takes the full width below the list.
	if m.expanded || m.width < minRightWidth {
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
	return m.width - m.listLeft()
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
	if m.detailRight() {
		return 0
	}
	if m.expanded {
		return m.expandedPaneHeight()
	}
	return max(0, min(m.detailH, m.height-m.chromeLines()-minListRows))
}

// listHeight is the number of session rows that fit.
// rowLines is the lines each session takes in the list: two while it shows
// the sessions Claude found, with why under each.
func (m Model) rowLines() int {
	if m.asked != nil && m.cfg.AskReasons {
		return 2
	}
	return 1
}

// listRows is how many sessions the list shows at once.
func (m Model) listRows() int { return max(1, m.listHeight()/m.rowLines()) }

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
	if m.width == 0 || m.height == 0 || m.settling {
		return ""
	}
	if m.ask.stage != askClosed {
		return m.withAsk(m.renderScreen())
	}
	if m.helpOpen {
		return m.withHelp(m.renderScreen())
	}
	if m.sortMenu {
		return m.withSortMenu(m.renderScreen())
	}
	return m.renderScreen()
}

func (m Model) renderScreen() string {

	lines := []string{m.renderHeader()}
	if m.filterShown() {
		line := " " + m.st.filter.Render(m.filter.View())
		// The hinted key follows the cursor, faint, until tab takes it.
		if hint := m.keyHint(); hint != "" && m.mode == modeFilter {
			line = " " + m.st.filter.Render(strings.TrimRight(m.filter.View(), " ")) + m.st.dim.Render(hint)
		}
		lines = append(lines, line)
	}
	table := m.renderTable()
	if m.sidebarShown() {
		table = strings.Split(joinColumns(strings.Join(m.renderSidebar(len(table)), "\n"), strings.Join(table, "\n"), sidebarWidth, m.st.rule.Render("│")), "\n")
	}
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
	// Suggestions for the folder: term open just under the filter line.
	if r, list, idx, from, ok := m.suggestRect(); ok {
		for i, l := range m.suggestBox(list, idx, from, r.w) {
			if j := r.y + i; j < len(lines) {
				lines[j] = overlay(lines[j], l, r.x)
			}
		}
	}
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
	count := fmt.Sprintf("%d / %d sessions · ", len(m.visible), len(m.rows))
	sort := "sort: " + sorts[m.sortIdx].name
	// Where the list is narrowed to: folder: folders, else the folder scope.
	where := ""
	if q := parseQuery(m.filter.Value()); len(q.in) > 0 {
		in := m.inFolders(q)
		where = fmt.Sprintf("%d folders", len(in))
		if len(in) == 1 {
			where = in[0].name
		}
	} else if m.scope != "" {
		where = m.folderName(m.scope)
	}
	right := m.st.tag.Render(count)
	if m.asked != nil {
		where = ""
		room := m.width - 2 - ansi.StringWidth(left) - 1 - len(count) - len("asked:  · ") - len(sort)
		right += m.st.filter.Background(m.st.header.GetBackground()).Render("asked: "+ansi.Truncate(m.askedFor, max(8, room), ellipsis)) + m.st.tag.Render(" · ")
	}
	if m.searching() {
		right = m.st.tag.Render("searching… · ") + right
	}
	if where != "" {
		// The folder gives way first when the bar is short.
		room := m.width - 2 - ansi.StringWidth(left) - 1 - len(count) - len("in  · ") - len(sort)
		right += m.st.filter.Background(m.st.header.GetBackground()).Render("in "+middleEllipsis(where, max(8, room))) + m.st.tag.Render(" · ")
	}
	return m.bar(left, right+m.st.tag.Render(sort))
}

// renderTable returns the rules, column headers, exactly listHeight rows
// and the row count line.
func (m Model) renderTable() []string {
	lw := m.listWidth()
	cols := layoutColumns(lw)
	plain := lipgloss.NewStyle()

	scoped := m.scope != "" && len(parseQuery(m.filter.Value()).in) == 0
	head := renderRow(cols, lw, "  ", func(p placed) string {
		if scoped && p.col.header == folderHeader {
			return m.st.colHdr.Render(worktreeHeader)
		}
		return m.st.colHdr.Render(p.col.header)
	}, plain)
	// Beside the folder list or a spread Conversation, the focused side's
	// rules take the accent. Only the side with the focus marks its
	// selection with the bar; the row keeps its background so it still shows
	// which session the frames are about.
	rule := m.rule(lw)
	if (m.sidebarShown() || m.expanded) && m.focus == focusList {
		rule = m.st.id.Render(strings.Repeat("╌", lw))
		head = m.st.key.Render(ansi.Strip(head))
	}
	lines := []string{rule, head, rule}
	bar := m.st.bar.Render("▎")
	if m.focus != focusList {
		bar = m.st.selected.Render(" ")
	}

	h := m.listRows()
	now := m.now()
	end := min(len(m.visible), m.offset+h)
	for i := m.offset; i < end; i++ {
		r := &m.rows[m.visible[i]]
		sel := i == m.cursor
		ctx := cellCtx{st: m.st, sel: sel, now: now, scoped: scoped}
		pad, indent := plain, "  "
		if sel {
			pad = m.st.selected
			indent = bar + m.st.selected.Render(" ")
		}
		lines = append(lines, renderRow(cols, lw, indent, func(p placed) string { return p.col.cell(ctx, r, p.width) }, pad))
		if m.rowLines() == 2 {
			lines = append(lines, "  "+strings.Repeat(" ", 16)+m.reasonLine(r.s.ID, lw-20))
		}
	}
	if len(m.visible) == 0 {
		lines = append(lines, m.st.muted.Render("  No sessions match the filter. Esc clears it."))
	}
	h = m.listHeight()
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
	// Each pair is a key hint template (see hintKeys) and what it does;
	// esc and the keys of a box or a field being typed in are written as
	// they are, since they cannot be remapped.
	var pairs [][2]string
	switch m.mode {
	case modeFilter:
		pairs = [][2]string{{"enter", "apply"}, {"esc", "clear"}, {"↑↓", "move"}}
		if list, _ := m.suggestions(); len(list) > 0 {
			pairs = [][2]string{{"↑↓", "folder"}, {"enter", "pick"}, {"tab", "complete"}, {"esc", "close"}}
		} else if m.keyHint() != "" {
			pairs = append([][2]string{{"tab", "key " + m.keyHintKey()}}, pairs...)
		} else if _, _, _, _, ok := m.keyTerm(); !ok {
			pairs = append(pairs, [2]string{"folder: text: title: branch: worktree: id:", "one field"})
		}
	case modeList:
		if m.ask.stage != askClosed {
			pairs = map[askStage][][2]string{
				askTyping:   {{"enter", "ask"}, {"esc", "close"}},
				askRunning:  {{"esc", "cancel"}},
				askAnswered: {{"↑↓", "pick"}, {"enter", "open"}, {"f", "filter the list to these"}, {"r", "ask again"}, {"esc", "close"}},
				askFailed:   {{"r", "ask again"}, {"esc", "close"}},
			}[m.ask.stage]
			break
		}
		if m.focus == focusFolders && m.sideTyping {
			pairs = [][2]string{{"↑↓", "folder"}, {"enter", "done"}, {"esc", "clear"}}
			break
		}
		if m.focus == focusFolders {
			pairs = [][2]string{{"{up.0}{down.0}", "folder"}, {"{search.0}", "search"}, {"{folders.back.0}", "sessions"},
				{"{focus_next.0}", "next"}, {"{scope.0}", "this folder"}, {"{quit.0}", "quit"}}
			if m.sideSearch.Value() != "" {
				pairs[1] = [2]string{"esc", "clear search"}
			}
			break
		}
		if m.conv.typing {
			pairs = [][2]string{{"enter", "done"}, {"esc", "clear"}}
			break
		}
		if m.convSearching() && m.conv.input.Value() != "" {
			pairs = [][2]string{{"{next_match.0} {prev_match.0}", "next, previous"}, {"{search.0}", "search again"}, {"esc", "clear search"},
				{"{down.1} {up.1}", "scroll"}, {"{focus_next.0}", "sessions"}, {"{help.0}", "keys"}}
			break
		}
		if m.expanded && m.focus == focusConv {
			pairs = [][2]string{{"{down.1} {up.1}", "scroll"}, {"{search.0}", "search"}, {"{focus_next.0}", "sessions"},
				{"{read.0} {quit.0} esc", "close"}, {"{resume.0}", "resume"}, {"{copy_id.0}", "copy id"}, {"{help.0}", "keys"}}
			break
		}
		if m.expanded {
			pairs = [][2]string{{"{down.1} {up.1}", "next session"}, {"{focus_next.0}", "conversation"}, {"{read.0}", "close"},
				{"{resume.0}", "resume"}, {"{grow.0}/{shrink.0}", "resize"}, {"{help.0}", "keys"}}
			break
		}
		if m.focus != focusList {
			pairs = [][2]string{{"{up.0}{down.0}", "scroll " + strings.ToLower(frameTitles[m.focus])}, {"{focus_next.0}", "next"},
				{"esc", "back to list"}, {"{resume.0}", "resume"}, {"{copy_id.0}", "copy id"}, {"{quit.0}", "quit"}}
			break
		}
		fallthrough
	default:
		here := "this folder"
		if m.scope != "" && m.scope == m.startFolder {
			here = "all folders"
		}
		// ? goes early so a narrow terminal still shows where the rest are.
		pairs = [][2]string{{"{resume.0}", "resume"}, {"{read.0}", "read"}, {"{help.0}", "keys"}, {"{search.0}", "filter"},
			{"{scope.0}", here}, {"{list.folders_open.0}", "folders"}, {"{focus_next.0}", "focus"}, {"{copy_id.0}", "copy id"},
			{"{copy_command.0}", "copy cmd"}, {"{grow.0}/{shrink.0}", "resize"}, {"{sort.0}", "sort"}, {"{quit.0}", "quit"}}
		if m.sidebarShown() {
			pairs[5] = [2]string{"{list.folders_close.0}", "close folders"}
		}
	}
	var parts []string
	for _, p := range pairs {
		// A hint whose keys were all remapped away is left out.
		if k := m.hintKeys(p[0]); k != "" {
			parts = append(parts, m.st.key.Render(k)+" "+m.st.muted.Render(p[1]))
		}
	}
	return " " + ansi.Truncate(strings.Join(parts, m.st.helpSep.Render(" · ")), m.width-2, ellipsis)
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
	return m.renderParts([]convPart{{msgs: p.Head}, {skipped: p.Skipped, msgs: p.Tail}}, 0, w)
}

// convPart is a run of messages shown together, after skipped ones that are
// left out.
type convPart struct {
	skipped int
	msgs    []db.Message
}

// renderParts formats runs of messages, a marker for the skipped ones
// before each and after the last (after of them), as renderConversation
// does.
func (m Model) renderParts(parts []convPart, after, w int) string {
	var b strings.Builder
	when := func(msg db.Message) string { return m.st.dim.Render(formatEnded(msg.Timestamp, m.now())) }
	user := func(msg db.Message) {
		bw := max(20, w-2) // box width, one cell of margin each side
		inner := bw - 4
		title := m.st.user.Render("you") + " " + when(msg)
		fill := max(0, bw-5-ansi.StringWidth(title))
		border := m.st.filter
		fmt.Fprintf(&b, " %s%s%s\n", border.Render("╭─ "), title, border.Render(" "+strings.Repeat("─", fill)+"╮"))
		for _, l := range m.messageBody(msg, inner) {
			pad := strings.Repeat(" ", max(0, inner-ansi.StringWidth(l.s)))
			if !l.image {
				l.s = m.st.strong.Bold(true).Render(l.s)
			}
			fmt.Fprintf(&b, " %s %s%s %s\n", border.Render("│"), l.s, pad, border.Render("│"))
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
	// skip marks n skipped messages between prev and next, either of which
	// may be missing.
	var last []db.Message
	skip := func(n int, next []db.Message) {
		fmt.Fprintf(&b, " %s\n\n", m.skipLine(db.Preview{Head: last, Tail: next, Skipped: n}, w-2))
	}
	shown := 0
	for _, part := range parts {
		if part.skipped > 0 {
			skip(part.skipped, part.msgs)
		}
		for _, msg := range part.msgs {
			one(msg)
			last = []db.Message{msg}
		}
		shown += len(part.msgs)
	}
	if after > 0 {
		skip(after, nil)
	}
	if shown == 0 {
		fmt.Fprintf(&b, " %s\n", m.st.muted.Render("This session has no text messages."))
	}
	return b.String()
}
