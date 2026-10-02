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

// Source loads what the detail pane shows about a session.
type Source interface {
	SessionDetail(sessionID string) (*db.Detail, error)
	// SessionsWithText lists the sessions whose conversation contains text.
	SessionsWithText(text string) ([]string, error)
}

type mode int

const (
	modeList mode = iota
	modeFilter
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
	toastFor = 2500 * time.Millisecond
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

	resolver *worktree.Resolver
	// folders are what the list can be narrowed to; scope is the chosen
	// one's key ("" for all) and startFolder the one the TUI started in.
	folders []folderInfo
	// branches and worktrees are the values the filter suggests for
	// branch: and worktree:.
	branches, worktrees []sideEntry
	scope               string
	startFolder         string
	sidebar             bool // the folder list is open
	sideOffset          int
	// sideSearch narrows the folder list; sideTyping is set while it has
	// the keys.
	sideSearch textinput.Model
	sideTyping bool

	cursor, offset int
	width, height  int
	mode           mode
	sortIdx        int

	filter textinput.Model
	text   textSearch
	comp   completion
	sugOff int // first suggestion shown
	sugSel int // highlighted suggestion
	// sugHidden is set when Esc closes the suggestions, until the filter
	// changes.
	sugHidden bool

	// expanded spreads Conversation over the pane, leaving expandRows list
	// rows; read is its content, rendered for readFor.
	expanded   bool
	expandRows int
	read       []string
	readFor    string

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

	helpOpen bool // the key list is showing

	// ask is the ask-Claude box, askRun how it asks. reasons are why Claude
	// picked each session found so far; asked narrows the list to the last
	// answer's sessions, in Claude's order, for the question askedFor.
	ask      askState
	askRun   askRunner
	reasons  map[string]string
	asked    map[string]int
	askedFor string
	// sortMenu is set while the sort menu shows, sortSel its highlight.
	sortMenu bool
	sortSel  int

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
	fi.Placeholder = "filter by title, folder, branch, ID or what was said · in:folder · text:word"

	ss := textinput.New()
	ss.Prompt = "/ "
	ss.Placeholder = "search folders"
	ss.SetWidth(sidebarWidth - 12)

	m := Model{
		ask:        askState{input: newAskInput()},
		askRun:     claudeRunner([]string{"recall", "mcp"}, config.ModelID(cfg.AskModel), ""),
		reasons:    map[string]string{},
		sideSearch: ss,
		resolver:   resolver,
		folders:    groupRows(rows),
		cfg:        cfg,
		source:     source,
		home:       home,
		now:        time.Now,
		st:         newStyles(theme.Get(cfg.Theme, true)),
		rows:       rows,
		filter:     fi,
		expandRows: defaultExpandRows,
		details:    map[string]*db.Detail{},
		text:       textSearch{found: map[string]map[string]bool{}, pending: map[string]bool{}, delay: textSearchDelay},
		detailH:    max(config.MinDetailHeight, cfg.DetailHeight),
	}
	m.branches = values(m.rows, func(r *row) string { return r.s.GitBranch })
	m.worktrees = values(m.rows, func(r *row) string { return r.worktree })
	m.refresh()
	return m
}

// RememberIn makes the model keep its state (the detail pane height) in the
// state file at path, starting from what is saved there.
func (m Model) RememberIn(path string) Model {
	m.statePath = path
	st := config.LoadState(path)
	if st.DetailHeight >= config.MinDetailHeight {
		m.detailH = st.DetailHeight
	}
	m.sidebar = st.Sidebar
	if st.ExpandRows >= minListRows {
		m.expandRows = st.ExpandRows
	}
	return m
}

func (m *Model) saveState() tea.Cmd {
	if m.statePath == "" {
		return nil
	}
	path, st := m.statePath, config.State{DetailHeight: m.detailH, Sidebar: m.sidebar, ExpandRows: m.expandRows}
	return func() tea.Msg {
		_ = config.SaveState(path, st)
		return nil
	}
}

// maxDetailH is the tallest pane that leaves the list a few rows.
func (m Model) maxDetailH() int {
	return max(config.MinDetailHeight, m.height-m.chromeLines()-minListRows)
}

