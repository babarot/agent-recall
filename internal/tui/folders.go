package tui

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/babarot/claude-recall/internal/config"
)

// The list can be narrowed to one folder: a repository with its worktrees,
// or a directory outside git. It starts narrowed to the folder the TUI was
// started in (tui.scope), `.` switches between that folder and all of them,
// and the sidebar (← to open, → to close) picks any other.

const (
	// herdrGroup marks the group of a removed herdr worktree, whose path
	// names only the repository; New replaces it with that repository's
	// checkout when the other sessions show which one it is.
	herdrGroup = "herdr:"

	sidebarWidth    = 30
	minSidebarWidth = 100 // narrowest terminal that shows the sidebar
)

// folderInfo is a folder the list can be narrowed to.
type folderInfo struct {
	key, name string
	count     int
	worktrees int
	last      int // index in rows of its latest session, for ordering
}

// groupRows resolves herdr placeholders and lists the folders, the most
// recently active first.
func groupRows(rows []row) []folderInfo {
	byBase := map[string][]string{}
	seen := map[string]bool{}
	for _, r := range rows {
		if !strings.HasPrefix(r.group, herdrGroup) && !seen[r.group] {
			seen[r.group] = true
			byBase[filepath.Base(r.group)] = append(byBase[filepath.Base(r.group)], r.group)
		}
	}
	names := map[string]string{}
	for i := range rows {
		r := &rows[i]
		names[r.group] = r.groupName
		if repo, ok := strings.CutPrefix(r.group, herdrGroup); ok {
			if keys := byBase[repo]; len(keys) == 1 {
				r.group = keys[0]
			}
		}
	}
	for i := range rows {
		rows[i].groupName = names[rows[i].group]
	}

	at := map[string]int{}
	var out []folderInfo
	wts := map[string]map[string]bool{}
	for i, r := range rows {
		j, ok := at[r.group]
		if !ok {
			j = len(out)
			at[r.group] = j
			out = append(out, folderInfo{key: r.group, name: r.groupName, last: i})
			wts[r.group] = map[string]bool{}
		}
		out[j].count++
		if r.worktree != "" {
			wts[r.group][r.worktree] = true
		}
		if rows[i].s.EndedAt.After(rows[out[j].last].s.EndedAt) {
			out[j].last = i
		}
	}
	for i := range out {
		out[i].worktrees = len(wts[out[i].key])
	}
	slices.SortFunc(out, func(a, b folderInfo) int {
		return cmp.Or(rows[b.last].s.EndedAt.Compare(rows[a.last].s.EndedAt), cmp.Compare(a.name, b.name))
	})
	return out
}

// StartIn sets the folder the TUI was started in: the repository dir is in,
// when it has sessions. With tui.scope = "folder" the list starts narrowed
// to it.
func (m Model) StartIn(dir string) Model {
	info := m.resolver.Resolve(dir)
	key := cmp.Or(realPath(info.MainRoot), info.Root, realPath(dir))
	if slices.ContainsFunc(m.folders, func(f folderInfo) bool { return f.key == key }) {
		m.startFolder = key
		if m.cfg.Scope == config.ScopeFolder {
			m.setScope(key)
		}
	}
	return m
}

// setScope narrows the list to the folder with key, or shows every folder
// for "", and starts at the top.
func (m *Model) setScope(key string) {
	m.scope = key
	m.refresh()
	m.cursor, m.offset = 0, 0
	m.clamp()
	m.revealFolder()
}

func (m Model) folderName(key string) string {
	for _, f := range m.folders {
		if f.key == key {
			return f.name
		}
	}
	return key
}

// toggleScope switches between the folder the TUI started in and all of
// them.
func (m *Model) toggleScope() tea.Cmd {
	if m.startFolder == "" {
		return m.showToast(toastInfo, "No sessions were started in this folder")
	}
	if m.scope == m.startFolder {
		m.setScope("")
	} else {
		m.setScope(m.startFolder)
	}
	return nil
}

// sidebarShown reports whether the sidebar is drawn: it is open and fits,
// which needs the detail pane below the list.
func (m Model) sidebarShown() bool {
	return m.sidebar && !m.detailRight() && m.width >= minSidebarWidth
}

// listLeft is the screen column where the list starts.
func (m Model) listLeft() int {
	if m.sidebarShown() {
		return sidebarWidth + 1
	}
	return 0
}

