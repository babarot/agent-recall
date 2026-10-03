package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/babarot/claude-recall/internal/config"
)

// a asks Claude to find sessions, in a box over the screen: type a
// question, watch it search, pick one of the sessions it found to jump to,
// or f to narrow the list to all of them. Why Claude picked each stays
// under its row and in Conversation (tui.ask_reasons).

type askStage int

const (
	askClosed askStage = iota
	askTyping
	askRunning
	askAnswered
	askFailed
)

const (
	askWidth     = 96
	askTickEvery = 200 * time.Millisecond
	spinFrames   = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
)

type askState struct {
	stage    askStage
	input    textinput.Model
	question string
	steps    []string
	started  time.Time
	took     time.Duration
	hits     []askHit
	cost     float64
	model    string
	sel      int
	err      string
	cancel   context.CancelFunc
	seq      int // the run messages belong to; older ones are dropped
}

type askStepMsg struct {
	seq  int
	step string
	ch   <-chan tea.Msg
}

type askDoneMsg struct {
	seq    int
	answer askAnswer
	err    error
}

type askTickMsg struct{ seq int }

// AskWith makes asking give Claude recall's MCP server as the command line
// recall (this binary, with its database) and run it from dir.
func (m Model) AskWith(recall []string, dir string) Model {
	m.askRun = claudeRunner(recall, config.ModelID(m.cfg.AskModel), dir)
	return m
}

func newAskInput() textinput.Model {
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = "what are you looking for?"
	return in
}

// openAsk shows the box with the last question to edit.
func (m *Model) openAsk() tea.Cmd {
	m.ask.stage = askTyping
	m.ask.input.SetValue(m.ask.question)
	m.ask.input.CursorEnd()
	m.ask.input.SetWidth(min(askWidth, m.width-4) - 8)
	return m.ask.input.Focus()
}

// startAsk runs the question in the background.
func (m *Model) startAsk() tea.Cmd {
	q := strings.TrimSpace(m.ask.input.Value())
	if q == "" {
		return nil
	}
	m.ask.input.Blur()
	ctx, cancel := context.WithCancel(context.Background())
	m.ask.seq++
	seq := m.ask.seq
	m.ask.stage, m.ask.question, m.ask.steps, m.ask.cancel = askRunning, q, nil, cancel
	m.ask.started = m.now()
	ch := make(chan tea.Msg, 64)
	run := m.askRun
	go func() {
		answer, err := run(ctx, q, func(step string) { ch <- askStepMsg{seq: seq, step: step} })
		ch <- askDoneMsg{seq, answer, err}
		close(ch)
	}()
	return tea.Batch(waitAsk(ch), m.askTick(seq))
}

func (m *Model) askTick(seq int) tea.Cmd {
	return tea.Tick(askTickEvery, func(time.Time) tea.Msg { return askTickMsg{seq} })
}

// closeAsk hides the box, stopping a run.
func (m *Model) closeAsk() {
	if m.ask.cancel != nil {
		m.ask.cancel()
		m.ask.cancel = nil
	}
	m.ask.seq++
	m.ask.stage = askClosed
	m.ask.input.Blur()
}

// askMsg handles the background run's messages.
func (m *Model) askMsg(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case askStepMsg:
		if msg.seq != m.ask.seq {
			return nil
		}
		m.ask.steps = append(m.ask.steps, msg.step)
		return waitAsk(msg.ch)
	case askDoneMsg:
		if msg.seq != m.ask.seq {
			return nil
		}
		m.ask.cancel = nil
		m.ask.took = m.now().Sub(m.ask.started)
		if msg.err != nil {
			m.ask.stage, m.ask.err = askFailed, msg.err.Error()
			return nil
		}
		m.ask.stage, m.ask.sel = askAnswered, 0
		m.ask.hits = m.resolveHits(msg.answer.hits)
		m.ask.cost, m.ask.model = msg.answer.cost, msg.answer.model
		for _, h := range m.ask.hits {
			m.reasons[h.id] = h.why
		}
	case askTickMsg:
		if msg.seq == m.ask.seq && m.ask.stage == askRunning {
			return m.askTick(msg.seq)
		}
	}
	return nil
}

// waitAsk delivers the run's next message; a step carries the channel so
// the one after it can be waited for in turn.
func waitAsk(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		if s, isStep := msg.(askStepMsg); isStep {
			s.ch = ch
			return s
		}
		return msg
	}
}

