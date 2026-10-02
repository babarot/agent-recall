// Package tui is the interactive session list: browse archived sessions,
// look inside one, copy its ID or resume it.
package tui

import (
	"cmp"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/babarot/claude-recall/internal/config"
	"github.com/babarot/claude-recall/internal/db"
	"github.com/babarot/claude-recall/internal/theme"
	"github.com/babarot/claude-recall/internal/worktree"
)

// Resume is what the caller should run after the TUI exits: claude -r ID in
// Dir.
type Resume struct {
	Dir       string
	SessionID string
}

// Source loads what the preview and the detail pane show about a session.
type Source interface {
	SessionPreview(sessionID string, head, tail int) (db.Preview, error)
	SessionDetail(sessionID string) (*db.Detail, error)
}

type mode int

const (
	modeList mode = iota
	modeFilter
	modePreview
)

type sortKey struct {
	name string
	less func(a, b *row) int
}

var sorts = []sortKey{
	{"Ended", func(a, b *row) int { return b.s.EndedAt.Compare(a.s.EndedAt) }},
	{"Started", func(a, b *row) int { return b.s.StartedAt.Compare(a.s.StartedAt) }},
	{"Msgs", func(a, b *row) int { return cmp.Compare(b.s.MessageCount, a.s.MessageCount) }},
	{"Size", func(a, b *row) int { return cmp.Compare(b.s.FileSize, a.s.FileSize) }},
}

const (
	previewHead = 4
	previewTail = 12
	toastFor    = 2500 * time.Millisecond
)

type toastExpired struct{ id int }

type toastKind int

const (
	toastInfo toastKind = iota
	toastOK
	toastWarn
)

// Model is the Bubble Tea model of the session list.
type Model struct {
	cfg     config.TUI
	source  Source
	home    string
	now     func() time.Time
	st      styles
	rows    []row
	visible []int // indexes into rows, filtered and sorted

	cursor, offset int
	width, height  int
	detailOpen     bool
	mode           mode
	sortIdx        int

	filter textinput.Model

	preview    viewport.Model
	previewFor string // session ID the preview shows

	// details caches the detail pane's data per session ID.
	details map[string]*db.Detail
	// detailH is the height of the detail pane below the list; statePath
	// is where a changed height is remembered.
	detailH   int
	statePath string
	dragging  bool

	// focus is the session list or a frame of the detail pane; scroll is
	// each frame's offset, reset when another session is selected.
	focus     focus
	scroll    [numFocus]int
	scrollFor string

	toast     string
	toastKind toastKind
	toastID   int

	// Result is set when the user picks a session to resume.
	Result *Resume
}

// New builds the model from the archived sessions.
func New(sessions []db.Session, source Source, cfg config.TUI) Model {
	home, _ := os.UserHomeDir()
	resolver := worktree.NewResolver()
	rows := make([]row, len(sessions))
	for i, s := range sessions {
		rows[i] = newRow(s, home, resolver)
	}

	fi := textinput.New()
	fi.Prompt = "/ "
	fi.Placeholder = "filter by title, folder, branch or ID"

	m := Model{
		cfg:        cfg,
		source:     source,
		home:       home,
		now:        time.Now,
		st:         newStyles(theme.Get(cfg.Theme, true)),
		rows:       rows,
		detailOpen: true,
		filter:     fi,
		preview:    viewport.New(),
		details:    map[string]*db.Detail{},
		detailH:    max(config.MinDetailHeight, cfg.DetailHeight),
	}
	m.refresh()
	return m
}

// RememberIn makes the model keep its state (the detail pane height) in the
// state file at path, starting from what is saved there.
func (m Model) RememberIn(path string) Model {
	m.statePath = path
	if st := config.LoadState(path); st.DetailHeight >= config.MinDetailHeight {
		m.detailH = st.DetailHeight
	}
	return m
}

