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

	"charm.land/bubbles/v2/key"
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
	// SessionMessages is a session's whole conversation, for searching it.
	SessionMessages(sessionID string) ([]db.Message, error)
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
	// conv is the search in the spread conversation.
	conv convSearch
	// km is which keys do what.
	km keyMap

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

	// details caches the detail pane's data per session ID; detailLoading
	// has the ones being read in the background, when background is set.
	details       map[string]*db.Detail
	detailLoading map[string]bool
	background    bool
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

	// settling holds off drawing until the terminal's size settles; sizes
	// counts the size reports so far.
	settling bool
	sizes    int

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
	fi.Placeholder = "filter by title, folder, branch, ID or what was said · folder:name · text:word"

	ss := textinput.New()
	ss.Prompt = "/ "
	ss.Placeholder = "search folders"
	ss.SetWidth(sidebarWidth - 12)

	m := Model{
		ask:           askState{input: newAskInput()},
		askRun:        claudeRunner([]string{"recall", "mcp"}, config.ModelID(cfg.AskModel), ""),
		reasons:       map[string]string{},
		sideSearch:    ss,
		conv:          newConvSearch(),
		km:            defaultKeyMap(),
		resolver:      resolver,
		folders:       groupRows(rows),
		cfg:           cfg,
		source:        source,
		home:          home,
		now:           time.Now,
		st:            newStyles(theme.Get(cfg.Theme, true)),
		rows:          rows,
		filter:        fi,
		expandRows:    defaultExpandRows,
		details:       map[string]*db.Detail{},
		detailLoading: map[string]bool{},
		text:          textSearch{found: map[string]map[string]bool{}, pending: map[string]bool{}, delay: textSearchDelay},
		detailH:       max(config.MinDetailHeight, cfg.DetailHeight),
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
func (m *Model) loadDetail() tea.Cmd {
	r := m.current()
	if r == nil {
		if m.focus != focusFolders {
			m.focus = focusList
		}
		return nil
	}
	if r.s.ID != m.scrollFor {
		m.scroll, m.scrollFor = [numFocus]int{}, r.s.ID
	}
	if _, ok := m.details[r.s.ID]; ok || m.detailLoading[r.s.ID] {
		return nil
	}
	if !m.background {
		d, err := m.source.SessionDetail(r.s.ID)
		if err != nil {
			d = nil
		}
		m.details[r.s.ID] = d
		return nil
	}
	// Reading a long session's detail can take a moment the first time;
	// the list keeps moving meanwhile, and the pane says it is loading.
	id, src := r.s.ID, m.source
	m.detailLoading[id] = true
	return func() tea.Msg {
		d, err := src.SessionDetail(id)
		if err != nil {
			d = nil
		}
		return detailLoaded{id, d}
	}
}

// detailLoaded brings a session's detail read in the background.
type detailLoaded struct {
	id string
	d  *db.Detail
}

// LoadInBackground makes the model read each session's detail off the
// keys, so moving onto a long session does not hold the TUI up. Only recall
// itself does; the tests read it in place.
func (m Model) LoadInBackground() Model {
	m.background = true
	return m
}

func (m Model) Init() tea.Cmd {
	if m.settling {
		return tea.Batch(tea.RequestBackgroundColor, tea.Tick(settleWait, func(time.Time) tea.Msg { return settledMsg{} }))
	}
	return tea.RequestBackgroundColor
}

// Some terminals first report a size a column off and the right one a few
// milliseconds later, through the in-band resize Bubble Tea turns on;
// drawn at the first, the columns right of the flexible ones jump when the
// second arrives. Waiting for that second report, or for settleWait in a
// terminal that sends none, draws once at the right size.
const settleWait = 100 * time.Millisecond

type settledMsg struct{}

// SettleSize makes the model wait for the terminal's size to settle before
// it first draws.
func (m Model) SettleSize() Model {
	m.settling = true
	return m
}

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
		// Claude's answer, or folder:, picks the sessions itself, over the
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

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	if nm, ok := next.(Model); ok {
		if nm.focus == focusFolders && !nm.sidebarShown() {
			nm.focus = focusList
		}
		load := nm.loadDetail()
		nm.readLines()
		return nm, tea.Batch(cmd, load, nm.scheduleTextSearch(), nm.convLoadCmd())
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
	case settledMsg:
		m.settling = false
		return m, nil
	case tea.WindowSizeMsg:
		if m.sizes++; m.sizes >= 2 {
			m.settling = false
		}
		m.width, m.height = msg.Width, msg.Height
		m.filter.SetWidth(max(10, m.width-30))
		m.clamp()
		return m, nil
	case askStepMsg, askDoneMsg, askTickMsg:
		return m, m.askMsg(msg)
	case convTick, convLoaded:
		m.convMsg(msg)
		return m, nil
	case detailLoaded:
		delete(m.detailLoading, msg.id)
		m.details[msg.id] = msg.d
		if r := m.current(); r != nil && r.s.ID == msg.id {
			m.readFor = "" // the spread conversation, with it
		}
		return m, nil
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
		// ctrl+c quits from anywhere, a running ask stopped first.
		if msg.String() == "ctrl+c" {
			if m.ask.stage != askClosed {
				m.closeAsk()
			}
			return m, tea.Quit
		}
		if m.ask.stage != askClosed {
			return m.updateAsk(msg)
		}
		if m.sortMenu {
			return m.updateSortMenu(msg)
		}
		if m.helpOpen {
			// The key list closes on esc, q or the key that opened it.
			if s := msg.String(); s == "esc" || s == "q" || key.Matches(msg, m.km.Global.Help) {
				m.helpOpen = false
			}
			return m, nil
		}
		if key.Matches(msg, m.km.Global.Help) && m.mode != modeFilter && !(m.focus == focusFolders && m.sideTyping) && !m.conv.typing {
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
	if m.conv.typing {
		return m.updateConvSearch(msg)
	}
	// A frame with no room to show is the list's.
	if f := m.focus; f != focusList && f != focusFolders {
		if _, ok := m.paneRects(); !ok {
			m.focus = focusList
		}
	}
	// The focused pane first; what it does not take, the keys that work
	// anywhere.
	var cmd tea.Cmd
	var ok bool
	switch m.keyContext() {
	case ctxList:
		cmd, ok = m.listKey(msg)
	case ctxFrame, ctxReading:
		cmd, ok = m.frameKey(msg)
	case ctxFolders:
		cmd, ok = m.folderKey(msg)
	}
	if !ok {
		cmd = m.globalKey(msg)
	}
	return m, cmd
}

// globalKey handles a key that works in any pane.
func (m *Model) globalKey(msg tea.KeyPressMsg) tea.Cmd {
	g := m.km.Global
	switch {
	case key.Matches(msg, g.Quit):
		return tea.Quit
	case key.Matches(msg, g.FocusNext):
		m.cycleFocus(1)
	case key.Matches(msg, g.FocusPrev):
		m.cycleFocus(-1)
	case key.Matches(msg, g.Scope):
		return m.toggleScope()
	case key.Matches(msg, g.Ask):
		return m.openAsk()
	case key.Matches(msg, g.Sort):
		m.openSortMenu()
	}
	return nil
}

// sessionKey handles a key that acts on the selected session, from the
// list, a frame or the spread conversation.
func (m *Model) sessionKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	s := m.km.Session
	switch {
	case key.Matches(msg, s.Read):
		m.toggleExpand()
		return nil, true
	case key.Matches(msg, s.Grow):
		m.resizeDetail(m.paneHeight() + 2)
		return m.saveState(), true
	case key.Matches(msg, s.Shrink):
		m.resizeDetail(m.paneHeight() - 2)
		return m.saveState(), true
	}
	r := m.current()
	if r == nil {
		return nil, false
	}
	switch {
	case key.Matches(msg, s.Resume):
		if r.gone {
			return m.showToast(toastWarn, "Folder no longer exists: "+tildePath(r.s.ProjectPath, m.home)), true
		}
		m.Result = &Resume{Dir: r.s.ProjectPath, SessionID: r.s.ID}
		return tea.Quit, true
	case key.Matches(msg, s.CopyID):
		return tea.Batch(copyCmd(r.s.ID), m.showToast(toastOK, "Copied session ID "+r.s.ID)), true
	case key.Matches(msg, s.CopyCommand):
		cmd := resumeCommand(r)
		return tea.Batch(copyCmd(cmd), m.showToast(toastOK, "Copied "+cmd)), true
	}
	return nil, false
}

// openFilter starts typing the session list's filter.
func (m *Model) openFilter() tea.Cmd {
	m.mode = modeFilter
	return m.filter.Focus()
}

// listKey handles a key on the session list.
func (m *Model) listKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if cmd, ok := m.sessionKey(msg); ok {
		return cmd, true
	}
	n, l := m.km.Nav, m.km.List
	switch {
	case key.Matches(msg, n.Down):
		m.move(1)
	case key.Matches(msg, n.Up):
		m.move(-1)
	case key.Matches(msg, n.PageDown):
		m.move(max(1, m.listRows()))
	case key.Matches(msg, n.PageUp):
		m.move(-max(1, m.listRows()))
	case key.Matches(msg, n.Top):
		m.move(-len(m.visible))
	case key.Matches(msg, n.Bottom):
		m.move(len(m.visible))
	case key.Matches(msg, n.Search):
		return m.openFilter(), true
	case msg.String() == "esc":
		// One step back: the filter, then Claude's answer, then the spread
		// conversation.
		switch {
		case m.filter.Value() != "":
			m.filter.SetValue("")
			m.refresh()
		case m.asked != nil:
			m.asked = nil
			m.refresh()
		case m.expanded:
			m.toggleExpand()
		}
	// ← ← (h h) opens the folder list and moves into it, → → (l l) comes
	// back and closes it.
	case key.Matches(msg, l.FoldersOpen):
		if m.sidebarShown() {
			m.focus = focusFolders
			return nil, true
		}
		return m.openSidebar(false), true
	case key.Matches(msg, l.FoldersClose):
		if m.sidebarShown() {
			return m.closeSidebar(), true
		}
	default:
		return nil, false
	}
	return nil, true
}

