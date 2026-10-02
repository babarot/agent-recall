package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/babarot/claude-recall/internal/config"
)

// fakeAsk answers every question with hits after reporting a step, or
// fails with err; it records what it was asked.
type fakeAsk struct {
	hits  []askHit
	err   error
	asked []string
	block chan struct{} // when set, waits for it or the cancel
}

func (f *fakeAsk) run(ctx context.Context, q string, progress func(string)) ([]askHit, error) {
	f.asked = append(f.asked, q)
	progress(`search "` + q + `"`)
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return f.hits, f.err
}

// settleAsk runs what a key started until the answer is in, leaving out
// the spinner's ticks.
func settleAsk(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, sub := range msg {
				run(sub)
			}
		case askStepMsg, askDoneMsg:
			next, more := m.Update(msg)
			m = next.(Model)
			run(more)
		}
	}
	run(cmd)
	return m
}

func askModel(t *testing.T, f *fakeAsk, cfg config.TUI) Model {
	t.Helper()
	m, _ := newTestModel(t, cfg, 140, 40)
	m.askRun = f.run
	return m
}

func ask(t *testing.T, m Model, q string) Model {
	t.Helper()
	m = press(t, m, "a")
	for _, r := range q {
		m = update(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return settleAsk(t, next.(Model), cmd)
}

func TestAskFindsSessions(t *testing.T) {
	f := &fakeAsk{hits: []askHit{{"cccccccc", "talked about the parser"}, {"made-up1", "not a session"}, {"aaaa", "the login bug"}, {"cccc", "again"}}}
	m := askModel(t, f, config.Default().TUI)
	m = press(t, m, "a")
	if m.ask.stage != askTyping || !strings.Contains(screen(m), "Ask Claude") || !strings.Contains(screen(m), "claude -p · sonnet") {
		t.Fatalf("a should open the box:\n%s", screen(m))
	}
	m = press(t, m, "esc")
	m = ask(t, m, "parser work")
	if len(f.asked) != 1 || f.asked[0] != "parser work" {
		t.Fatalf("asked %q", f.asked)
	}
	if m.ask.stage != askAnswered || len(m.ask.hits) != 2 || m.ask.hits[0].id != "cccccccc-3333" || m.ask.hits[1].id != "aaaaaaaa-1111" {
		t.Fatalf("hits %+v: short IDs resolve, made-up and repeated ones drop", m.ask.hits)
	}
	s := screen(m)
	for _, want := range []string{"Ask Claude · 2 sessions", "Refactor the parser", "talked about the parser", "fix the login bug", "enter open"} {
		if !strings.Contains(s, want) {
			t.Errorf("answer lacks %q:\n%s", want, s)
		}
	}
	// Enter jumps to the picked session and closes the box.
	m = press(t, m, "j", "enter")
	if m.ask.stage != askClosed || m.current().s.ID != "aaaaaaaa-1111" {
		t.Fatalf("enter: stage %v selected %s", m.ask.stage, m.current().s.ID)
	}
	// The reason stays in Conversation.
	if !strings.Contains(screen(m), "↳ the login bug") {
		t.Fatalf("Conversation should keep the reason:\n%s", screen(m))
	}
	// a again starts from the last question.
	if m = press(t, m, "a"); m.ask.input.Value() != "parser work" {
		t.Fatalf("reopened with %q", m.ask.input.Value())
	}
}

func TestAskFilterShowsTheAnswer(t *testing.T) {
	f := &fakeAsk{hits: []askHit{{"cccccccc", "talked about the parser"}, {"aaaaaaaa", "the login bug"}}}
	m := ask(t, askModel(t, f, config.Default().TUI), "anything")
	m = press(t, m, "f")
	if got := visibleIDs(m); got != "cccccccc-3333,aaaaaaaa-1111" {
		t.Fatalf("f shows %s, want Claude's order", got)
	}
	s := screen(m)
	if !strings.Contains(s, "asked: anything") || !strings.Contains(s, "↳ talked about the parser") || !strings.Contains(s, "↳ the login bug") {
		t.Fatalf("the list should say what was asked and why each:\n%s", s)
	}
	// A click on the second session's row, two lines down.
	m = update(t, m, tea.MouseClickMsg{X: 10, Y: m.listTop() + 2, Button: tea.MouseLeft})
	if m.current().s.ID != "aaaaaaaa-1111" {
		t.Fatalf("click picked %s", m.current().s.ID)
	}
	if m = press(t, m, "esc"); m.asked != nil || len(m.visible) != 3 {
		t.Fatal("esc should drop the answer's filter")
	}
}

func TestAskReasonsCanBeHidden(t *testing.T) {
	cfg := config.Default().TUI
	cfg.AskReasons = false
	f := &fakeAsk{hits: []askHit{{"cccccccc", "talked about the parser"}}}
	m := press(t, ask(t, askModel(t, f, cfg), "anything"), "f")
	if s := screen(m); strings.Contains(s, "↳") || m.rowLines() != 1 {
		t.Fatalf("with ask_reasons off, no reasons:\n%s", s)
	}
}

func TestAskCancelAndFail(t *testing.T) {
	f := &fakeAsk{block: make(chan struct{})}
	m := askModel(t, f, config.Default().TUI)
	m = press(t, m, "a", "x")
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if m.ask.stage != askRunning || !strings.Contains(screen(m), "searching…") {
		t.Fatalf("running:\n%s", screen(m))
	}
	m = press(t, m, "esc")
	if m.ask.stage != askClosed {
		t.Fatal("esc should cancel")
	}
	// The cancelled run's answer comes too late to matter.
	if m = settleAsk(t, m, cmd); m.ask.stage != askClosed {
		t.Fatalf("a cancelled run reopened the box: %v", m.ask.stage)
	}

	f = &fakeAsk{err: errors.New("claude (Claude Code) is not on PATH")}
	m = ask(t, askModel(t, f, config.Default().TUI), "q")
	if m.ask.stage != askFailed || !strings.Contains(screen(m), "not on PATH") {
		t.Fatalf("failure:\n%s", screen(m))
	}
	if m = press(t, m, "r"); m.ask.stage != askTyping || m.ask.input.Value() != "q" {
		t.Fatal("r should let the question be asked again")
	}
}

func TestAskJumpsPastTheFolder(t *testing.T) {
	ff := newFolderFixture(t)
	m := folderModel(t, config.Default().TUI, ff, 140, 40).StartIn(ff.repo)
	f := &fakeAsk{hits: []askHit{{"other-2", "in the notes"}}}
	m.askRun = f.run
	m = press(t, ask(t, m, "notes"), "enter")
	if m.current().s.ID != "other-2" || m.scope != "" {
		t.Fatalf("jumped to %s with scope %q", m.current().s.ID, m.scope)
	}
}

func TestReadAskStream(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"looking"},{"type":"tool_use","name":"mcp__recall__recall_search","input":{"query":"herdr layout"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","content":"..."}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"mcp__recall__recall_list","input":{"project":"dotfiles","limit":5}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"StructuredOutput","input":{}}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"structured_output":{"sessions":[{"session_id":"4683157c","why":" herdr layouts "}]}}`,
	}, "\n") + "\n"
	var steps []string
	hits, err := readAskStream(strings.NewReader(stream), func(s string) { steps = append(steps, s) })
	if err != nil || len(hits) != 1 || hits[0] != (askHit{"4683157c", "herdr layouts"}) {
		t.Fatalf("hits %+v err %v", hits, err)
	}
	if strings.Join(steps, "|") != `search "herdr layout"|list project:dotfiles limit:5` {
		t.Fatalf("steps %q", steps)
	}
	if _, err := readAskStream(strings.NewReader(`{"type":"result","is_error":true,"result":"usage limit reached"}`+"\n"), func(string) {}); err == nil || !strings.Contains(err.Error(), "usage limit") {
		t.Fatalf("error result: %v", err)
	}
	if _, err := readAskStream(strings.NewReader(`{"type":"system"}`+"\n"), func(string) {}); err == nil {
		t.Fatal("a stream without a result is an error")
	}
}