func (m *Model) saveState() tea.Cmd {
	if m.statePath == "" {
		return nil
	}
	path, st := m.statePath, config.State{DetailHeight: m.detailH}
	return func() tea.Msg {
		_ = config.SaveState(path, st)
		return nil
	}
}

// maxDetailH is the tallest pane that leaves the list a few rows.
func (m Model) maxDetailH() int {
	return max(config.MinDetailHeight, m.height-m.chromeLines()-minListRows)
}

func (m *Model) resizeDetail(h int) {
	m.detailH = max(config.MinDetailHeight, min(h, m.maxDetailH()))
	m.clamp()
}

// paneTop is the screen row of the detail pane's top edge, or -1.
func (m Model) paneTop() int {
	if m.paneHeight() == 0 {
		return -1
	}
	return m.height - footerLines - m.paneHeight()
}

// loadDetail fetches the detail pane's data for the selected session the
// first time it is shown.
func (m *Model) loadDetail() {
	r := m.current()
	if !m.detailOpen || r == nil {
		m.focus = focusList
		return
	}
	if r.s.ID != m.scrollFor {
		m.scroll, m.scrollFor = [numFocus]int{}, r.s.ID
	}
	if _, ok := m.details[r.s.ID]; ok {
		return
	}
	d, err := m.source.SessionDetail(r.s.ID)
	if err != nil {
		d = nil
	}
	m.details[r.s.ID] = d
}

func (m Model) Init() tea.Cmd { return tea.RequestBackgroundColor }

// refresh recomputes the visible rows from the filter and sort order, keeping
// the selected session selected when it is still visible.
func (m *Model) refresh() {
	var keep string
	if r := m.current(); r != nil {
		keep = r.s.ID
	}
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	m.visible = m.visible[:0]
	for i := range m.rows {
		if q == "" || matches(m.rows[i].search, q) {
			m.visible = append(m.visible, i)
		}
	}
	less := sorts[m.sortIdx].less
	slices.SortStableFunc(m.visible, func(a, b int) int { return less(&m.rows[a], &m.rows[b]) })

	m.cursor = 0
	for i, idx := range m.visible {
		if m.rows[idx].s.ID == keep {
			m.cursor = i
			break
		}
	}
	m.clamp()
}

// matches requires every space-separated word of q to appear in text.
func matches(text, q string) bool {
	for w := range strings.FieldsSeq(q) {
		if !strings.Contains(text, w) {
			return false
		}
	}
	return true
}

func (m *Model) current() *row {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return nil
	}
	return &m.rows[m.visible[m.cursor]]
}

func (m *Model) move(delta int) {
	m.cursor += delta
	m.clamp()
}

