package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/babarot/claude-recall/internal/db"
)

// The detail pane is three frames: Conversation (how the session began and
// where it left off), What was done (activity, tools, edited files,
// commands) and Details (times, counts, IDs). Below the list, the first two
// share the left column and Details runs down the right; beside the list,
// all three stack. Each frame scrolls on its own, with the mouse wheel over
// it or the keys once it has focus.

// focus is what keys and the wheel act on: the session list or a frame.
type focus int

const (
	focusList focus = iota
	focusConv
	focusDone
	focusDetails
	numFocus
)

var frameTitles = [numFocus]string{"", "Conversation", "What was done", "Details"}

const (
	// The Details frame below the list, border included: wide enough for a
	// full session ID after its label.
	detailsWidth = 4 + labelWidth + 36
	labelWidth   = 9 // "Commands " and friends
	minFrame     = 5 // smallest frame: borders plus three lines
	sparkChars   = "▁▂▃▄▅▆▇█"
)

// rect is a frame's place on the screen.
type rect struct{ x, y, w, h int }

func (r rect) contains(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

// paneRects returns where the three frames are drawn, indexed by focus, or
// false when the pane is hidden. Drawing and mouse hit tests both use it.
func (m Model) paneRects() ([numFocus]rect, bool) {
	var out [numFocus]rect
	r := m.current()
	if r == nil || !m.detailOpen {
		return out, false
	}
	content := m.frameContent(r, 0)
	need := func(f focus) int { return len(content[f].pinned) + len(content[f].scroll) + 2 }

	if m.detailRight() {
		x, w := m.listWidth()+1, detailWidth-1
		top := 1
		if m.filterShown() {
			top++
		}
		h := m.listHeight() + tableChrome
		detailsH := min(need(focusDetails), max(minFrame, h/3))
		convH, doneH := split(h-detailsH, need(focusConv), need(focusDone))
		out[focusConv] = rect{x, top, w, convH}
		out[focusDone] = rect{x, top + convH, w, doneH}
		out[focusDetails] = rect{x, top + convH + doneH, w, detailsH}
		return out, true
	}

	h := m.paneHeight()
	if h == 0 {
		return out, false
	}
	top := m.paneTop()
	rightW := max(30, min(detailsWidth, m.width-70))
	leftW := m.width - rightW - 1
	convH, doneH := split(h, need(focusConv), need(focusDone))
	out[focusConv] = rect{0, top, leftW, convH}
	out[focusDone] = rect{0, top + convH, leftW, doneH}
	out[focusDetails] = rect{leftW + 1, top, rightW, h}
	return out, true
}

// split shares h lines between two stacked frames that would like a and b
// lines: each gets what it needs if that fits, else the second, What was
// done, gets what it needs up to half and Conversation the rest.
func split(h, a, b int) (int, int) {
	if a+b <= h {
		return h - b, b
	}
	second := max(minFrame, min(b, h/2))
	return max(0, h-second), second
}

// frameLines is a frame's content: pinned lines stay at the top, the rest
// scrolls. fromBottom frames start scrolled to their end.
type frameLines struct {
	pinned, scroll []string
	fromBottom     bool
	// keep is a scrolling line to show even when it is above the newest
	// ones, as long as the frame is not scrolled: the last thing the user
	// said. -1 for none.
	keep int
	// gap, when set, marks the messages skipped between the pinned lines
	// and the visible ones: before is how many come before the first
	// scrolling line, and gap renders the marker for a count.
	gap    func(n int) string
	before int
	// notMessages are scrolling lines that are not messages (day lines),
	// left out of the skipped count.
	notMessages map[int]bool
}

// messagesBefore counts the messages before scrolling line i.
func (c frameLines) messagesBefore(i int) int {
	n := c.before + i
	for j := range c.notMessages {
		if j < i {
			n--
		}
	}
	return n
}

// frameContent builds each frame's lines. inner is the What was done
// frame's inner width, for aligning columns; 0 when only counting lines.
func (m Model) frameContent(r *row, inner int) [numFocus]frameLines {
	d := m.details[r.s.ID]
	var out [numFocus]frameLines
	out[focusConv] = m.conversationContent(r, d)
	out[focusDone] = m.doneContent(r, d, inner)
	out[focusDetails] = frameLines{scroll: m.detailsLines(r, d), keep: -1}
	return out
}

// window returns the visible lines of a frame with n lines of room, the
// clamped scroll offset, the index of the first visible scrolling line and
// how many there are. The offset counts from the top, or from the bottom for
// fromBottom frames.
func window(c frameLines, n, offset int) (lines []string, off, first, total int) {
	room := max(0, n-len(c.pinned))
	total = len(c.scroll)
	off = max(0, min(offset, total-room))
	start := off
	if c.fromBottom {
		start = max(0, total-room-off)
	}
	end := min(total, start+room)
	from := start // first scrolling line shown after any gap marker
	if c.gap != nil && room >= 2 && c.messagesBefore(start) > 0 && end-start == room {
		// Give a row to the marker: the oldest shown line, unless that is
		// the very first one, then the newest.
		if start > 0 {
			from++
		} else {
			end--
		}
	}
	visible := c.scroll[from:end]
	skipped := c.messagesBefore(from)
	if c.fromBottom && off == 0 && c.keep >= 0 && c.keep < from && len(visible) >= 2 {
		// Show the last user message even though newer ones fill the frame.
		visible = append([]string{c.scroll[c.keep]}, visible[1:]...)
		skipped = c.messagesBefore(c.keep)
	}
	if c.gap != nil && skipped > 0 && room >= 2 {
		visible = append([]string{c.gap(skipped)}, visible...)
	}
	return append(append([]string{}, c.pinned...), visible...), off, start, total
}

// frame draws lines inside a rounded border w cells wide and h lines tall,
// with title set into the top edge and, when the content scrolls, the
// visible range in the bottom edge.
func (m Model) frame(title string, lines []string, w, h int, focused bool, scrollInfo string) string {
	if w < 6 || h < 2 {
		return ""
	}
	border, name := m.st.rule, m.st.subtle.Bold(true)
	if focused {
		border, name = m.st.id, m.st.key
	}
	inner := w - 4
	fill := max(0, w-5-ansi.StringWidth(title))
	out := []string{border.Render("╭─ ") + name.Render(title) + border.Render(" "+strings.Repeat("─", fill)+"╮")}
	for _, l := range fit(lines, h-2) {
		l = ansi.Truncate(l, inner, ellipsis)
		pad := strings.Repeat(" ", max(0, inner-ansi.StringWidth(l)))
		out = append(out, border.Render("│")+" "+l+pad+" "+border.Render("│"))
	}
	bottom := border.Render("╰" + strings.Repeat("─", w-2) + "╯")
	if iw := ansi.StringWidth(scrollInfo); scrollInfo != "" && w > iw+6 {
		bottom = border.Render("╰"+strings.Repeat("─", w-5-iw)+" ") + m.st.muted.Render(scrollInfo) + border.Render(" ─╯")
	}
	return strings.Join(append(out, bottom), "\n")
}

// renderFrame draws frame f of the selected session at r.
func (m Model) renderFrame(f focus, c frameLines, r rect) string {
	lines, _, first, total := window(c, r.h-2, m.scroll[f])
	info := ""
	if room := r.h - 2 - len(c.pinned); total > room && room > 0 {
		info = fmt.Sprintf("%d-%d/%d", first+1, min(total, first+room), total)
	}
	return m.frame(frameTitles[f], lines, r.w, r.h, m.focus == f, info)
}

func durationText(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	h, mins := int(d.Hours()), int(d.Minutes())%60
	if h == 0 {
		return fmt.Sprintf("%dm", mins)
	}
	return fmt.Sprintf("%dh %dm", h, mins)
}

func (m Model) section(s string) string { return m.st.subtle.Bold(true).Render(strings.ToUpper(s)) }

// messageLine is one message of the conversation: time, the speaker in its
// color, the text. The user's own words are bold.
func (m Model) messageLine(msg db.Message) string {
	when := "     "
	if !msg.Timestamp.IsZero() {
		when = msg.Timestamp.Local().Format("15:04")
	}
	if msg.Role == "user" {
		return m.st.dim.Render(when) + " " + m.st.user.Render("you   ") + " " + m.st.strong.Bold(true).Render(collapse(msg.Content))
	}
	return m.st.dim.Render(when) + " " + m.st.claude.Render("claude") + " " + m.st.subtle.Render(collapse(msg.Content))
}

func (m Model) dayLine(t time.Time) string {
	return m.st.dim.Render("── " + t.Local().Format("Mon Jan 2") + " ──")
}

func (m Model) headLine(r *row) string {
	return m.st.title.Render(r.title) + m.st.muted.Render(fmt.Sprintf("  %s · %d msgs · %s",
		durationText(r.s.EndedAt.Sub(r.s.StartedAt)), r.s.MessageCount, formatSize(r.s.FileSize)))
}

// conversationContent pins the title and the first request; the rest of the
// conversation scrolls, starting at its newest messages, with a line where
// the day changes.
func (m Model) conversationContent(r *row, d *db.Detail) frameLines {
	c := frameLines{pinned: []string{m.headLine(r)}, fromBottom: true, keep: -1}
	if d == nil || d.First == nil {
		c.pinned = append(c.pinned, m.st.muted.Render("No text messages."))
		return c
	}
	c.pinned = append(c.pinned, m.messageLine(*d.First))
	c.before = d.Hidden
	// The marker sits under the speaker column.
	c.gap = func(n int) string { return m.st.muted.Render(fmt.Sprintf("        ⋮    %d messages", n)) }
	day := d.First.Timestamp.Local().Format(time.DateOnly)
	for _, msg := range d.Tail {
		if !msg.Timestamp.IsZero() {
			if dd := msg.Timestamp.Local().Format(time.DateOnly); dd != day {
				if c.notMessages == nil {
					c.notMessages = map[int]bool{}
				}
				c.notMessages[len(c.scroll)] = true
				c.scroll = append(c.scroll, m.dayLine(msg.Timestamp))
				day = dd
			}
		}
		if msg.Role == "user" {
			c.keep = len(c.scroll)
		}
		c.scroll = append(c.scroll, m.messageLine(msg))
	}
	return c
}

// spark draws counts as bars; empty stretches show as a faint baseline so
// the line still reads as one.
func (m Model) spark(buckets []int) string {
	top := 1
	for _, v := range buckets {
		top = max(top, v)
	}
	var b strings.Builder
	for _, v := range buckets {
		if v == 0 {
			b.WriteString(m.st.rule.Render("▁"))
			continue
		}
		b.WriteString(m.st.id.Render(string([]rune(sparkChars)[min(7, v*8/(top+1))])))
	}
	return b.String()
}

// doneContent pins activity, tools and commands, one line each; the edited
// files, grouped by where they live, scroll below. inner is the frame's
// inner width, 0 when only the number of lines matters.
func (m Model) doneContent(r *row, d *db.Detail, inner int) frameLines {
	c := frameLines{keep: -1}
	if d == nil {
		return c
	}
	c.pinned = append(c.pinned, m.section("Activity")+"  "+m.spark(d.Activity)+
		m.st.muted.Render(fmt.Sprintf("  %s · %d msgs", durationText(r.s.EndedAt.Sub(r.s.StartedAt)), r.s.MessageCount)))
	var tools []string
	for _, t := range d.TopTools {
		tools = append(tools, m.st.strong.Render(t.Name)+" "+m.st.title.UnsetBold().Render(fmt.Sprint(t.N)))
	}
	if len(tools) == 0 {
		tools = []string{m.st.muted.Render("none")}
	}
	c.pinned = append(c.pinned, m.section("Tools")+"     "+strings.Join(tools, "   "))
	c.pinned = append(c.pinned, m.section("Commands")+"  "+m.commandBars(commandCounts(d.Commands), inner-labelWidth-1))

	groups, temps := groupFiles(d.Files, r.s.ProjectPath, m.home)
	places := ""
	if len(groups)+boolInt(temps > 0) > 1 {
		places = fmt.Sprintf(" in %d places", len(groups)+boolInt(temps > 0))
	}
	c.scroll = append(c.scroll, m.section("Files")+m.st.muted.Render(fmt.Sprintf("  %d edited%s", d.FileCount, places)))
	if d.FileCount == 0 {
		c.scroll = append(c.scroll, "  "+m.st.muted.Render("none"))
	}
	nameW := max(20, inner-10)
	for _, g := range groups {
		name := g.name
		if name == "" {
			name = "this folder"
		}
		c.scroll = append(c.scroll, "  "+m.st.muted.Render(name))
		for _, f := range g.files {
			rel := middleEllipsis(f.Name, nameW)
			count := ""
			if f.N > 1 {
				count = fmt.Sprintf("×%d", f.N)
			}
			pad := " "
			if inner > 0 {
				pad = strings.Repeat(" ", max(1, nameW-len([]rune(rel))+1))
			}
			c.scroll = append(c.scroll, "    "+m.st.strong.Render(rel)+pad+m.st.muted.Render(count))
		}
	}
	if temps > 0 {
		c.scroll = append(c.scroll, "  "+m.st.muted.Render(fmt.Sprintf("+%d temp files", temps)))
	}
	if extra := d.FileCount - len(d.Files); extra > 0 {
		c.scroll = append(c.scroll, "  "+m.st.muted.Render(fmt.Sprintf("+%d more", extra)))
	}
	return c
}

// commandBars draws how often each program ran as short bars on one line,
// most used first, as many as fit in w cells.
func (m Model) commandBars(counts []db.Count, w int) string {
	if len(counts) == 0 {
		return m.st.muted.Render("none")
	}
	const barMax = 6
	top := counts[0].N
	var parts []string
	used := 0
	for i, c := range counts {
		bar := strings.Repeat("▇", max(1, c.N*barMax/top))
		part := m.st.strong.Bold(true).Render(c.Name) + " " + m.st.id.Render(bar) + " " + m.st.title.UnsetBold().Render(fmt.Sprint(c.N))
		pw := ansi.StringWidth(part) + 2
		more := ""
		if rest := len(counts) - i - 1; rest > 0 {
			more = fmt.Sprintf("+%d", rest)
		}
		if w > 0 && used+pw+len(more) > w && len(parts) > 0 {
			parts = append(parts, m.st.muted.Render(fmt.Sprintf("+%d", len(counts)-i)))
			break
		}
		parts = append(parts, part)
		used += pw
	}
	return strings.Join(parts, "  ")
}

// detailsLines is the Details frame in three groups: when, how much, where.
func (m Model) detailsLines(r *row, d *db.Detail) []string {
	kv := func(k, v string) string { return m.label(k) + v }
	stamp := func(t time.Time) string { return t.Local().Format("2006-01-02 15:04") }
	lines := []string{
		m.section("When"),
		kv("Started", m.st.strong.Render(stamp(r.s.StartedAt))),
		kv("Ended", m.st.strong.Render(stamp(r.s.EndedAt))+m.st.muted.Render("  "+durationText(r.s.EndedAt.Sub(r.s.StartedAt)))),
		"",
		m.section("How much"),
	}
	msgs := m.st.strong.Render(fmt.Sprint(r.s.MessageCount))
	if d != nil {
		msgs += m.st.muted.Render(fmt.Sprintf("  you %d · claude %d", d.You, d.Claude))
	}
	lines = append(lines, kv("Messages", msgs))
	if d != nil {
		lines = append(lines, kv("Calls", m.st.strong.Render(fmt.Sprint(d.Tools))+m.st.muted.Render(fmt.Sprintf("  thinking %d", d.Thinking))),
			kv("Files", m.st.strong.Render(fmt.Sprintf("%d edited", d.FileCount))))
	}
	size := m.st.strong.Render(formatSize(r.s.FileSize))
	if d != nil && d.Images > 0 {
		size += m.st.muted.Render(fmt.Sprintf("  %d images", d.Images))
	}
	lines = append(lines, kv("Size", size))
	if d != nil && d.Version != "" {
		lines = append(lines, kv("Version", m.st.muted.Render("Claude Code "+d.Version)))
	}

	name, badge := m.st.strong, m.st.worktree
	if r.gone {
		name, badge = m.st.gone, m.st.gone
	}
	folder := name.Render(r.folder)
	if r.worktree != "" {
		folder += " " + badge.Render(worktreeM+" "+r.worktree)
	}
	lines = append(lines, "", m.section("Where"), kv("ID", m.st.id.Render(r.s.ID)), kv("Folder", folder))
	if r.mainRoot != "" {
		lines = append(lines, kv("", m.st.muted.Render("worktree of "+tildePath(r.mainRoot, m.home))))
	}
	lines = append(lines, kv("Branch", m.st.dim.Render(r.s.GitBranch)))
	path := tildePath(r.s.ProjectPath, m.home)
	if r.gone {
		path += ", removed"
	}
	return append(lines, kv("Path", m.st.muted.Render(path)))
}

func (m Model) label(s string) string { return m.st.subtle.Render(fmt.Sprintf("%-*s", labelWidth, s)) }

// renderPane draws the three frames: below the list as two columns, or
// beside it as one.
func (m Model) renderPane() string {
	rects, ok := m.paneRects()
	r := m.current()
	if !ok || r == nil {
		return ""
	}
	content := m.sizedContent(r, rects)
	frame := func(f focus) string { return m.renderFrame(f, content[f], rects[f]) }
	if m.detailRight() {
		return strings.Join([]string{frame(focusConv), frame(focusDone), frame(focusDetails)}, "\n")
	}
	return joinColumns(frame(focusConv)+"\n"+frame(focusDone), frame(focusDetails), rects[focusConv].w, " ")
}

// scrollFrame moves frame f by delta lines; positive scrolls toward later
// content.
func (m *Model) scrollFrame(f focus, delta int) {
	r := m.current()
	rects, ok := m.paneRects()
	if r == nil || !ok || f == focusList {
		return
	}
	c := m.sizedContent(r, rects)[f]
	if c.fromBottom {
		delta = -delta
	}
	_, off, _, _ := window(c, rects[f].h-2, m.scroll[f]+delta)
	m.scroll[f] = off
}

// joinColumns puts two blocks of lines side by side; left is padded to
// leftW cells.
func joinColumns(left, right string, leftW int, gap string) string {
	l, r := strings.Split(left, "\n"), strings.Split(right, "\n")
	n := max(len(l), len(r))
	out := make([]string, n)
	for i := range n {
		var a, b string
		if i < len(l) {
			a = l[i]
		}
		if i < len(r) {
			b = r[i]
		}
		out[i] = a + strings.Repeat(" ", max(0, leftW-ansi.StringWidth(a))) + gap + b
	}
	return strings.Join(out, "\n")
}

// sizedContent builds the frames' content for their actual size.
func (m Model) sizedContent(r *row, rects [numFocus]rect) [numFocus]frameLines {
	return m.frameContent(r, rects[focusDone].w-4)
}
