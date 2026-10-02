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
	collapsedFiles    = 6 // files shown when the frame's height is not known yet
	collapsedCommands = 3

	// The Details frame below the list, border included: wide enough for a
	// full session ID after its label.
	detailsWidth    = 4 + labelWidth + 36
	labelWidth      = 9 // "Commands " and friends
	minFrame        = 5 // smallest frame: borders plus three lines
	minConversation = 6 // title, first request and two messages
	sparkChars      = "▁▂▃▄▅▆▇█"
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
	content := m.frameContent(r, 0, 0)
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
// needs as long as the first, Conversation, which scrolls, keeps
// minConversation lines.
func split(h, a, b int) (int, int) {
	if a+b <= h {
		return h - b, b
	}
	second := max(minFrame, min(b, h-minConversation))
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
	// toggles are the scrolling lines that expand or collapse the frame
	// when clicked ("+3 more files", "− show less").
	toggles map[int]bool
}

// frameContent builds each frame's lines. inner and room are the What was
// done frame's inner width and height, for aligning columns and deciding
// how many files and commands to show; 0 when only counting lines.
func (m Model) frameContent(r *row, inner, room int) [numFocus]frameLines {
	d := m.details[r.s.ID]
	var out [numFocus]frameLines
	out[focusConv] = m.conversationContent(r, d)
	out[focusDone] = m.doneContent(r, d, inner, room, m.expanded[r.s.ID])
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

func (m Model) section(s string) string { return m.st.subtle.Bold(true).Render(strings.ToUpper(s)) }

// messageLine is one message of the conversation: time, a colored dot for
// the speaker, the text. The user's own words are bold.
func (m Model) messageLine(msg db.Message) string {
	when := "     "
	if !msg.Timestamp.IsZero() {
		when = msg.Timestamp.Local().Format("15:04")
	}
	if msg.Role == "user" {
		return m.st.dim.Render(when) + " " + m.st.user.Render("●") + " " + m.st.strong.Bold(true).Render(collapse(msg.Content))
	}
	return m.st.dim.Render(when) + " " + m.st.claude.Render("●") + " " + m.st.subtle.Render(collapse(msg.Content))
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
	if d.Hidden > 0 {
		c.scroll = append(c.scroll, m.st.muted.Render(fmt.Sprintf("      ⋮  %d earlier messages", d.Hidden)))
	}
	day := d.First.Timestamp.Local().Format(time.DateOnly)
	for _, msg := range d.Tail {
		if !msg.Timestamp.IsZero() {
			if dd := msg.Timestamp.Local().Format(time.DateOnly); dd != day {
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

// doneContent pins activity and tools; edited files, grouped by where they
// live, and commands follow. Collapsed, it shows as many files as fit while
// keeping a few commands in view, and "+N more" lines expand it; expanded,
// everything is there to scroll through. inner and room are the frame's
// inner width and height, 0 when only the number of lines matters.
func (m Model) doneContent(r *row, d *db.Detail, inner, room int, expanded bool) frameLines {
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

	groups, temps := groupFiles(d.Files, r.s.ProjectPath, m.home)
	shownFiles := len(d.Files)
	shownCmds := len(d.Commands)
	gap := true // blank lines before FILES and COMMANDS
	if !expanded {
		// Collapsed: keep the COMMANDS heading and the latest commands in
		// view and fill what remains with files. When the frame is short,
		// show fewer commands, then drop the blank lines.
		shownCmds = min(len(d.Commands), collapsedCommands)
		shownFiles = collapsedFiles
		if room > 0 {
			cmdLines := func(n int, gap bool) int {
				if len(d.Commands) == 0 {
					return 0
				}
				return boolInt(gap) + 1 + n
			}
			minFiles := 1 // "none"
			if len(groups) > 0 {
				minFiles = 2 // a group heading and one file
			}
			// The last line says what is hidden and expands the frame.
			const moreLine = 1
			for _, try := range []struct {
				cmds int
				gap  bool
			}{{shownCmds, true}, {min(1, shownCmds), true}, {min(1, shownCmds), false}} {
				shownCmds, gap = try.cmds, try.gap
				if len(c.pinned)+boolInt(gap)+1+minFiles+cmdLines(shownCmds, gap)+moreLine <= room {
					break
				}
			}
			// Lines left for files; each group shown costs a heading.
			left := room - len(c.pinned) - boolInt(gap) - 1 - cmdLines(shownCmds, gap) - moreLine
			shownFiles = 0
			for _, g := range groups {
				if left < 2 {
					break
				}
				left--
				take := min(len(g.files), left)
				shownFiles += take
				left -= take
				if take < len(g.files) {
					break
				}
			}
		}
		shownFiles = max(1, shownFiles)
	}

	places := ""
	if len(groups)+boolInt(temps > 0) > 1 {
		places = fmt.Sprintf(" in %d places", len(groups)+boolInt(temps > 0))
	}
	if gap {
		c.scroll = append(c.scroll, "")
	}
	c.scroll = append(c.scroll, m.section("Files")+m.st.muted.Render(fmt.Sprintf("  %d edited%s", d.FileCount, places)))
	if d.FileCount == 0 {
		c.scroll = append(c.scroll, "  "+m.st.muted.Render("none"))
	}
	toggle := func(text string) {
		if c.toggles == nil {
			c.toggles = map[int]bool{}
		}
		c.toggles[len(c.scroll)] = true
		c.scroll = append(c.scroll, "  "+m.st.id.Render(text))
	}
	nameW := max(20, inner-10)
	shown := 0
	for _, g := range groups {
		if shown >= shownFiles {
			break
		}
		name := g.name
		if name == "" {
			name = "this folder"
		}
		c.scroll = append(c.scroll, "  "+m.st.muted.Render(name))
		for _, f := range g.files {
			if shown >= shownFiles {
				break
			}
			shown++
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
	hiddenFiles := d.FileCount - shown
	if temps > 0 && expanded {
		c.scroll = append(c.scroll, "  "+m.st.muted.Render(fmt.Sprintf("+%d temp files", temps)))
		hiddenFiles -= temps
	}
	if hiddenFiles > 0 && expanded {
		c.scroll = append(c.scroll, "  "+m.st.muted.Render(fmt.Sprintf("+%d more not loaded", hiddenFiles)))
	}

	if len(d.Commands) > 0 {
		if gap {
			c.scroll = append(c.scroll, "")
		}
		c.scroll = append(c.scroll, m.section("Commands")+m.st.muted.Render("  latest first"))
		for _, cmd := range d.Commands[:shownCmds] {
			prog, args, rest := commandParts(cmd)
			l := "  " + m.st.strong.Bold(true).Render(prog)
			if args != "" {
				l += " " + m.st.subtle.Render(args)
			}
			if rest != "" {
				l += " " + m.st.dim.Render(rest)
			}
			c.scroll = append(c.scroll, l)
		}
	}
	var hidden []string
	if !expanded && hiddenFiles > 0 {
		hidden = append(hidden, fmt.Sprintf("+%d more files", hiddenFiles))
	}
	if more := len(d.Commands) - shownCmds; more > 0 {
		hidden = append(hidden, fmt.Sprintf("+%d more commands", more))
	}
	switch {
	case expanded:
		toggle("− show less")
	case len(hidden) > 0:
		toggle(strings.Join(hidden, " · "))
	}
	return c
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
	content := m.doneSized(r, rects)
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
	c := m.doneSized(r, rects)[f]
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

// doneSized builds the frames' content for the frames' actual size.
func (m Model) doneSized(r *row, rects [numFocus]rect) [numFocus]frameLines {
	return m.frameContent(r, rects[focusDone].w-4, rects[focusDone].h-2)
}

// toggleAt reports whether a click at screen row y hits an expand or
// collapse line of the What was done frame.
func (m Model) toggleAt(y int) bool {
	r := m.current()
	rects, ok := m.paneRects()
	if r == nil || !ok {
		return false
	}
	rc := rects[focusDone]
	c := m.doneSized(r, rects)[focusDone]
	row := y - rc.y - 1
	if row < len(c.pinned) || row >= rc.h-2 {
		return false
	}
	_, _, first, _ := window(c, rc.h-2, m.scroll[focusDone])
	return c.toggles[first+row-len(c.pinned)]
}

// toggleDone expands or collapses the What was done frame of the selected
// session.
func (m *Model) toggleDone() {
	if r := m.current(); r != nil {
		m.expanded[r.s.ID] = !m.expanded[r.s.ID]
		m.scroll[focusDone] = 0
	}
}