func (m *Model) clamp() {
	m.cursor = max(0, min(m.cursor, len(m.visible)-1))
	h := m.listHeight()
	if h <= 0 {
		m.offset = 0
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	m.offset = max(0, min(m.offset, max(0, len(m.visible)-h)))
}

func (m *Model) showToast(kind toastKind, text string) tea.Cmd {
	m.toast, m.toastKind = text, kind
	m.toastID++
	id := m.toastID
	return tea.Tick(toastFor, func(time.Time) tea.Msg { return toastExpired{id} })
}

func copyCmd(text string) tea.Cmd {
	// OSC 52 works over SSH and in most terminals; pbcopy covers terminals
	// that ignore it on macOS.
	cmds := []tea.Cmd{tea.SetClipboard(text)}
	if runtime.GOOS == "darwin" {
		if path, err := exec.LookPath("pbcopy"); err == nil {
			cmds = append(cmds, func() tea.Msg {
				c := exec.Command(path)
				c.Stdin = strings.NewReader(text)
				_ = c.Run()
				return nil
			})
		}
	}
	return tea.Batch(cmds...)
}

func resumeCommand(r *row) string {
	return "cd " + shellQuote(r.s.ProjectPath) + " && claude -r " + r.s.ID
}

func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(c rune) bool {
		return !(c == '/' || c == '.' || c == '-' || c == '_' || c == '~' ||
			c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9')
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// action handles the keys that work on the selected session in both the list
// and the preview.
func (m *Model) action(key string) (tea.Cmd, bool) {
	r := m.current()
	if r == nil {
		return nil, false
	}
	switch key {
	case "enter":
		if r.gone {
			return m.showToast(toastWarn, "Folder no longer exists: "+tildePath(r.s.ProjectPath, m.home)), true
		}
		m.Result = &Resume{Dir: r.s.ProjectPath, SessionID: r.s.ID}
		return tea.Quit, true
	case "y":
		return tea.Batch(copyCmd(r.s.ID), m.showToast(toastOK, "Copied session ID "+r.s.ID)), true
	case "Y":
		cmd := resumeCommand(r)
		return tea.Batch(copyCmd(cmd), m.showToast(toastOK, "Copied "+cmd)), true
	}
	return nil, false
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	if nm, ok := next.(Model); ok {
		nm.loadDetail()
		return nm, cmd
	}
	return next, cmd
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft && m.mode == modeList {
			m.click(msg.X, msg.Y)
		}
		return m, nil
	case tea.MouseMotionMsg:
		if m.dragging {
			m.resizeDetail(m.height - footerLines - msg.Y)
		}
		return m, nil
	case tea.MouseReleaseMsg:
		if m.dragging {
			m.dragging = false
			return m, m.saveState()
		}
		return m, nil
	case tea.MouseWheelMsg:
		if m.mode == modePreview {
			var cmd tea.Cmd
			m.preview, cmd = m.preview.Update(msg)
			return m, cmd
		}
		step := 1
		if msg.Button == tea.MouseWheelUp {
			step = -1
		}
		if f := m.frameAt(msg.X, msg.Y); f != focusList {
			m.scrollFrame(f, 3*step)
		} else {
			m.move(step)
		}
		return m, nil
	case tea.BackgroundColorMsg:
		m.st = newStyles(theme.Get(m.cfg.Theme, msg.IsDark()))
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.filter.SetWidth(max(10, m.width-30))
		m.sizePreview()
		m.clamp()
		return m, nil
	case toastExpired:
		if msg.id == m.toastID {
			m.toast = ""
		}
		return m, nil
	case tea.KeyPressMsg:
		switch m.mode {
		case modeFilter:
			return m.updateFilter(msg)
		case modePreview:
			return m.updatePreview(msg)
		default:
			return m.updateList(msg)
		}
	}
	return m, nil
}

func (m Model) updateList(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if cmd, ok := m.action(key); ok {
		return m, cmd
	}
	switch key {
	case "]":
		m.focus = (m.focus + 1) % numFocus
		if !m.detailOpen {
			m.focus = focusList
		}
		return m, nil
	case "[":
		m.focus = (m.focus + numFocus - 1) % numFocus
		if !m.detailOpen {
			m.focus = focusList
		}
		return m, nil
	}
	if m.focus != focusList {
		if rects, ok := m.paneRects(); ok {
			page := max(1, rects[m.focus].h-4)
			switch key {
			case "esc":
				m.focus = focusList
			case "down", "j", "ctrl+n":
				m.scrollFrame(m.focus, 1)
			case "up", "k", "ctrl+p":
				m.scrollFrame(m.focus, -1)
			case "pgdown", "ctrl+f", "ctrl+d":
				m.scrollFrame(m.focus, page)
			case "pgup", "ctrl+b", "ctrl+u":
				m.scrollFrame(m.focus, -page)
			case "home", "g":
				m.scrollFrame(m.focus, -1<<20)
			case "end", "G":
				m.scrollFrame(m.focus, 1<<20)
			default:
				goto list
			}
			return m, nil
		}
		m.focus = focusList
	}
list:
	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "down", "j", "ctrl+n":
		m.move(1)
	case "up", "k", "ctrl+p":
		m.move(-1)
	case "pgdown", "ctrl+f":
		m.move(max(1, m.listHeight()))
	case "pgup", "ctrl+b":
		m.move(-max(1, m.listHeight()))
	case "home", "g":
		m.move(-len(m.visible))
	case "end", "G":
		m.move(len(m.visible))
	case "tab":
		m.detailOpen = !m.detailOpen
		m.focus = focusList
		m.clamp()
	case "+", "=":
		m.resizeDetail(m.detailH + 2)
		return m, m.saveState()
	case "-":
		m.resizeDetail(m.detailH - 2)
		return m, m.saveState()
	case "s":
		m.sortIdx = (m.sortIdx + 1) % len(sorts)
		m.refresh()
		m.cursor, m.offset = 0, 0
	case "/":
		m.mode = modeFilter
		return m, m.filter.Focus()
	case "esc":
		if m.filter.Value() != "" {
			m.filter.SetValue("")
			m.refresh()
		}
	case "space":
		return m, m.openPreview()
	}
	return m, nil
}

func (m Model) updateFilter(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filter.SetValue("")
		m.filter.Blur()
		m.mode = modeList
		m.refresh()
		return m, nil
	case "enter":
		m.filter.Blur()
		m.mode = modeList
		return m, nil
	case "down", "ctrl+n":
		m.move(1)
		return m, nil
	case "up", "ctrl+p":
		m.move(-1)
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	}
	var cmd tea.Cmd
	before := m.filter.Value()
	m.filter, cmd = m.filter.Update(msg)
	if m.filter.Value() != before {
		m.refresh()
	}
	return m, cmd
}

