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
// all three stack.

const (
	// The Details frame below the list, border included: wide enough for a
	// full session ID after its label.
	detailsWidth = 4 + labelWidth + 36
	labelWidth   = 9  // "Commands " and friends
	sparkChars   = "▁▂▃▄▅▆▇█"
)

// frame draws lines inside a rounded border w cells wide and h lines tall,
// with title set into the top edge.
func (m Model) frame(title string, lines []string, w, h int) string {
	if w < 6 || h < 2 {
		return ""
	}
	inner := w - 4
	titleW := ansi.StringWidth(title)
	fill := max(0, w-5-titleW)
	out := []string{m.st.rule.Render("╭─ ") + m.st.subtle.Bold(true).Render(title) + m.st.rule.Render(" "+strings.Repeat("─", fill)+"╮")}
	for _, l := range fit(lines, h-2) {
		l = ansi.Truncate(l, inner, ellipsis)
		pad := strings.Repeat(" ", max(0, inner-ansi.StringWidth(l)))
		out = append(out, m.st.rule.Render("│")+" "+l+pad+" "+m.st.rule.Render("│"))
	}
	out = append(out, m.st.rule.Render("╰"+strings.Repeat("─", w-2)+"╯"))
	return strings.Join(out, "\n")
}

// block is a part of a frame that can grow: it gets min lines first, then
// one more at a time up to max while space is left, in priority order.
type block struct{ min, max int }

func allocate(n int, blocks ...block) []int {
	got := make([]int, len(blocks))
	for i, b := range blocks {
		g := min(b.min, n)
		got[i], n = g, n-g
	}
	for moved := true; moved && n > 0; {
		moved = false
		for i, b := range blocks {
			if n > 0 && got[i] < b.max {
				got[i]++
				n--
				moved = true
			}
		}
	}
	return got
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

func oneLine(s string) string { return collapse(s) }

func (m Model) messageLine(msg db.Message) string {
	who := m.st.claude.Render("claude")
	if msg.Role == "user" {
		who = m.st.user.Render("you   ")
	}
	when := "-"
	if !msg.Timestamp.IsZero() {
		when = msg.Timestamp.Local().Format("01-02 15:04")
	}
	return who + " " + m.st.muted.Render(when) + " " + m.st.strong.Render(oneLine(msg.Content))
}

// conversationLines is the first request, then as many of the latest
// messages as fit, always keeping the last thing the user said.
func (m Model) conversationLines(d *db.Detail, n int) []string {
	if d == nil || d.First == nil || n <= 0 {
		return []string{m.st.muted.Render("No text messages.")}
	}
	lines := []string{m.messageLine(*d.First)}
	room := n - 2
	if room <= 0 || len(d.Tail) == 0 {
		return lines
	}
	shown := d.Tail[max(0, len(d.Tail)-room):]
	lastUser := -1
	for i := len(d.Tail) - 1; i >= 0; i-- {
		if d.Tail[i].Role == "user" {
			lastUser = i
			break
		}
	}
	if start := len(d.Tail) - len(shown); lastUser >= 0 && lastUser < start {
		shown = append([]db.Message{d.Tail[lastUser]}, d.Tail[len(d.Tail)-room+1:]...)
	}
	hidden := d.Hidden + len(d.Tail) - len(shown)
	lines = append(lines, strings.Repeat(" ", 18)+m.st.muted.Render(fmt.Sprintf("··· %d messages ···", hidden)))
	for _, msg := range shown {
		lines = append(lines, m.messageLine(msg))
	}
	return lines
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
		i := min(7, v*8/(top+1))
		b.WriteString(string([]rune(sparkChars)[i]))
	}
	return b.String()
}

func (m Model) label(s string) string { return m.st.subtle.Render(fmt.Sprintf("%-*s", labelWidth, s)) }

