package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, Default()) {
		t.Fatalf("got %+v", got)
	}
}

func TestLoadKeepsDefaultsForMissingKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[tui]\ndetail_position = \"auto\"\n"), 0o644)
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.TUI.DetailPosition != DetailAuto || got.TUI.DetailAutoWidth != 160 {
		t.Fatalf("got %+v", got)
	}
}

func TestLoadRejectsUnknownPosition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[tui]\ndetail_position = \"top\"\n"), 0o644)
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error")
	}
}

func TestLoadScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[tui]\nscope = \"all\"\n"), 0o644)
	if got, err := Load(path); err != nil || got.TUI.Scope != ScopeAll {
		t.Fatalf("got %+v, %v", got, err)
	}
	os.WriteFile(path, []byte("[tui]\nscope = \"repo\"\n"), 0o644)
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error for an unknown scope")
	}
}

func TestLoadScrollbar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	for body, ok := range map[string]bool{
		"scrollbar_thumb = \"thin\"\nscrollbar_color = \"#f5a3b5\"\n": true,
		"scrollbar_thumb = \"block\"\nscrollbar_color = \"12\"\n":     true,
		"scrollbar_thumb = \"fat\"\n":                                 false,
		"scrollbar_color = \"pink\"\n":                                false,
		"scrollbar_color = \"256\"\n":                                 false,
		"scrollbar_color = \"#fff\"\n":                                false,
	} {
		os.WriteFile(path, []byte("[tui]\n"+body), 0o644)
		if _, err := Load(path); (err == nil) != ok {
			t.Errorf("%q: got %v", body, err)
		}
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cases := map[string]string{
		"scope = \"all\"\n":        `unknown key "scope"; it belongs under [tui]`,
		"[tui]\nscop = \"all\"\n":  `unknown key "tui.scop" (known: core.db`,
		"[ui]\ntheme = \"nord\"\n": `unknown key "ui.theme"; it belongs under [tui]`,
		"db = \"/tmp/v.db\"\n":     `unknown key "db"; it belongs under [core]`,
		"[tui]\nport = 8080\n":     `unknown key "tui.port"; it belongs under [ui]`,
		"[web]\nport = 8080\n":     `unknown table [web] (known: [core], [ui], [tui], [keys])`,
	}
	for body, want := range cases {
		os.WriteFile(path, []byte(body), 0o644)
		_, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", body, err, want)
		}
	}
}

func TestTemplateLoadsAsTheDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.toml")
	if err := WriteTemplate(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || !reflect.DeepEqual(got, Default()) {
		t.Fatalf("got %+v, %v", got, err)
	}
	// It never replaces a file that is there.
	os.WriteFile(path, []byte("[tui]\nscope = \"all\"\n"), 0o644)
	if err := WriteTemplate(path); err == nil {
		t.Fatal("expected an error for an existing file")
	}
	if got, _ := Load(path); got.TUI.Scope != ScopeAll {
		t.Fatal("the existing file was replaced")
	}
	// Every setting is in the template, and the README shows the template.
	for _, k := range knownKeys() {
		section, name, _ := strings.Cut(k, ".")
		if !strings.Contains(Template, "# "+name+" = ") || !strings.Contains(Template, "["+section+"]") {
			t.Errorf("template lacks %s", k)
		}
	}
	readme, _ := os.ReadFile("../../README.md")
	if !strings.Contains(string(readme), Template) {
		t.Error("README.md should show the config template as written")
	}
}