// openSidebar shows the sidebar, moving the focus to it when focus is set.
func (m *Model) openSidebar(focus bool) tea.Cmd {
	was := m.sidebar
	m.sidebar = true
	if !m.sidebarShown() {
		m.sidebar = was
		return m.showToast(toastInfo, fmt.Sprintf("The folder list needs %d columns and the detail pane below", minSidebarWidth))
	}
	if focus {
		m.focus = focusFolders
	}
	m.revealFolder()
	m.clamp()
	return m.saveState()
}

func (m *Model) closeSidebar() tea.Cmd {
	m.sidebar = false
	if m.focus == focusFolders {
		m.focus = focusList
	}
	m.clamp()
	return m.saveState()
}

// sideEntry is a line of the sidebar: All (key "") or a folder, with the
// runes of its name the search matched.
type sideEntry struct {
	key, name string
	count     int
	hits      []int
}

// sideEntries are the sidebar's lines: All and every folder, or the
// folders the search matches, best first.
func (m Model) sideEntries() []sideEntry {
	q := strings.TrimSpace(m.sideSearch.Value())
	if q == "" {
		return append([]sideEntry{{name: "All", count: len(m.rows)}}, m.rankFolders("")...)
	}
	return m.rankFolders(q)
}

// rankFolders lists the folders whose name fuzzy-matches q, best first and,
// among equals, the most recent; every folder, most recent first, for "".
func (m Model) rankFolders(q string) []sideEntry {
	type scored struct {
		sideEntry
		score, order int
	}
	var found []scored
	for i, f := range m.folders {
		if score, hits, ok := fuzzyMatch(q, f.name); ok {
			found = append(found, scored{sideEntry{f.key, f.name, f.count, hits}, score, i})
		}
	}
	// Best match first; among equals, the most recent folder.
	slices.SortStableFunc(found, func(a, b scored) int { return cmp.Or(cmp.Compare(b.score, a.score), cmp.Compare(a.order, b.order)) })
	out := make([]sideEntry, len(found))
	for i, f := range found {
		out[i] = f.sideEntry
	}
	return out
}

// sidebarIndex is the selected entry, or -1 when the search hides it.
func (m Model) sidebarIndex() int {
	return slices.IndexFunc(m.sideEntries(), func(e sideEntry) bool { return e.key == m.scope })
}

// sidebarRows is how many entries the sidebar shows at once.
func (m Model) sidebarRows() int { return max(1, m.listHeight()+tableChrome-3) }

// moveFolder selects the entry delta away and narrows the list to it.
func (m *Model) moveFolder(delta int) {
	es := m.sideEntries()
	if len(es) == 0 {
		return
	}
	i := m.sidebarIndex()
	if i < 0 { // the search hides it: start from the top
		i = 0
		if delta > 0 {
			i = -1
		}
	}
	m.setScope(es[max(0, min(i+delta, len(es)-1))].key)
}

// revealFolder scrolls the sidebar to its selected entry.
func (m *Model) revealFolder() {
	i, h, n := max(0, m.sidebarIndex()), m.sidebarRows(), len(m.sideEntries())
	if i < m.sideOffset {
		m.sideOffset = i
	}
	if i >= m.sideOffset+h {
		m.sideOffset = i - h + 1
	}
	m.sideOffset = max(0, min(m.sideOffset, n-h))
}

// scrollSidebar moves the sidebar's view without changing the selection.
func (m *Model) scrollSidebar(delta int) {
	m.sideOffset = max(0, min(m.sideOffset+delta, len(m.sideEntries())-m.sidebarRows()))
}

// sidebarAt returns the sidebar entry under a screen cell, or -1.
func (m Model) sidebarAt(x, y int) int {
	if !m.sidebarShown() || x >= sidebarWidth {
		return -1
	}
	i := y - m.listTop() // entries line up with the session rows
	if i < 0 || i >= m.sidebarRows() || m.sideOffset+i >= len(m.sideEntries()) {
		return -1
	}
	return m.sideOffset + i
}

func (m *Model) pickFolder(i int) {
	if es := m.sideEntries(); i >= 0 && i < len(es) {
		m.setScope(es[i].key)
	}
}

// searchFolders starts typing a search in the sidebar.
func (m *Model) searchFolders() tea.Cmd {
	m.sideTyping = true
	return m.sideSearch.Focus()
}

