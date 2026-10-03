package tui

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/babarot/claude-recall/internal/config"
)

func TestCheckKey(t *testing.T) {
	for _, k := range []string{"a", "Y", "?", "+", "=", "/", "space", "enter", "tab", "shift+tab", "ctrl+d", "ctrl++",
		"alt+enter", "ctrl+shift+y", "ctrl+alt+x", "shift+up", "f5", "pgdown", "delete"} {
		if err := checkKey(k); err != nil {
			t.Errorf("%q: %v", k, err)
		}
	}
	for k, want := range map[string]string{
		"ctrl-d":       `write "ctrl+d"`,
		"Enter":        `write "enter"`,
		"shift+y":      `write "Y"`,
		"ctrl+Y":       `write "ctrl+shift+y"`,
		"alt+shift+Y":  `write "alt+shift+y"`,
		"shift+ctrl+a": `write "ctrl+shift+a"`,
		"esc":          "fixed",
		"ctrl+c":       "fixed",
		"cmd+a":        `"cmd" is not one of ctrl, alt, shift`,
		"ctrl+ctrl+a":  "each once",
		"foo":          `"foo" is not a key`,
		"":             `write space as "space"`,
		" ":            `write space as "space"`,
	} {
		err := checkKey(k)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", k, err, want)
		}
	}
}

func list(keys ...string) config.KeyList { return config.KeyList{Keys: keys} }

// [keys] swaps resume and read: the keys do it and the hints say it.
func TestWithKeys(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 40)
	m, err := m.WithKeys(map[string]config.KeyList{"resume": list("space"), "read": list("enter"), "sort": list()})
	if err != nil {
		t.Fatal(err)
	}
	footer := ansi.Strip(m.renderHelp())
	if !strings.HasPrefix(footer, " space resume · enter read") || strings.Contains(footer, "sort") {
		t.Fatalf("footer %q", footer)
	}
	if r := press(t, m, "enter"); !r.expanded {
		t.Fatal("enter should read")
	}
	if r := press(t, m, "s"); r.sortMenu {
		t.Fatal("sort = [] should leave s doing nothing")
	}
	// The rest keep their keys.
	if r := press(t, m, "?"); !r.helpOpen {
		t.Fatal("? should still open the key list")
	}
}

// Every mistake is reported, and the model is left as it was.
func TestWithKeysErrors(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 40)
	next, err := m.WithKeys(map[string]config.KeyList{
		"resum":        list("space"),
		"read":         list("ctrl-d"),
		"copy_command": list("shift+y"),
		"ask":          {Err: errors.New("must be a key or a list of keys, as strings")},
	})
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{
		"keys.resum: no such operation (known: ask, bottom,",
		`keys.read: "ctrl-d" is not a key; write "ctrl+d"`,
		`keys.copy_command: "shift+y" is never read`,
		"keys.ask: must be a key or a list of keys",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("lacks %q in:\n%v", want, err)
		}
	}
	if !press(t, next, "space").expanded {
		t.Fatal("the model should be unchanged")
	}
	// A conflict, once the keys themselves are right.
	if _, err := m.WithKeys(map[string]config.KeyList{"resume": list("j")}); err == nil ||
		!strings.Contains(err.Error(), "keys: j is both resume and down in the session list") {
		t.Fatalf("got %v", err)
	}
}

// docs/tui.md lists every operation [keys] takes.
func TestDocsListEveryOperation(t *testing.T) {
	doc, err := os.ReadFile("../../docs/tui.md")
	if err != nil {
		t.Fatal(err)
	}
	k := defaultKeyMap()
	for name := range k.refs() {
		if !strings.Contains(string(doc), "`"+name+"`") {
			t.Errorf("docs/tui.md does not list %s", name)
		}
	}
}