func (m Model) updatePreview(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if cmd, ok := m.action(key); ok {
		return m, cmd
	}
	switch key {
	case "space", "esc", "q":
		m.mode = modeList
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "g", "home":
		m.preview.GotoTop()
		return m, nil
	case "G", "end":
		m.preview.GotoBottom()
		return m, nil
	}
	var cmd tea.Cmd
	m.preview, cmd = m.preview.Update(msg)
	return m, cmd
}

func (m *Model) openPreview() tea.Cmd {
	r := m.current()
	if r == nil {
		return nil
	}
	p, err := m.source.SessionPreview(r.s.ID, previewHead, previewTail)
	if err != nil {
		return m.showToast(toastWarn, "Could not load the conversation: "+err.Error())
	}
	m.mode = modePreview
	m.previewFor = r.s.ID
	m.sizePreview()
	m.preview.SetContent(m.renderConversation(p, m.width))
	m.preview.GotoTop()
	return nil
}

func (m *Model) sizePreview() {
	m.preview.SetWidth(m.width)
	m.preview.SetHeight(max(1, m.height-previewChrome))
}

// listTop is the screen row of the first session row.
func (m Model) listTop() int {
	top := 1 + 3 // header bar, rule, column headers, rule
	if m.filterShown() {
		top++
	}
	return top
}

// frameAt returns the frame under a screen cell, or focusList.
func (m Model) frameAt(x, y int) focus {
	if rects, ok := m.paneRects(); ok {
		for f := focusConv; f < numFocus; f++ {
			if rects[f].contains(x, y) {
				return f
			}
		}
	}
	return focusList
}

// click handles a left click in the list: the pane's top edge (or the row
// count line just above it) starts a resize, a frame takes focus, a session
// row is selected.
func (m *Model) click(x, y int) {
	if top := m.paneTop(); top >= 0 && (y == top || y == top-1) {
		m.dragging = true
		return
	}
	if f := m.frameAt(x, y); f != focusList {
		m.focus = f
		return
	}
	if i := y - m.listTop(); x < m.listWidth() && i >= 0 && i < m.listHeight() && m.offset+i < len(m.visible) {
		m.cursor = m.offset + i
		m.focus = focusList
		m.clamp()
	}
}
