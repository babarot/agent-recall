package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/babarot/claude-recall/internal/config"
	"github.com/babarot/claude-recall/internal/db"
)

// folderFixture lays out a repository with a subdirectory and a linked
// worktree, a removed herdr worktree of it, and a folder outside git.
type folderFixture struct {
	repo, sub, wt, herdr, other string
}

func newFolderFixture(t *testing.T) folderFixture {
	t.Helper()
	base := t.TempDir()
	f := folderFixture{
		repo:  filepath.Join(base, "src", "me", "app"),
		wt:    filepath.Join(base, "wt", "feat"),
		herdr: filepath.Join(base, ".herdr", "worktrees", "app", "worktree-old"),
		other: filepath.Join(base, "notes"),
	}
	f.sub = filepath.Join(f.repo, "docs")
	admin := filepath.Join(f.repo, ".git", "worktrees", "feat")
	for _, d := range []string{f.sub, admin, f.wt, f.other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(f.wt, ".git"), []byte("gitdir: "+admin+"\n"), 0o644)
	os.WriteFile(filepath.Join(admin, "commondir"), []byte("../..\n"), 0o644)
	return f
}

func (f folderFixture) sessions() []db.Session {
	at := func(h int) time.Time { return now.Add(time.Duration(-h) * time.Hour) }
	s := func(id, dir string, h int) db.Session {
		return db.Session{ID: id, ProjectPath: dir, Title: "session " + id, MessageCount: 1, StartedAt: at(h + 1), EndedAt: at(h)}
	}
	return []db.Session{
		s("repo-1", f.repo, 1), s("sub-1", f.sub, 2), s("wt-1", f.wt, 3), s("herdr-1", f.herdr, 4),
		s("other-1", f.other, 5), s("other-2", f.other, 6),
	}
}

func folderModel(t *testing.T, cfg config.TUI, f folderFixture, w, h int) Model {
	t.Helper()
	m := New(f.sessions(), &fakePreview{}, cfg)
	m.now = func() time.Time { return now }
	return update(t, m, tea.WindowSizeMsg{Width: w, Height: h})
}

func visibleIDs(m Model) string {
	var ids []string
	for _, i := range m.visible {
		ids = append(ids, m.rows[i].s.ID)
	}
	return strings.Join(ids, ",")
}

func TestFoldersGroupARepositoryWithItsWorktrees(t *testing.T) {
	f := newFolderFixture(t)
	m := folderModel(t, config.Default().TUI, f, 140, 40)
	if len(m.folders) != 2 {
		t.Fatalf("want the repository and the other folder, got %+v", m.folders)
	}
	app, other := m.folders[0], m.folders[1]
	if app.count != 4 || app.worktrees != 2 || !strings.HasSuffix(app.name, "src/me/app") {
		t.Errorf("repository folder %+v: want the checkout, its subdirectory, the worktree and the removed herdr worktree", app)
	}
	if other.count != 2 || !strings.HasSuffix(other.name, "notes") {
		t.Errorf("other folder %+v", other)
	}
}

func TestHerdrWorktreeWithoutCheckoutKeepsItsName(t *testing.T) {
	gone := filepath.Join(t.TempDir(), ".herdr", "worktrees", "lost", "worktree-x")
	m := New([]db.Session{{ID: "x", ProjectPath: gone, EndedAt: now}}, &fakePreview{}, config.Default().TUI)
	if len(m.folders) != 1 || m.folders[0].name != "lost" {
		t.Fatalf("got %+v", m.folders)
	}
}

func TestStartInNarrowsToTheFolder(t *testing.T) {
	f := newFolderFixture(t)
	m := folderModel(t, config.Default().TUI, f, 140, 40).StartIn(f.sub)
	if got := visibleIDs(m); got != "repo-1,sub-1,wt-1,herdr-1" {
		t.Fatalf("started in a subdirectory, the list shows %s", got)
	}
	s := screen(m)
	if !strings.Contains(s, "4 / 6 sessions · in ") || !strings.Contains(s, "Worktree") || !strings.Contains(s, "⌥ feat") {
		t.Fatalf("the header and Worktree column should show the folder:\n%s", s)
	}
	// From a worktree, the same folder.
	if got := visibleIDs(folderModel(t, config.Default().TUI, f, 140, 40).StartIn(f.wt)); got != "repo-1,sub-1,wt-1,herdr-1" {
		t.Fatalf("started in the worktree, the list shows %s", got)
	}
	// A folder without sessions, and scope = "all", show everything.
	if got := visibleIDs(folderModel(t, config.Default().TUI, f, 140, 40).StartIn(t.TempDir())); got != "repo-1,sub-1,wt-1,herdr-1,other-1,other-2" {
		t.Fatalf("started elsewhere, the list shows %s", got)
	}
	cfg := config.Default().TUI
	cfg.Scope = config.ScopeAll
	if got := visibleIDs(folderModel(t, cfg, f, 140, 40).StartIn(f.repo)); got != "repo-1,sub-1,wt-1,herdr-1,other-1,other-2" {
		t.Fatalf("with scope all, the list shows %s", got)
	}
}