// resizeDetail sets the pane's height; while Conversation is spread, that
// sets the list rows left above it instead.
func (m *Model) resizeDetail(h int) {
	h = max(config.MinDetailHeight, min(h, m.maxDetailH()))
	if m.expanded {
		m.expandRows = max(minListRows, m.height-m.chromeLines()-h)
	} else {
		m.detailH = h
	}
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
	if r == nil {
		if m.focus != focusFolders {
			m.focus = focusList
		}
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
	q := parseQuery(m.filter.Value())
	m.visible = m.visible[:0]
	for i := range m.rows {
		// Claude's answer, or in:, picks the sessions itself, over the
		// folder the list is narrowed to.
		if m.asked != nil {
			if _, ok := m.asked[m.rows[i].s.ID]; !ok {
				continue
			}
		} else if m.scope != "" && len(q.in) == 0 && m.rows[i].group != m.scope {
			continue
		}
		if m.match(q, &m.rows[i]) {
			m.visible = append(m.visible, i)
		}
	}
	less := sorts[m.sortIdx].less
	if m.asked != nil { // Claude's best first
		less = func(a, b *row) int { return cmp.Compare(m.asked[a.s.ID], m.asked[b.s.ID]) }
	}
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
	h := m.listRows()
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
		if nm.focus == focusFolders && !nm.sidebarShown() {
			nm.focus = focusList
		}
		nm.loadDetail()
		nm.readLines()
		return nm, tea.Batch(cmd, nm.scheduleTextSearch())
	}
	return next, cmd
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if m.helpOpen {
			m.helpOpen = false
			return m, nil
		}
		if m.ask.stage != askClosed {
			return m, nil
		}
		if m.sortMenu {
			if i := m.sortMenuAt(msg.X, msg.Y); i >= 0 {
				m.applySort(i)
			} else {
				m.sortMenu = false
			}
			return m, nil
		}
		if msg.Button == tea.MouseLeft && m.mode == modeList {
			m.click(msg.X, msg.Y)
		}
		if i := m.suggestionAt(msg.X, msg.Y); msg.Button == tea.MouseLeft && i >= 0 {
			m.pickSuggestion(i)
			m.refresh()
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
		step := 1
		if msg.Button == tea.MouseWheelUp {
			step = -1
		}
		if r, _, _, _, ok := m.suggestRect(); ok && r.contains(msg.X, msg.Y) {
			m.scrollSuggestions(step)
		} else if m.sidebarAt(msg.X, msg.Y) >= 0 {
			m.scrollSidebar(3 * step)
		} else if f := m.frameAt(msg.X, msg.Y); f != focusList {
			m.scrollFrame(f, 3*step)
		} else {
			m.move(step)
		}
		return m, nil
	case tea.BackgroundColorMsg:
		m.st = newStyles(theme.Get(m.cfg.Theme, msg.IsDark()))
		m.readFor = "" // drawn in the old colors
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.filter.SetWidth(max(10, m.width-30))
		m.clamp()
		return m, nil
	case askStepMsg, askDoneMsg, askTickMsg:
		return m, m.askMsg(msg)
	case textSearchTick:
		return m, m.startTextSearch(msg)
	case textSearchDone:
		return m, m.finishTextSearch(msg)
	case toastExpired:
		if msg.id == m.toastID {
			m.toast = ""
		}
		return m, nil
	case tea.KeyPressMsg:
		if m.ask.stage != askClosed {
			return m.updateAsk(msg)
		}
		if m.sortMenu {
			return m.updateSortMenu(msg)
		}
		if m.helpOpen {
			switch msg.String() {
			case "?", "esc", "q":
				m.helpOpen = false
			case "ctrl+c":
				return m, tea.Quit
			}
			return m, nil
		}
		if msg.String() == "?" && m.mode != modeFilter && !(m.focus == focusFolders && m.sideTyping) {
			m.helpOpen = true
			return m, nil
		}
		switch m.mode {
		case modeFilter:
			return m.updateFilter(msg)
		default:
			return m.updateList(msg)
		}
	}
	return m, nil
}