// resolveHits turns Claude's IDs, often shortened, into the sessions' own,
// dropping any it made up or repeated.
func (m Model) resolveHits(hits []askHit) []askHit {
	var out []askHit
	seen := map[string]bool{}
	for _, h := range hits {
		id := strings.ToLower(h.id)
		if len(id) < 4 {
			continue
		}
		for i := range m.rows {
			full := m.rows[i].s.ID
			if strings.HasPrefix(strings.ToLower(full), id) && !seen[full] {
				seen[full] = true
				out = append(out, askHit{full, h.why})
				break
			}
		}
	}
	return out
}

// updateAsk handles a key while the box shows.
func (m Model) updateAsk(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch m.ask.stage {
	case askTyping:
		switch key {
		case "enter":
			return m, m.startAsk()
		case "esc":
			m.closeAsk()
			return m, nil
		}
		var cmd tea.Cmd
		m.ask.input, cmd = m.ask.input.Update(msg)
		return m, cmd
	case askRunning:
		if key == "esc" || key == "q" {
			m.closeAsk()
		}
	case askAnswered:
		switch key {
		case "down", "j", "ctrl+n", "tab":
			m.ask.sel = min(m.ask.sel+1, max(0, len(m.ask.hits)-1))
		case "up", "k", "ctrl+p", "shift+tab":
			m.ask.sel = max(0, m.ask.sel-1)
		case "enter":
			if len(m.ask.hits) > 0 {
				m.jumpTo(m.ask.hits[m.ask.sel].id)
			}
			m.closeAsk()
		case "f":
			if len(m.ask.hits) > 0 {
				m.showAsked()
			}
			m.closeAsk()
		case "r":
			return m, m.openAsk()
		case "esc", "q":
			m.closeAsk()
		}
	case askFailed:
		switch key {
		case "r", "enter":
			return m, m.openAsk()
		case "esc", "q":
			m.closeAsk()
		}
	}
	return m, nil
}

// jumpTo selects session id, showing every folder and dropping the filter
// first when they hide it.
func (m *Model) jumpTo(id string) {
	find := func() int {
		for i, idx := range m.visible {
			if m.rows[idx].s.ID == id {
				return i
			}
		}
		return -1
	}
	if find() < 0 {
		m.asked, m.scope = nil, ""
		m.filter.SetValue("")
		m.refresh()
	}
	if i := find(); i >= 0 {
		m.cursor = i
	}
	m.focus = focusList
	m.clamp()
}

// showAsked narrows the list to the sessions of the last answer, in
// Claude's order.
func (m *Model) showAsked() {
	m.asked = map[string]int{}
	for i, h := range m.ask.hits {
		m.asked[h.id] = i
	}
	m.askedFor = m.ask.question
	m.filter.SetValue("")
	m.refresh()
	m.cursor, m.offset = 0, 0
	m.focus = focusList
	m.clamp()
}

// reasonLine shows why Claude picked the selected session, for the
// Conversation frame.
func (m Model) reasonLine(id string, w int) string {
	why := m.reasons[id]
	if !m.cfg.AskReasons || why == "" {
		return ""
	}
	return ansi.Truncate(m.st.worktree.Render("↳ ")+m.st.subtle.Render(why), w, ellipsis)
}