// doneLines is the What was done frame: activity and tools, then files and
// commands with fileN and cmdN lines each.
func (m Model) doneLines(r *row, d *db.Detail, fileN, cmdN int) []string {
	if d == nil {
		return nil
	}
	hm := func(t time.Time) string { return t.Local().Format("15:04") }
	lines := []string{m.label("Activity") + m.st.muted.Render(hm(r.s.StartedAt)+" ") + m.st.id.Render(spark(d.Activity)) +
		m.st.muted.Render(" "+hm(r.s.EndedAt)+"  "+durationText(r.s.EndedAt.Sub(r.s.StartedAt)))}

	var tools []string
	for _, t := range d.TopTools {
		tools = append(tools, m.st.strong.Render(t.Name)+" "+m.st.title.UnsetBold().Render(fmt.Sprint(t.N)))
	}
	if len(tools) == 0 {
		tools = []string{m.st.muted.Render("none")}
	}
	lines = append(lines, m.label("Tools")+strings.Join(tools, m.st.helpSep.Render(" · ")))

	switch {
	case d.FileCount == 0:
		lines = append(lines, m.label("Edited")+m.st.muted.Render("no files"))
	case fileN <= 1:
		var names []string
		for _, f := range d.Files[:min(4, len(d.Files))] {
			names = append(names, m.st.strong.Render(shortPath(f.Name, m.home)))
		}
		more := ""
		if d.FileCount > len(names) {
			more = m.st.muted.Render(fmt.Sprintf("  +%d", d.FileCount-len(names)))
		}
		lines = append(lines, m.label("Edited")+strings.Join(names, "  ")+more)
	default:
		for i, f := range d.Files[:min(fileN, len(d.Files))] {
			l := m.label("")
			if i == 0 {
				l = m.label("Edited")
			}
			lines = append(lines, l+m.st.strong.Render(shortPath(f.Name, m.home))+m.st.muted.Render(fmt.Sprintf(" ×%d", f.N)))
		}
		if extra := d.FileCount - min(fileN, len(d.Files)); extra > 0 && fileN >= len(d.Files) {
			lines = append(lines, m.label("")+m.st.muted.Render(fmt.Sprintf("+%d more", extra)))
		}
	}
	for i, c := range d.Commands[:min(cmdN, len(d.Commands))] {
		l := m.label("")
		if i == 0 {
			l = m.label("Commands")
		}
		lines = append(lines, l+m.st.muted.Render("$ ")+m.st.strong.Render(oneLine(c)))
	}
	return lines
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

func (m Model) headLine(r *row) string {
	return m.st.title.Render(r.title) + m.st.muted.Render(fmt.Sprintf("  %s · %d msgs · %s",
		durationText(r.s.EndedAt.Sub(r.s.StartedAt)), r.s.MessageCount, formatSize(r.s.FileSize)))
}

// renderDetailBelow draws the pane under the list: width cells, height lines.
func (m Model) renderDetailBelow(width, height int) string {
	r := m.current()
	if r == nil {
		return m.frame("Details", nil, width, height)
	}
	d := m.details[r.s.ID]
	rightW := max(30, min(detailsWidth, width-70))
	leftW := width - rightW - 1

	// The two left frames cost four border lines; the head line, activity
	// and tools take three; conversation, files and commands share the rest.
	var cmds, files, tail int
	if d != nil {
		cmds, files, tail = len(d.Commands), max(1, len(d.Files)), len(d.Tail)+2
	}
	got := allocate(max(0, height-4-3), block{3, max(3, tail)}, block{1, max(1, files)}, block{0, cmds})
	conv := append([]string{m.headLine(r)}, m.conversationLines(d, got[0])...)
	done := m.doneLines(r, d, got[1], got[2])
	topH := len(conv) + 2
	if d == nil {
		topH = 1 + 3 + 2
	}
	topH = min(topH, height-3)
	botH := height - topH

	left := m.frame("Conversation", conv, leftW, topH) + "\n" + m.frame("What was done", done, leftW, botH)
	right := m.frame("Details", m.detailsLines(r, d), rightW, height)
	return joinColumns(left, right, leftW, " ")
}

// renderDetailBeside draws the pane right of the list, the frames stacked.
func (m Model) renderDetailBeside(width, height int) string {
	r := m.current()
	if r == nil {
		return m.frame("Details", nil, width, height)
	}
	d := m.details[r.s.ID]
	details := m.detailsLines(r, d)
	detailsH := min(len(details)+2, max(4, height/3))
	var cmds, files, tail int
	if d != nil {
		cmds, files, tail = len(d.Commands), max(1, len(d.Files)), len(d.Tail)+2
	}
	got := allocate(max(0, height-detailsH-4-3), block{3, max(3, tail)}, block{1, max(1, files)}, block{0, cmds})
	conv := append([]string{m.headLine(r)}, m.conversationLines(d, got[0])...)
	done := m.doneLines(r, d, got[1], got[2])
	convH := min(len(conv)+2, height-detailsH-3)
	doneH := height - detailsH - convH
	return strings.Join([]string{m.frame("Conversation", conv, width, convH), m.frame("What was done", done, width, doneH),
		m.frame("Details", details, width, detailsH)}, "\n")
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
