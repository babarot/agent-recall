package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/babarot/claude-recall/internal/config"
)

func TestDefaultKeyMapHasNoConflicts(t *testing.T) {
	if c := defaultKeyMap().conflicts(); len(c) > 0 {
		t.Fatalf("conflicts: %v", c)
	}
}

func TestConflicts(t *testing.T) {
	for _, tc := range []struct {
		edit func(*keyMap)
		want string
	}{
		{func(k *keyMap) { k.Session.Resume = keys("j") }, "j is both resume and down in the session list"},
		{func(k *keyMap) { k.Session.Continue = keys("enter") }, "enter is both resume and continue in the session list"},
		{func(k *keyMap) { k.Global.Sort = keys("1") }, "1 is both a fixed key and sort in the sort menu"},
		{func(k *keyMap) { k.Global.Help = keys("esc") }, "esc is both a fixed key and help"},
		{func(k *keyMap) { k.Folders.FoldersBack = keys("q") }, "q is both quit and folders.back in the folder list"},
	} {
		k := defaultKeyMap()
		tc.edit(&k)
		if got := fmt.Sprint(k.conflicts()); !strings.Contains(got, tc.want) {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
	// The folder list's enter and the session's enter never meet.
	if c := defaultKeyMap().conflicts(); len(c) != 0 {
		t.Fatal(c)
	}
}

func TestHintKeys(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 40)
	for tmpl, want := range map[string]string{
		"{up.0} {down.0}  {down.1} {up.1}": "↑ ↓  j k",
		"{up.0}{down.0}":                   "↑↓",
		"{grow.0}/{shrink.0}":              "+/-",
		"{focus_next.0}  {focus_prev.0}":   "tab  ⇧tab",
		"{read.0} esc":                     "space esc",
		"{help.0} esc q close":             "? esc q close",
		"↑ ↓  enter":                       "↑ ↓  enter",
		"move; {top.1} {bottom.1} top":     "move; g G top",
	} {
		if got := m.hintKeys(tmpl); got != want {
			t.Errorf("%q: got %q, want %q", tmpl, got, want)
		}
	}
	// A key a remap took away goes with its separator.
	m.km.Nav.Up = keys("up")
	m.km.Session.Shrink = keys()
	for tmpl, want := range map[string]string{
		"{up.0} {down.0}  {down.1} {up.1}": "↑ ↓  j",
		"{grow.0}/{shrink.0}":              "+",
		"{shrink.0}/{grow.0}":              "+",
		"{shrink.0}":                       "",
	} {
		if got := m.hintKeys(tmpl); got != want {
			t.Errorf("remapped %q: got %q, want %q", tmpl, got, want)
		}
	}
}

func TestKeyContext(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 40)
	for _, tc := range []struct {
		keys []string
		want keyContext
	}{
		{nil, ctxList},
		{[]string{"tab"}, ctxFrame},
		{[]string{"tab", "tab"}, ctxFrame},
		{[]string{"space"}, ctxReading},
		{[]string{"space", "tab"}, ctxList},
	} {
		if got := press(t, m, tc.keys...).keyContext(); got != tc.want {
			t.Errorf("%v: got %v, want %v", tc.keys, got, tc.want)
		}
	}
	f := newFolderFixture(t)
	if got := press(t, folderModel(t, config.Default().TUI, f, 140, 40), "left", "left").keyContext(); got != ctxFolders {
		t.Errorf("folder list: got %v", got)
	}
}

// What the keymap says is what the keys do and what the hints show:
// swapping enter and space swaps resume and read.
func TestRemappedKeys(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 40)
	m.km.Session.Resume = keys("space")
	m.km.Session.Read = keys("enter")
	if footer := ansi.Strip(m.renderHelp()); !strings.HasPrefix(footer, " space resume · enter read") {
		t.Fatalf("footer %q", footer)
	}
	if r := press(t, m, "enter"); !r.expanded || r.Result != nil {
		t.Fatalf("enter should read: expanded %v result %v", r.expanded, r.Result)
	}
	r := press(t, m, "j", "space") // the first session's folder is gone
	if r.Result == nil || r.expanded {
		t.Fatalf("space should resume: result %v expanded %v", r.Result, r.expanded)
	}

	// A box closes on the key that opens it, whichever that is.
	m, _ = newTestModel(t, config.Default().TUI, 140, 40)
	m.km.Global.Help = keys("H")
	m.km.Global.Sort = keys("o")
	m = press(t, m, "H")
	if !m.helpOpen || !strings.Contains(m.render(), "H esc q close") {
		t.Fatal("H should open the key list, and its edge should say so")
	}
	if m = press(t, m, "H"); m.helpOpen {
		t.Fatal("H should close the key list")
	}
	if m = press(t, m, "?"); m.helpOpen {
		t.Fatal("? no longer opens it")
	}
	if m = press(t, m, "o"); !m.sortMenu {
		t.Fatal("o should open the sort menu")
	}
	if m = press(t, m, "s"); !m.sortMenu {
		t.Fatal("s no longer closes it")
	}
	if m = press(t, m, "o"); m.sortMenu {
		t.Fatal("o should close the sort menu")
	}
}