func (m Model) updateList(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.focus == focusFolders && m.sideTyping {
		return m.updateSideSearch(msg)
	}
	key := msg.String()
	switch key {
	case "tab", "]":
		m.cycleFocus(1)
		return m, nil
	case "shift+tab", "[":
		m.cycleFocus(-1)
		return m, nil
	case ".":
		return m, m.toggleScope()
	case "a":
		return m, m.openAsk()
	}
	if m.focus == focusFolders {
		page := max(1, m.sidebarRows()-1)
		switch key {
		case "down", "j", "ctrl+n":
			m.moveFolder(1)
		case "up", "k", "ctrl+p":
			m.moveFolder(-1)
		case "pgdown", "ctrl+f", "ctrl+d":
			m.moveFolder(page)
		case "pgup", "ctrl+b", "ctrl+u":
			m.moveFolder(-page)
		case "home", "g":
			m.moveFolder(-len(m.folders) - 1)
		case "end", "G":
			m.moveFolder(len(m.folders) + 1)
		case "/":
			return m, m.searchFolders()
		case "esc":
			if m.sideSearch.Value() != "" {
				m.clearSideSearch()
				break
			}
			m.focus = focusList
		case "enter", "right", "l":
			m.focus = focusList
		case "q", "ctrl+c":
			return m, tea.Quit
		}
		return m, nil
	}
	if cmd, ok := m.action(key); ok {
		return m, cmd
	}
	if m.focus != focusList {
		if rects, ok := m.paneRects(); ok {
			page := max(1, rects[m.focus].h-4)
			switch key {
			case "esc":
				if m.expanded {
					m.toggleExpand()
					break
				}
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
		m.move(max(1, m.listRows()))
	case "pgup", "ctrl+b":
		m.move(-max(1, m.listRows()))
	case "home", "g":
		m.move(-len(m.visible))
	case "end", "G":
		m.move(len(m.visible))
	case "+", "=":
		m.resizeDetail(m.paneHeight() + 2)
		return m, m.saveState()
	case "-":
		m.resizeDetail(m.paneHeight() - 2)
		return m, m.saveState()
	case "s":
		// The list's own key: not while a frame is being read.
		if m.focus == focusList {
			m.openSortMenu()
		}
	case "/":
		m.mode = modeFilter
		return m, m.filter.Focus()
	case "esc":
		switch {
		case m.filter.Value() != "":
			m.filter.SetValue("")
			m.refresh()
		case m.asked != nil:
			m.asked = nil
			m.refresh()
		}
	case "space":
		m.toggleExpand()
	// ← ← (h h) opens the folder list and moves into it, → → (l l) comes
	// back and closes it.
	case "left", "h":
		if m.focus != focusList {
			break
		}
		if m.sidebarShown() {
			m.focus = focusFolders
			return m, nil
		}
		return m, m.openSidebar(false)
	case "right", "l":
		if m.focus == focusList && m.sidebarShown() {
			return m, m.closeSidebar()
		}
	}
	return m, nil
}

// cycleFocus moves the focus along the folder list (when shown), the
// session list and the detail pane's frames, in the order they are laid
// out: below the list Details is under Conversation and What was done on
// the right; beside it What was done comes second.
func (m *Model) cycleFocus(delta int) {
	var order []focus
	if m.sidebarShown() {
		order = append(order, focusFolders)
	}
	order = append(order, focusList)
	if m.current() != nil {
		if m.expanded {
			order = append(order, focusConv)
		} else if m.detailRight() {
			order = append(order, focusConv, focusDone, focusDetails)
		} else {
			order = append(order, focusConv, focusDetails, focusDone)
		}
	}
	i := max(0, slices.Index(order, m.focus))
	m.focus = order[(i+delta+len(order))%len(order)]
}

func (m Model) updateFilter(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// While folder suggestions show, the arrows, Enter and Esc act on them.
	if list, _ := m.suggestions(); len(list) > 0 {
		switch msg.String() {
		case "down", "ctrl+n":
			m.moveSuggestion(1)
			m.refresh()
			return m, nil
		case "up", "ctrl+p":
			m.moveSuggestion(-1)
			m.refresh()
			return m, nil
		case "pgdown":
			m.moveSuggestion(maxSuggest)
			m.refresh()
			return m, nil
		case "pgup":
			m.moveSuggestion(-maxSuggest)
			m.refresh()
			return m, nil
		case "enter":
			m.acceptSuggestion()
			m.refresh()
			return m, nil
		case "esc":
			m.sugHidden = true
			return m, nil
		}
	}
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
	case "tab", "right":
		// The hinted key first; then tab completes a value.
		if m.acceptKeyHint() {
			m.refresh()
			return m, nil
		}
		if msg.String() == "right" {
			break
		}
		if m.complete(1) {
			m.refresh()
		}
		return m, nil
	case "shift+tab":
		if m.complete(-1) {
			m.refresh()
		}
		return m, nil
	}
	var cmd tea.Cmd
	before := m.filter.Value()
	m.filter, cmd = m.filter.Update(msg)
	if m.filter.Value() != before {
		m.sugOff, m.sugSel, m.sugHidden = 0, 0, false
		m.refresh()
	}
	return m, cmd
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
	if i := m.sidebarAt(x, y); i >= 0 {
		m.pickFolder(i)
		m.focus = focusFolders
		return
	}
	// Anywhere else, a search being typed in the folder list stops.
	m.sideTyping = false
	m.sideSearch.Blur()
	if f := m.frameAt(x, y); f != focusList {
		m.focus = f
		return
	}
	if i := (y - m.listTop()) / m.rowLines(); y >= m.listTop() && x >= m.listLeft() && x < m.listLeft()+m.listWidth() && i < m.listRows() && m.offset+i < len(m.visible) {
		m.cursor = m.offset + i
		m.focus = focusList
		m.clamp()
	}
}