// updateSideSearch handles a key while the sidebar search is typed: the
// arrows pick among the matches, Enter keeps the search, Esc drops it.
func (m Model) updateSideSearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "down", "ctrl+n":
		m.moveFolder(1)
		return m, nil
	case "up", "ctrl+p":
		m.moveFolder(-1)
		return m, nil
	case "enter":
		m.sideTyping = false
		m.sideSearch.Blur()
		return m, nil
	case "esc":
		m.clearSideSearch()
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	}
	before := m.sideSearch.Value()
	var cmd tea.Cmd
	m.sideSearch, cmd = m.sideSearch.Update(msg)
	if m.sideSearch.Value() != before {
		// The best match is selected, and the list follows it.
		m.sideOffset = 0
		if es := m.sideEntries(); len(es) > 0 {
			m.setScope(es[0].key)
		}
	}
	return m, cmd
}

// clearSideSearch drops the search and shows every folder again.
func (m *Model) clearSideSearch() {
	m.sideTyping = false
	m.sideSearch.Blur()
	m.sideSearch.SetValue("")
	m.revealFolder()
}

// renderSidebar returns n lines, sidebarWidth cells wide: a title (or the
// search) between rules, level with the column headers, then the entries.
func (m Model) renderSidebar(n int) []string {
	w := sidebarWidth
	focused := m.focus == focusFolders
	// Focused, its rules and title take the accent, as a focused frame's
	// border does.
	rule, name := m.st.rule, m.st.colHdr
	if focused {
		rule, name = m.st.id, m.st.key
	}
	es := m.sideEntries()
	title := name.Render("  Folders") + m.st.muted.Render(fmt.Sprintf(" %d", len(m.folders)))
	if m.sideTyping || m.sideSearch.Value() != "" {
		count := m.st.muted.Render(fmt.Sprintf(" %d/%d", len(es), len(m.folders)))
		box := m.sideSearch.View()
		if !m.sideTyping {
			box = m.st.filter.Render("/ " + m.sideSearch.Value())
		}
		title = " " + ansi.Truncate(box, w-1-ansi.StringWidth(count), ellipsis)
		title += strings.Repeat(" ", max(0, w-ansi.StringWidth(title)-ansi.StringWidth(count))) + count
	}
	lines := []string{rule.Render(strings.Repeat("╌", w)), title, rule.Render(strings.Repeat("╌", w))}
	sel := m.sidebarIndex()
	if len(es) == 0 {
		lines = append(lines, m.st.muted.Render("  No folder matches"))
	}
	for i := m.sideOffset; i < len(es) && len(lines) < n; i++ {
		e := es[i]
		num := fmt.Sprint(e.count)
		label := middleEllipsis(e.name, w-3-len(num)-1) // the end names the repository
		gap := strings.Repeat(" ", max(1, w-2-ansi.StringWidth(label)-len(num)-1))
		if i != sel {
			style := m.st.text
			if e.key == "" {
				style = m.st.subtle
			}
			lines = append(lines, "  "+m.highlight(e.name, label, e.hits, style, m.st.filter.Bold(true))+gap+m.st.muted.Render(num)+" ")
			continue
		}
		bar := m.st.selected.Render(" ")
		if focused {
			bar = m.st.bar.Render("▎")
		}
		lines = append(lines, bar+m.st.selected.Render(" ")+m.highlight(e.name, label, e.hits, m.st.on(m.st.key, true), m.st.on(m.st.filter.Bold(true), true))+
			m.st.selected.Render(gap)+m.st.on(m.st.muted, true).Render(num)+m.st.selected.Render(" "))
	}
	return fit(lines, n)
}

// highlight renders label, a shortened name, with the runes of name at hits
// in the hit style. Shortening keeps the start and the end of the name with
// an ellipsis between, so hits map to either side of it.
func (m Model) highlight(name, label string, hits []int, base, hit lipgloss.Style) string {
	if len(hits) == 0 {
		return base.Render(label)
	}
	n, l := []rune(name), []rune(label)
	at := map[int]bool{}
	if string(n) == label {
		for _, h := range hits {
			at[h] = true
		}
	} else if cut := slices.Index(l, []rune(ellipsis)[0]); cut >= 0 {
		tail := len(l) - cut - 1 // runes after the ellipsis, the end of name
		for _, h := range hits {
			switch {
			case h < cut && n[h] == l[h]:
				at[h] = true
			case h >= len(n)-tail:
				at[h-(len(n)-tail)+cut+1] = true
			}
		}
	}
	var b strings.Builder
	for i, c := range l {
		if at[i] {
			b.WriteString(hit.Render(string(c)))
		} else {
			b.WriteString(base.Render(string(c)))
		}
	}
	return b.String()
}