func TestModelID(t *testing.T) {
	cases := map[string]string{
		"sonnet-5.5":        "claude-sonnet-5-5",
		"Opus-5.5":          "claude-opus-5-5",
		"haiku-4.5":         "claude-haiku-4-5",
		"fable-5.1":         "claude-fable-5-1",
		"claude-sonnet-5-5": "claude-sonnet-5-5",
		"sonnet":            "sonnet",
	}
	for in, want := range cases {
		if got := ModelID(in); got != want {
			t.Errorf("ModelID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDBPath(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	cases := map[string]string{
		"":               "/home/me/.claude/vault.db",
		"~/archive/v.db": "/home/me/archive/v.db",
		"/srv/recall.db": "/srv/recall.db",
	}
	for db, want := range cases {
		if got := (File{Core: Core{DB: db}}).DBPath(); got != want {
			t.Errorf("%q: got %q, want %q", db, got, want)
		}
	}
}

func TestLoadCoreAndUI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[core]\ndb = \"~/v.db\"\n\n[ui]\nport = 8080\n"), 0o644)
	got, err := Load(path)
	if err != nil || got.Core.DB != "~/v.db" || got.UI.Port != 8080 {
		t.Fatalf("got %+v, %v", got, err)
	}
	for body, want := range map[string]string{
		"[core]\ndb = \"vault.db\"\n": "core.db must be an absolute path or start with ~/",
		"[ui]\nport = 0\n":            "ui.port must be between 1 and 65535",
		"[ui]\nport = 70000\n":        "ui.port must be between 1 and 65535",
	} {
		os.WriteFile(path, []byte(body), 0o644)
		if _, err := LoadCore(path); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", body, err, want)
		}
	}
}

// A mistake under [tui] stops the TUI but not the other commands, while a
// file that does not parse or has an unknown key stops them all.
func TestLoadCoreLeavesTUIValuesToTheTUI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[core]\ndb = \"/srv/v.db\"\n\n[tui]\ntheme = \"nope\"\n"), 0o644)
	if got, err := LoadCore(path); err != nil || got.DBPath() != "/srv/v.db" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load should reject the theme")
	}
	for _, body := range []string{"[core\n", "[tui]\nthem = \"nord\"\n"} {
		os.WriteFile(path, []byte(body), 0o644)
		if _, err := LoadCore(path); err == nil {
			t.Errorf("%q: expected an error", body)
		}
	}
}

// Transcripts follow CLAUDE_CONFIG_DIR; the archive stays where it is.
func TestClaudeConfigDir(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if got := ProjectsDir(); got != "/home/me/.claude/projects" {
		t.Errorf("ProjectsDir: got %q", got)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "/home/me/.config/claude")
	if got := ProjectsDir(); got != "/home/me/.config/claude/projects" {
		t.Errorf("ProjectsDir: got %q", got)
	}
	if got := DefaultDBPath(); got != "/home/me/.claude/vault.db" {
		t.Errorf("DefaultDBPath: got %q", got)
	}
}

// [keys] takes a key or a list of keys per operation; a value of another
// type is kept for the TUI to report and stops no other command.
func TestLoadKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[keys]\nresume = \"space\"\nread = [\"enter\", \"o\"]\nsort = []\nask = 3\n"), 0o644)
	got, err := LoadCore(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Keys["resume"].Keys, []string{"space"}) || !reflect.DeepEqual(got.Keys["read"].Keys, []string{"enter", "o"}) {
		t.Errorf("got %+v", got.Keys)
	}
	if k := got.Keys["sort"]; k.Keys == nil || len(k.Keys) != 0 || k.Err != nil {
		t.Errorf("[] should be an empty list: %+v", k)
	}
	if got.Keys["ask"].Err == nil {
		t.Error("a number should be kept as an error")
	}
	if _, err := Load(path); err != nil {
		t.Errorf("Load leaves the keys to the TUI: %v", err)
	}
}

// A pane's table under [keys] names its operations after the pane.
func TestLoadPaneKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[keys]\nresume = \"space\"\n\n[keys.list]\nfolders_open = \"o\"\n\n[keys.folders]\nback = [\"b\"]\n"), 0o644)
	got, err := LoadCore(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Keys{"resume": {Keys: []string{"space"}}, "list.folders_open": {Keys: []string{"o"}}, "folders.back": {Keys: []string{"b"}}}
	if !reflect.DeepEqual(got.Keys, want) {
		t.Fatalf("got %+v", got.Keys)
	}
}