func TestDotTogglesThisFolder(t *testing.T) {
	f := newFolderFixture(t)
	m := folderModel(t, config.Default().TUI, f, 140, 40).StartIn(f.other)
	if got := visibleIDs(m); got != "other-1,other-2" {
		t.Fatalf("got %s", got)
	}
	m = press(t, m, ".")
	if len(m.visible) != 6 || strings.Contains(screen(m), "Worktree") {
		t.Fatalf(". should show every folder:\n%s", screen(m))
	}
	if m = press(t, m, "."); visibleIDs(m) != "other-1,other-2" {
		t.Fatalf(". again should narrow to the folder, got %s", visibleIDs(m))
	}
	// Started where there are no sessions, . says so.
	m = press(t, folderModel(t, config.Default().TUI, f, 140, 40).StartIn(t.TempDir()), ".")
	if len(m.visible) != 6 || !strings.Contains(screen(m), "No sessions were started in this folder") {
		t.Fatalf("got %s:\n%s", visibleIDs(m), screen(m))
	}
}

func TestSidebarPicksAFolder(t *testing.T) {
	f := newFolderFixture(t)
	m := folderModel(t, config.Default().TUI, f, 140, 40).StartIn(f.repo)
	m = update(t, update(t, m, tea.KeyPressMsg{Code: tea.KeyLeft}), tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.focus != focusFolders || !m.sidebarShown() {
		t.Fatal("← ← should open the folder list and focus it")
	}
	s := screen(m)
	for _, want := range []string{"Folders 2", "All", "notes"} {
		if !strings.Contains(s, want) {
			t.Errorf("sidebar lacks %q:\n%s", want, s)
		}
	}
	m = press(t, m, "down")
	if got := visibleIDs(m); got != "other-1,other-2" {
		t.Fatalf("moving down should narrow to the next folder, got %s", got)
	}
	m = press(t, m, "k", "k")
	if len(m.visible) != 6 {
		t.Fatalf("All should show every session, got %s", visibleIDs(m))
	}
	m = press(t, m, "enter")
	if m.focus != focusList {
		t.Fatal("enter should return to the list")
	}
	m = update(t, m, tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.focus != focusFolders {
		t.Fatal("left should move to the folder list")
	}
	// The focus cycles through the folder list, the list and the frames.
	m = press(t, m, "]")
	if m.focus != focusList {
		t.Fatalf("] from the folders: %v", m.focus)
	}
	m = press(t, m, "[")
	if m.focus != focusFolders {
		t.Fatalf("[ from the list: %v", m.focus)
	}
	// A click picks a folder: the third entry is "notes".
	m = update(t, m, tea.MouseClickMsg{X: 3, Y: m.listTop() + 2, Button: tea.MouseLeft})
	if got := visibleIDs(m); got != "other-1,other-2" {
		t.Fatalf("clicking notes, the list shows %s", got)
	}
	// A click on a session row still selects it, past the sidebar.
	m = update(t, m, tea.MouseClickMsg{X: sidebarWidth + 10, Y: m.listTop() + 1, Button: tea.MouseLeft})
	if m.cursor != 1 || m.focus != focusList {
		t.Fatalf("row click: cursor %d focus %v", m.cursor, m.focus)
	}
	if m = update(t, m, tea.KeyPressMsg{Code: tea.KeyRight}); m.sidebarShown() || m.listLeft() != 0 {
		t.Fatal("→ should close the folder list")
	}
}

func TestSidebarNeedsRoom(t *testing.T) {
	f := newFolderFixture(t)
	m := update(t, folderModel(t, config.Default().TUI, f, 90, 30), tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.sidebarShown() || !strings.Contains(screen(m), "needs 100 columns") {
		t.Fatalf("a narrow terminal should not open the folder list:\n%s", screen(m))
	}
	cfg := config.Default().TUI
	cfg.DetailPosition = config.DetailRight
	if m := update(t, folderModel(t, cfg, f, 160, 30), tea.KeyPressMsg{Code: tea.KeyLeft}); m.sidebarShown() {
		t.Fatal("the folder list should not open beside a detail pane on the right")
	}
}

func TestSidebarIsRemembered(t *testing.T) {
	f := newFolderFixture(t)
	path := filepath.Join(t.TempDir(), "state.json")
	m := folderModel(t, config.Default().TUI, f, 140, 40).RememberIn(path)
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m = next.(Model)
	if cmd != nil {
		cmd()
	}
	if !config.LoadState(path).Sidebar {
		t.Fatal("the open folder list should be saved")
	}
	again := folderModel(t, config.Default().TUI, f, 140, 40).RememberIn(path)
	if !again.sidebarShown() {
		t.Fatal("the folder list should open again")
	}
}

func TestSidebarFitsTheTerminal(t *testing.T) {
	f := newFolderFixture(t)
	for _, w := range []int{100, 120, 200} {
		m := update(t, folderModel(t, config.Default().TUI, f, w, 24).StartIn(f.repo), tea.KeyPressMsg{Code: tea.KeyLeft})
		lines := strings.Split(m.render(), "\n")
		if len(lines) > 24 {
			t.Errorf("%d: %d lines", w, len(lines))
		}
		for i, l := range lines {
			if got := ansi.StringWidth(l); got > w {
				t.Errorf("%d: line %d is %d wide: %q", w, i, got, ansi.Strip(l))
			}
		}
	}
}

func TestArrowsOpenEnterLeaveAndCloseTheSidebar(t *testing.T) {
	f := newFolderFixture(t)
	m := folderModel(t, config.Default().TUI, f, 140, 40)
	left, right := tea.KeyPressMsg{Code: tea.KeyLeft}, tea.KeyPressMsg{Code: tea.KeyRight}
	if m = update(t, m, left); !m.sidebarShown() || m.focus != focusList {
		t.Fatalf("first ← opens the folder list and stays on the sessions: shown %v focus %v", m.sidebarShown(), m.focus)
	}
	if m = update(t, m, left); m.focus != focusFolders {
		t.Fatalf("second ← moves into the folder list: %v", m.focus)
	}
	if m = update(t, m, right); !m.sidebarShown() || m.focus != focusList {
		t.Fatalf("→ in the folder list returns to the sessions: shown %v focus %v", m.sidebarShown(), m.focus)
	}
	if m = update(t, m, right); m.sidebarShown() || m.focus != focusList {
		t.Fatalf("→ on the sessions closes the folder list: shown %v focus %v", m.sidebarShown(), m.focus)
	}
	// From a frame of the detail pane the arrows do nothing.
	m = press(t, m, "]")
	if m = update(t, m, left); m.sidebarShown() {
		t.Fatal("← with a frame focused should not open the folder list")
	}
	// Too narrow, ← says why.
	m = update(t, folderModel(t, config.Default().TUI, f, 90, 30), left)
	if m.sidebarShown() || !strings.Contains(screen(m), "needs 100 columns") {
		t.Fatalf("narrow:\n%s", screen(m))
	}
}

func TestFocusedSideStandsOut(t *testing.T) {
	f := newFolderFixture(t)
	left := tea.KeyPressMsg{Code: tea.KeyLeft}
	m := update(t, update(t, folderModel(t, config.Default().TUI, f, 140, 40), left), left)
	line := func(m Model, i int) string { return strings.Split(m.render(), "\n")[i] }
	accent := func(m Model, w int) string { return m.st.id.Render(strings.Repeat("╌", w)) }
	selBar := func(m Model) bool { return strings.Contains(ansi.Strip(line(m, m.listTop())), "│▎") }

	// The folder list focused: its rule takes the accent, the sessions' does
	// not, and only it shows the selection bar.
	if r := line(m, 1); !strings.Contains(r, accent(m, sidebarWidth)) || strings.Contains(r, accent(m, m.listWidth())) {
		t.Errorf("folder list focused, rule line %q", r)
	}
	if selBar(m) {
		t.Error("the sessions should not show the selection bar")
	}
	// The sessions focused: the other way round.
	m = update(t, m, tea.KeyPressMsg{Code: tea.KeyRight})
	if r := line(m, 1); strings.Contains(r, accent(m, sidebarWidth)) || !strings.Contains(r, accent(m, m.listWidth())) {
		t.Errorf("sessions focused, rule line %q", r)
	}
	if !selBar(m) {
		t.Error("the sessions should show the selection bar")
	}
}
