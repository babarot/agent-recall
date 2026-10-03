package tui

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
	// The keys keep their places; what they do changes.
	if !strings.HasPrefix(footer, " enter read · space resume") || strings.Contains(footer, "sort") {
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
	// A conflict between two operations set in the file, once the keys
	// themselves are right.
	if _, err := m.WithKeys(map[string]config.KeyList{"resume": list("j"), "down": list("j")}); err == nil ||
		!strings.Contains(err.Error(), "keys: j is both resume and down in the session list") {
		t.Fatalf("got %v", err)
	}
}

// A key set in the file takes over from the operations that have it by
// default where they meet, as if the key were set to the operation.
func TestWithKeysTakesOverDefaults(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 40)
	m, err := m.WithKeys(map[string]config.KeyList{"continue": list("enter")})
	if err != nil {
		t.Fatal(err)
	}
	if got := m.km.Session.Resume.Keys(); len(got) != 0 {
		t.Fatalf("resume keeps %q", got)
	}
	// The folder list's enter never meets continue, so back keeps it.
	if got := m.km.Folders.FoldersBack.Keys(); !slices.Contains(got, "enter") {
		t.Fatalf("folders.back is %q", got)
	}
	if r := press(t, m, "enter"); !r.cont.open || r.Result != nil {
		t.Fatalf("enter should continue: box %v result %+v", r.cont.open, r.Result)
	}
	// resume, without a key, leaves the footer and the key list.
	footer := ansi.Strip(m.renderHelp())
	if !strings.HasPrefix(footer, " enter continue · space read") || strings.Contains(footer, "resume") {
		t.Fatalf("footer %q", footer)
	}
	if strings.Contains(ansi.Strip(press(t, m, "?").render()), "resume the session") {
		t.Fatal("the key list still shows resume")
	}

	// Other keys of the default operation stay.
	m, _ = newTestModel(t, config.Default().TUI, 140, 40)
	if m, err = m.WithKeys(map[string]config.KeyList{"resume": list("j")}); err != nil {
		t.Fatal(err)
	}
	if got := m.km.Nav.Down.Keys(); !slices.Equal(got, []string{"down", "ctrl+n"}) {
		t.Fatalf("down is %q", got)
	}
	// Swapped, each key keeps its place in the footer.
	m, _ = newTestModel(t, config.Default().TUI, 200, 40)
	if m, err = m.WithKeys(map[string]config.KeyList{"continue": list("enter"), "resume": list("c")}); err != nil {
		t.Fatal(err)
	}
	if footer := ansi.Strip(m.renderHelp()); !strings.HasPrefix(footer, " enter continue · space read") ||
		!strings.Contains(footer, "← folders · c resume · tab focus") {
		t.Fatalf("footer %q", footer)
	}
	// A key none of the defaults have goes after them.
	m, _ = newTestModel(t, config.Default().TUI, 200, 40)
	if m, err = m.WithKeys(map[string]config.KeyList{"resume": list("x")}); err != nil {
		t.Fatal(err)
	}
	if footer := ansi.Strip(m.renderHelp()); !strings.HasPrefix(footer, " space read") || !strings.HasSuffix(footer, "q quit · x resume") {
		t.Fatalf("footer %q", footer)
	}
	// Setting both keeps the key on the one set, the other moving away.
	m, _ = newTestModel(t, config.Default().TUI, 140, 40)
	if m, err = m.WithKeys(map[string]config.KeyList{"continue": list("enter"), "resume": list("c")}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(m.km.Session.Resume.Keys(), []string{"c"}) || !slices.Equal(m.km.Session.Continue.Keys(), []string{"enter"}) {
		t.Fatalf("resume %q continue %q", m.km.Session.Resume.Keys(), m.km.Session.Continue.Keys())
	}
	// A fixed key is still a mistake.
	if _, err := m.WithKeys(map[string]config.KeyList{"sort": list("1")}); err == nil {
		t.Fatal("sort = \"1\" should be reported")
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
		want := "`" + name + "`"
		if pane, op, ok := strings.Cut(name, "."); ok {
			want = "`[keys." + pane + "]` `" + op + "`"
		}
		if !strings.Contains(string(doc), want) {
			t.Errorf("docs/tui.md does not list %s as %s", name, want)
		}
	}
}

