package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// c continues the selected session in a new claude, for a session claude -r
// cannot resume (its folder or transcript is gone): a box over the screen
// takes what to recall about it, empty for where it left off, then the TUI
// quits and the caller starts claude in the folder recall was started in,
// asking it to recall the session through recall's MCP server.

// Continue is what the caller should run after the TUI exits instead of a
// resume: a new claude that recalls SessionID, about Topic when it is not
// empty.
type Continue struct {
	SessionID string
	Topic     string
}

type contState struct {
	open  bool
	id    string
	input textinput.Model
}

func newContInput() textinput.Model {
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = "what to recall about it (empty: pick up where it left off)"
	return in
}

// openCont shows the box for session id.
func (m *Model) openCont(id string) tea.Cmd {
	m.cont.open, m.cont.id = true, id
	m.cont.input.SetValue("")
	m.cont.input.SetWidth(min(askWidth, m.width-4) - 8)
	return m.cont.input.Focus()
}

func (m *Model) closeCont() {
	m.cont.open = false
	m.cont.input.Blur()
}

// updateCont handles a key while the box shows.
func (m Model) updateCont(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.Continue = &Continue{SessionID: m.cont.id, Topic: strings.TrimSpace(m.cont.input.Value())}
		m.closeCont()
		return m, m.imagesQuit()
	case "esc":
		m.closeCont()
		return m, nil
	}
	var cmd tea.Cmd
	m.cont.input, cmd = m.cont.input.Update(msg)
	return m, cmd
}

// withCont lays the box over the screen.
func (m Model) withCont(screen string) string {
	w := min(askWidth, m.width-4)
	if w < 30 {
		return screen
	}
	var body []string
	for i := range m.rows {
		if r := &m.rows[i]; r.s.ID == m.cont.id {
			where := r.folder
			if r.worktree != "" {
				where += " " + worktreeM + " " + r.worktree
			}
			meta := fmt.Sprintf("  %s · %s", where, r.s.ID[:min(8, len(r.s.ID))])
			body = append(body, m.st.title.Render(r.title)+m.st.muted.Render(meta), "")
		}
	}
	body = append(body, m.cont.input.View(), "")
	where := "  claude with recall's MCP server"
	if m.startDir != "" {
		where += ", in " + tildePath(m.startDir, m.home)
	}
	body = append(body, m.st.dim.Render(where))
	return m.boxOver(screen, w, "Continue in a new claude", "enter start · esc close", body)
}
