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
	content := m.frameContent(r)
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
// lines: each gets what it needs if that fits, else the second gets what it
// needs up to half and the first, Conversation, the rest.
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
}

func (m Model) frameContent(r *row) [numFocus]frameLines {
	d := m.details[r.s.ID]
	var out [numFocus]frameLines
	out[focusConv] = m.conversationContent(r, d)
	out[focusDone] = m.doneContent(r, d)
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
	visible := c.scroll[start:end]
	if c.fromBottom && off == 0 && c.keep >= 0 && c.keep < start && room >= 2 {
		visible = append([]string{c.scroll[c.keep]}, c.scroll[start+1:end]...)
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

func (m Model) messageLine(msg db.Message) string {
	who := m.st.claude.Render("claude")
	if msg.Role == "user" {
		who = m.st.user.Render("you   ")
	}
	when := "-"
	if !msg.Timestamp.IsZero() {
		when = msg.Timestamp.Local().Format("01-02 15:04")
	}
	return who + " " + m.st.muted.Render(when) + " " + m.st.strong.Render(collapse(msg.Content))
}

func (m Model) headLine(r *row) string {
	return m.st.title.Render(r.title) + m.st.muted.Render(fmt.Sprintf("  %s · %d msgs · %s",
		durationText(r.s.EndedAt.Sub(r.s.StartedAt)), r.s.MessageCount, formatSize(r.s.FileSize)))
}

// conversationContent pins the head line and the first request; the rest of
// the conversation scrolls, starting at its newest messages.
func (m Model) conversationContent(r *row, d *db.Detail) frameLines {
	c := frameLines{pinned: []string{m.headLine(r)}, fromBottom: true, keep: -1}
	if d == nil || d.First == nil {
		c.pinned = append(c.pinned, m.st.muted.Render("No text messages."))
		return c
	}
	c.pinned = append(c.pinned, m.messageLine(*d.First))
	if d.Hidden > 0 {
		c.scroll = append(c.scroll, strings.Repeat(" ", 18)+m.st.muted.Render(fmt.Sprintf("··· %d earlier messages ···", d.Hidden)))
	}
	for _, msg := range d.Tail {
		if msg.Role == "user" {
			c.keep = len(c.scroll)
		}
		c.scroll = append(c.scroll, m.messageLine(msg))
	}
	return c
}

func spark(buckets []int) string {
	top := 1
	for _, v := range buckets {
		top = max(top, v)
	}
	var b strings.Builder
	for _, v := range buckets {
		if v == 0 {
			b.WriteByte(' ')
			continue
		}
		b.WriteString(string([]rune(sparkChars)[min(7, v*8/(top+1))]))
	}
	return b.String()
}

func (m Model) label(s string) string { return m.st.subtle.Render(fmt.Sprintf("%-*s", labelWidth, s)) }

// doneContent pins activity and tools; edited files and commands scroll.
func (m Model) doneContent(r *row, d *db.Detail) frameLines {
	c := frameLines{keep: -1}
	if d == nil {
		return c
	}
	hm := func(t time.Time) string { return t.Local().Format("15:04") }
	c.pinned = append(c.pinned, m.label("Activity")+m.st.muted.Render(hm(r.s.StartedAt)+" ")+m.st.id.Render(spark(d.Activity))+
		m.st.muted.Render(" "+hm(r.s.EndedAt)+"  "+durationText(r.s.EndedAt.Sub(r.s.StartedAt))))
	var tools []string
	for _, t := range d.TopTools {
		tools = append(tools, m.st.strong.Render(t.Name)+" "+m.st.title.UnsetBold().Render(fmt.Sprint(t.N)))
	}
	if len(tools) == 0 {
		tools = []string{m.st.muted.Render("none")}
	}
	c.pinned = append(c.pinned, m.label("Tools")+strings.Join(tools, m.st.helpSep.Render(" · ")))

	if d.FileCount == 0 {
		c.scroll = append(c.scroll, m.label("Edited")+m.st.muted.Render("no files"))
	}
	for i, f := range d.Files {
		l := m.label("")
		if i == 0 {
			l = m.label("Edited")
		}
		c.scroll = append(c.scroll, l+m.st.strong.Render(shortPath(f.Name, m.home))+m.st.muted.Render(fmt.Sprintf(" ×%d", f.N)))
	}
	if extra := d.FileCount - len(d.Files); extra > 0 {
		c.scroll = append(c.scroll, m.label("")+m.st.muted.Render(fmt.Sprintf("+%d more", extra)))
	}
	for i, cmd := range d.Commands {
		l := m.label("")
		if i == 0 {
			l = m.label("Commands")
		}
		c.scroll = append(c.scroll, l+m.st.muted.Render("$ ")+m.st.strong.Render(collapse(cmd)))
	}
	return c
}

// detailsLines is the Details frame, most useful first so a short frame
// keeps what matters.
func (m Model) detailsLines(r *row, d *db.Detail) []string {
	kv := func(k, v string) string { return m.label(k) + v }
	stamp := func(t time.Time) string { return t.Local().Format("2006-01-02 15:04") }
	lines := []string{
		kv("Started", m.st.strong.Render(stamp(r.s.StartedAt))),
		kv("Ended", m.st.strong.Render(stamp(r.s.EndedAt))+m.st.muted.Render(" ("+durationText(r.s.EndedAt.Sub(r.s.StartedAt))+")")),
		kv("Messages", m.st.strong.Render(fmt.Sprint(r.s.MessageCount))),
		kv("ID", m.st.id.Render(r.s.ID)),
	}
	if d != nil {
		lines = append(lines,
			kv("", m.st.muted.Render(fmt.Sprintf("you %d · claude %d", d.You, d.Claude))),
			kv("", m.st.muted.Render(fmt.Sprintf("tools %d · thinking %d", d.Tools, d.Thinking))))
	}
	size := m.st.strong.Render(formatSize(r.s.FileSize))
	if d != nil && d.Images > 0 {
		size += m.st.muted.Render(fmt.Sprintf(" · %d images", d.Images))
	}
	lines = append(lines, kv("Size", size), kv("Branch", m.st.dim.Render(r.s.GitBranch)))
	if d != nil {
		lines = append(lines, kv("Files", m.st.strong.Render(fmt.Sprintf("%d edited", d.FileCount))),
			kv("Version", m.st.muted.Render(d.Version)))
	}
	name, badge := m.st.strong, m.st.worktree
	if r.gone {
		name, badge = m.st.gone, m.st.gone
	}
	folder := name.Render(r.folder)
	if r.worktree != "" {
		folder += " " + badge.Render(worktreeM+" "+r.worktree)
	}
	lines = append(lines, kv("Folder", folder))
	if r.mainRoot != "" {
		lines = append(lines, kv("Worktree", m.st.muted.Render("of "+tildePath(r.mainRoot, m.home))))
	}
	path := tildePath(r.s.ProjectPath, m.home)
	if r.gone {
		path += ", removed"
	}
	return append(lines, kv("Path", m.st.muted.Render(path)))
}

// renderPane draws the three frames: below the list as two columns, or
// beside it as one.
func (m Model) renderPane() string {
	rects, ok := m.paneRects()
	r := m.current()
	if !ok || r == nil {
		return ""
	}
	content := m.frameContent(r)
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
	c := m.frameContent(r)[f]
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