// withAsk lays the box over the screen.
func (m Model) withAsk(screen string) string {
	w := min(askWidth, m.width-4)
	if w < 30 {
		return screen
	}
	inner := w - 4
	var title, hint string
	var body []string
	q := m.st.filter.Render("› " + m.ask.question)
	switch m.ask.stage {
	case askTyping:
		title, hint = "Ask Claude", "enter ask · esc close"
		body = []string{m.ask.input.View(),
			m.st.muted.Render(`  e.g. "the session where we fixed the terraform state" · "who looked at PR #7427"`), "",
			m.st.dim.Render(fmt.Sprintf("  claude -p · %s · searches with recall only (no files, no shell)", config.ModelID(m.cfg.AskModel))),
			m.st.dim.Render("  uses your Claude plan, as Claude Code does")}
	case askRunning:
		elapsed := m.now().Sub(m.ask.started)
		spin := string([]rune(spinFrames)[int(elapsed/(100*time.Millisecond))%len([]rune(spinFrames))])
		title, hint = "Ask Claude", "esc cancel"
		body = []string{ansi.Truncate(q, inner, ellipsis), "",
			m.st.id.Render(spin) + " " + m.st.subtle.Render("searching…") + m.st.muted.Render(fmt.Sprintf(" %ds", int(elapsed.Seconds())))}
		steps := m.ask.steps
		if len(steps) > 6 {
			steps = steps[len(steps)-6:]
		}
		for _, s := range steps {
			body = append(body, m.st.muted.Render("  · "+ansi.Truncate(s, inner-4, ellipsis)))
		}
	case askAnswered:
		title = fmt.Sprintf("Ask Claude · %d sessions", len(m.ask.hits))
		hint = m.askSummary()
		body = []string{ansi.Truncate(q, inner, ellipsis), ""}
		if len(m.ask.hits) == 0 {
			body = append(body, m.st.muted.Render("  No session matched. r asks again."))
		}
		room := max(3, m.height-8-len(body))
		var items [][]string
		for i, h := range m.ask.hits {
			items = append(items, m.askItem(h, inner, i == m.ask.sel))
		}
		// Keep the selected item in view.
		from, used := 0, 0
		for i := 0; i <= m.ask.sel && i < len(items); i++ {
			used += len(items[i])
			for used > room && from < i {
				used -= len(items[from])
				from++
			}
		}
		for i := from; i < len(items) && len(body)+len(items[i]) <= room+2; i++ {
			body = append(body, items[i]...)
		}
	case askFailed:
		title, hint = "Ask Claude", "r ask again · esc close"
		body = []string{ansi.Truncate(q, inner, ellipsis), ""}
		for _, l := range wrap(m.ask.err, inner) {
			body = append(body, m.st.warn.Render(l))
		}
	}
	return m.boxOver(screen, w, title, hint, body)
}

// boxOver lays a box w cells wide over the screen, near the top: title and
// hint in its top edge, body inside.
func (m Model) boxOver(screen string, w int, title, hint string, body []string) string {
	inner := w - 4
	b := m.st.id
	fill := max(0, w-5-ansi.StringWidth(title)-ansi.StringWidth(hint)-2)
	box := []string{b.Render("╭─ ") + m.st.key.Render(title) + b.Render(" "+strings.Repeat("─", fill)+" ") + m.st.muted.Render(hint) + b.Render(" ╮")}
	for _, l := range body {
		l = ansi.Truncate(l, inner, ellipsis)
		box = append(box, b.Render("│ ")+l+strings.Repeat(" ", max(0, inner-ansi.StringWidth(l)))+b.Render(" │"))
	}
	box = append(box, b.Render("╰"+strings.Repeat("─", w-2)+"╯"))
	lines := strings.Split(screen, "\n")
	x := (m.width - w) / 2
	for i, l := range box {
		if j := 2 + i; j < len(lines) {
			lines[j] = overlay(lines[j], l, x)
		}
	}
	return strings.Join(lines, "\n")
}

// askSummary says how an answer came: the model, how long it took and,
// with tui.ask_show_cost, what claude says it cost.
func (m Model) askSummary() string {
	parts := []string{}
	if m.ask.model != "" {
		parts = append(parts, m.ask.model)
	}
	parts = append(parts, fmt.Sprintf("%ds", int(m.ask.took.Seconds())))
	if m.cfg.AskShowCost && m.ask.cost > 0 {
		parts = append(parts, fmt.Sprintf("$%.2f", m.ask.cost))
	}
	return strings.Join(parts, " · ")
}

// askItem is a found session in the box: its title and where it is, then
// Claude's reason in up to two lines.
func (m Model) askItem(h askHit, w int, sel bool) []string {
	var r *row
	for i := range m.rows {
		if m.rows[i].s.ID == h.id {
			r = &m.rows[i]
		}
	}
	if r == nil {
		return nil
	}
	where := r.folder
	if r.worktree != "" {
		where += " " + worktreeM + " " + r.worktree
	}
	meta := fmt.Sprintf("  %s · %s · %d msgs · %s", where, relativeDate(r.s.EndedAt, m.now()), r.s.MessageCount, r.s.ID[:min(8, len(r.s.ID))])
	lines := []string{m.st.title.Render(r.title) + m.st.muted.Render(meta)}
	why := wrap(h.why, w-2)
	if len(why) > 2 {
		why = append(why[:1], ansi.Truncate(why[1]+" "+strings.Join(why[2:], " "), w-2, ellipsis))
	}
	for _, l := range why {
		lines = append(lines, m.st.subtle.Render(l))
	}
	lead := "  "
	if sel {
		lead = m.st.bar.Render("▎") + " "
	}
	for i := range lines {
		lines[i] = lead + ansi.Truncate(lines[i], w-2, ellipsis)
	}
	return lines
}
