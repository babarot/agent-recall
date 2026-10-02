package tui

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/babarot/claude-recall/internal/config"
)

// The list can be narrowed to one folder: a repository with its worktrees,
// or a directory outside git. It starts narrowed to the folder the TUI was
// started in (tui.scope), `.` switches between that folder and all of them,
// and the sidebar (`f`) picks any other.

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
	return m.sidebar && m.mode != modePreview && !m.detailRight() && m.width >= minSidebarWidth
}

// listLeft is the screen column where the list starts.
func (m Model) listLeft() int {
	if m.sidebarShown() {
		return sidebarWidth + 1
	}
	return 0
}

// toggleSidebar opens the sidebar, focused, or closes it.
func (m *Model) toggleSidebar() tea.Cmd {
	if m.sidebarShown() {
		return m.closeSidebar()
	}
	return m.openSidebar(true)
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

// sidebarIndex is the selected entry: 0 for All, then the folders.
func (m Model) sidebarIndex() int {
	if m.scope == "" {
		return 0
	}
	for i, f := range m.folders {
		if f.key == m.scope {
			return i + 1
		}
	}
	return 0
}

// sidebarRows is how many entries the sidebar shows at once.
func (m Model) sidebarRows() int { return max(1, m.listHeight()+tableChrome-3) }

// moveFolder selects the entry delta away and narrows the list to it.
func (m *Model) moveFolder(delta int) {
	i := max(0, min(m.sidebarIndex()+delta, len(m.folders)))
	key := ""
	if i > 0 {
		key = m.folders[i-1].key
	}
	m.setScope(key)
}

// revealFolder scrolls the sidebar to its selected entry.
func (m *Model) revealFolder() {
	i, h := m.sidebarIndex(), m.sidebarRows()
	if i < m.sideOffset {
		m.sideOffset = i
	}
	if i >= m.sideOffset+h {
		m.sideOffset = i - h + 1
	}
	m.sideOffset = max(0, min(m.sideOffset, len(m.folders)+1-h))
}

// scrollSidebar moves the sidebar's view without changing the selection.
func (m *Model) scrollSidebar(delta int) {
	m.sideOffset = max(0, min(m.sideOffset+delta, len(m.folders)+1-m.sidebarRows()))
}

// sidebarAt returns the sidebar entry under a screen cell, or -1.
func (m Model) sidebarAt(x, y int) int {
	if !m.sidebarShown() || x >= sidebarWidth {
		return -1
	}
	i := y - m.listTop() // entries line up with the session rows
	if i < 0 || i >= m.sidebarRows() || m.sideOffset+i > len(m.folders) {
		return -1
	}
	return m.sideOffset + i
}

func (m *Model) pickFolder(i int) {
	key := ""
	if i > 0 {
		key = m.folders[i-1].key
	}
	m.setScope(key)
}

// renderSidebar returns n lines, sidebarWidth cells wide: a title between
// rules, level with the column headers, then the entries.
func (m Model) renderSidebar(n int) []string {
	w := sidebarWidth
	focused := m.focus == focusFolders
	title := m.st.colHdr.Render("  Folders") + m.st.muted.Render(fmt.Sprintf(" %d", len(m.folders)))
	lines := []string{m.rule(w), title, m.rule(w)}
	sel := m.sidebarIndex()
	for i := m.sideOffset; i <= len(m.folders) && len(lines) < n; i++ {
		name, count := "All", len(m.rows)
		if i > 0 {
			name, count = m.folders[i-1].name, m.folders[i-1].count
		}
		num := fmt.Sprint(count)
		name = middleEllipsis(name, w-3-len(num)-1) // the end names the repository
		gap := strings.Repeat(" ", max(1, w-2-ansi.StringWidth(name)-len(num)-1))
		if i != sel {
			style := m.st.text
			if i == 0 {
				style = m.st.subtle
			}
			lines = append(lines, "  "+style.Render(name)+gap+m.st.muted.Render(num)+" ")
			continue
		}
		bar := m.st.selected.Render(" ")
		if focused {
			bar = m.st.bar.Render("▎")
		}
		lines = append(lines, bar+m.st.selected.Render(" ")+m.st.on(m.st.key, true).Render(name)+
			m.st.selected.Render(gap)+m.st.on(m.st.muted, true).Render(num)+m.st.selected.Render(" "))
	}
	return fit(lines, n)
}