// frameKey handles a key on a detail frame, or on the Conversation spread
// over the pane, which searches itself.
func (m *Model) frameKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if cmd, ok := m.sessionKey(msg); ok {
		return cmd, true
	}
	reading := m.keyContext() == ctxReading
	rects, _ := m.paneRects()
	page := max(1, rects[m.focus].h-4)
	n := m.km.Nav
	switch {
	case key.Matches(msg, n.Down):
		m.scrollFrame(m.focus, 1)
	case key.Matches(msg, n.Up):
		m.scrollFrame(m.focus, -1)
	case key.Matches(msg, n.PageDown):
		m.scrollFrame(m.focus, page)
	case key.Matches(msg, n.PageUp):
		m.scrollFrame(m.focus, -page)
	case key.Matches(msg, n.Top):
		m.scrollFrame(m.focus, -1<<20)
	case key.Matches(msg, n.Bottom):
		m.scrollFrame(m.focus, 1<<20)
	case key.Matches(msg, n.Search):
		// The spread conversation searches itself; a frame, which has no
		// search, opens the list's filter.
		if reading {
			return m.startConvSearch(), true
		}
		return m.openFilter(), true
	case reading && len(m.conv.hits) > 0 && key.Matches(msg, n.NextMatch):
		m.nextConvHit(1)
	case reading && len(m.conv.hits) > 0 && key.Matches(msg, n.PrevMatch):
		m.nextConvHit(-1)
	case msg.String() == "esc":
		switch {
		case reading && m.conv.input.Value() != "":
			m.clearConvSearch()
		case m.expanded:
			m.toggleExpand()
		default:
			m.focus = focusList
		}
	default:
		return nil, false
	}
	return nil, true
}

// folderKey handles a key in the folder list.
func (m *Model) folderKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	page := max(1, m.sidebarRows()-1)
	n := m.km.Nav
	switch {
	case key.Matches(msg, n.Down):
		m.moveFolder(1)
	case key.Matches(msg, n.Up):
		m.moveFolder(-1)
	case key.Matches(msg, n.PageDown):
		m.moveFolder(page)
	case key.Matches(msg, n.PageUp):
		m.moveFolder(-page)
	case key.Matches(msg, n.Top):
		m.moveFolder(-len(m.folders) - 1)
	case key.Matches(msg, n.Bottom):
		m.moveFolder(len(m.folders) + 1)
	case key.Matches(msg, n.Search):
		return m.searchFolders(), true
	case msg.String() == "esc":
		if m.sideSearch.Value() != "" {
			m.clearSideSearch()
			break
		}
		m.focus = focusList
	case key.Matches(msg, m.km.Folders.FoldersBack):
		m.focus = focusList
	default:
		return nil, false
	}
	return nil, true
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