// The config file written on first run lists every operation under [keys]
// with its keys, so a key is changed by editing its line: uncommented as
// they are, they give the keymap as it is.
func TestTemplateListsEveryKey(t *testing.T) {
	section := config.Template[strings.Index(config.Template, "[keys]"):]
	var b strings.Builder
	b.WriteString("[keys]\n")
	// A setting, or a pane's table.
	setting := regexp.MustCompile(`^# ([a-z_]+ = (".*"|\[.*\])|\[keys\.[a-z]+\])$`)
	for _, l := range strings.Split(section, "\n") {
		if m := setting.FindStringSubmatch(l); m != nil {
			b.WriteString(m[1] + "\n")
		}
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	def := defaultKeyMap()
	for name := range def.refs() {
		if _, ok := cfg.Keys[name]; !ok {
			t.Errorf("the template lacks keys.%s", name)
		}
	}
	got, ps := applyKeys(def, cfg.Keys)
	if len(ps) > 0 {
		t.Fatal(ps)
	}
	want := defaultKeyMap()
	for name, b := range want.byName() {
		if g := got.byName()[name]; !slices.Equal(g.Keys(), b.Keys()) {
			t.Errorf("keys.%s in the template is %q, the default is %q", name, g.Keys(), b.Keys())
		}
	}
}

// An operation written in the wrong place is pointed to where it goes.
func TestWithKeysMisplaced(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 40)
	for name, want := range map[string]string{
		"list.resume":          "resume goes directly under [keys], before [keys.list] and [keys.folders]",
		"folders_open":         "folders_open goes under [keys.list]",
		"back":                 "back goes under [keys.folders]",
		"folders.folders_open": "folders_open goes under [keys.list]",
	} {
		_, err := m.WithKeys(config.Keys{name: list("x")})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %q", name, err, want)
		}
	}
	// In its pane's table, it works.
	m, err := m.WithKeys(config.Keys{"list.folders_open": list("o"), "folders.back": list("b")})
	if err != nil {
		t.Fatal(err)
	}
	if m = press(t, m, "o"); !m.sidebarShown() {
		t.Fatal("o should open the folder list")
	}
}

// Each mistake under [keys] says where it is in the config file: a bad key
// is the element it is, a conflict the key of the operation set there, an
// operation in the wrong place its name.
func TestKeyProblemsSayWhere(t *testing.T) {
	m, _ := newTestModel(t, config.Default().TUI, 140, 40)
	_, err := m.WithKeys(config.Keys{"read": list("enter", "shift+y"), "list.resume": list("o")})
	var ps config.Problems
	if !errors.As(err, &ps) || len(ps) != 2 {
		t.Fatalf("got %v", err)
	}
	byKey := map[string]config.Problem{}
	for _, p := range ps {
		byKey[strings.Join(p.Key, ".")] = p
	}
	if p := byKey["keys.read"]; p.Index != 1 || p.AtKey {
		t.Errorf("a bad key is its element: %+v", p)
	}
	if p := byKey["keys.list.resume"]; !p.AtKey {
		t.Errorf("an operation in the wrong place is its name: %+v", p)
	}
	// A conflict is one problem however many places it is in, at a key
	// set in the file.
	_, err = m.WithKeys(config.Keys{"resume": list("x", "j"), "down": list("down", "j")})
	if !errors.As(err, &ps) || len(ps) != 1 {
		t.Fatalf("got %v", err)
	}
	if p := ps[0]; strings.Join(p.Key, ".") != "keys.down" || p.Index != 1 ||
		!strings.Contains(p.Message, "j is both resume and down in the session list and a frame or the spread conversation") {
		t.Errorf("got %+v", p)
	}
}
