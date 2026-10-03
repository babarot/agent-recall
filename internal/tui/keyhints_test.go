package tui

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/babarot/claude-recall/internal/config"
)

var updateHints = flag.Bool("update", false, "rewrite testdata/keyhints.golden")

// TestKeyHints pins every place that names keys, as rendered: the footer
// in each context, the ? key list, and the hints on the modals' top
// edges. A change to a key or its wording shows up here.
func TestKeyHints(t *testing.T) {
	var b strings.Builder
	add := func(name, s string) {
		b.WriteString("== " + name + "\n" + s + "\n")
	}
	footer := func(name string, m Model) { add("footer: "+name, m.renderHelp()) }
	topLine := func(name string, m Model, has string) {
		for _, l := range strings.Split(m.render(), "\n") {
			if strings.Contains(ansi.Strip(l), has) {
				add(name, l)
				return
			}
		}
		t.Errorf("%s: no line with %q:\n%s", name, has, screen(m))
	}

	base := func() Model { m, _ := newTestModel(t, config.Default().TUI, 140, 40); return m }
	m := base()
	footer("list", m)
	footer("conversation frame", press(t, m, "tab"))
	footer("details frame", press(t, m, "tab", "tab"))
	footer("what was done frame", press(t, m, "tab", "tab", "tab"))
	footer("reading", press(t, m, "space"))
	footer("reading, list focused", press(t, m, "space", "tab"))
	footer("conversation search typing", press(t, m, "space", "/"))
	footer("conversation search kept", press(t, typeText(t, press(t, m, "space", "/"), "older"), "enter"))
	footer("filter", press(t, m, "/"))
	footer("filter, key hint", typeText(t, press(t, m, "/"), "bra"))
	footer("filter, key term", typeText(t, press(t, m, "/"), "title:"))
	for _, st := range []askStage{askTyping, askRunning, askAnswered, askFailed} {
		a := base()
		a.ask.stage = st
		footer("ask "+[]string{"closed", "typing", "running", "answered", "failed"}[st], a)
	}

	f := newFolderFixture(t)
	fm := folderModel(t, config.Default().TUI, f, 140, 40)
	footer("list, started in a folder", fm.StartIn(f.repo))
	footer("list, folder list open", press(t, fm, "left"))
	footer("folder list", press(t, fm, "left", "left"))
	footer("folder search typing", press(t, fm, "left", "left", "/"))
	footer("folder search kept", press(t, typeText(t, press(t, fm, "left", "left", "/"), "app"), "enter"))
	footer("filter, suggestions", typeText(t, press(t, fm, "/"), "folder:"))

	add("? key list", strings.Join(m.helpBox(100, 80), "\n"))
	topLine("sort menu top edge", press(t, m, "s"), "Sort by")
	for _, st := range []askStage{askTyping, askRunning, askFailed} {
		a := base()
		a.ask.stage = st
		topLine("ask box top edge "+[]string{"closed", "typing", "running", "answered", "failed"}[st], a, "Ask Claude")
	}

	got := b.String()
	path := filepath.Join("testdata", "keyhints.golden")
	if *updateHints {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/tui -run TestKeyHints -update)", err)
	}
	if got != string(want) {
		gl, wl := strings.Split(got, "\n"), strings.Split(string(want), "\n")
		for i := range max(len(gl), len(wl)) {
			var g, w string
			if i < len(gl) {
				g = gl[i]
			}
			if i < len(wl) {
				w = wl[i]
			}
			if g != w {
				t.Fatalf("line %d differs:\n got  %q\n want %q", i+1, ansi.Strip(g), ansi.Strip(w))
			}
		}
	}
}
