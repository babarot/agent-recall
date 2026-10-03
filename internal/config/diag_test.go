package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const diagDoc = `[ui]
port = 70000

[keys]
read = ["enter", "shift+y"]

[keys.list]
folders_open = "o"

[web]
x = 1
`

func TestLocate(t *testing.T) {
	for _, tc := range []struct {
		p                Problem
		line, col, width int
	}{
		{Problem{Key: []string{"ui", "port"}, Index: -1}, 2, 8, 5},
		{Problem{Key: []string{"ui", "port"}, Index: -1, AtKey: true}, 2, 1, 4},
		{Problem{Key: []string{"keys", "read"}, Index: 1}, 5, 18, 9},
		{Problem{Key: []string{"keys", "read"}, Index: -1}, 5, 1, 4}, // a list has no span of its own
		{Problem{Key: []string{"keys", "list", "folders_open"}, Index: 0}, 8, 16, 3},
		{Problem{Key: []string{"web"}, Index: -1, AtKey: true}, 10, 2, 3}, // a table that is no setting: its header
		{Problem{Key: []string{"tui", "theme"}, Index: -1}, 0, 0, 0},      // not in the file
	} {
		line, col, width, ok := locate([]byte(diagDoc), tc.p)
		if line != tc.line || col != tc.col || width != tc.width || ok != (tc.line > 0) {
			t.Errorf("%v: got %d:%d+%d %v, want %d:%d+%d", tc.p.Key, line, col, width, ok, tc.line, tc.col, tc.width)
		}
	}
}

// A report shows each problem where it is, top to bottom, with the line
// and a mark under the mistake.
func TestReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte(diagDoc), 0o644)
	err := Report(path, Problems{
		{Key: []string{"keys", "read"}, Index: 1, Message: `"shift+y" is never read; write "Y"`},
		{Key: []string{"ui", "port"}, Index: -1, Message: "ui.port must be between 1 and 65535, got 70000"},
		{Key: []string{"tui", "theme"}, Index: -1, Message: "not in the file"},
	})
	want := path + `: not in the file

` + path + `:2:8: ui.port must be between 1 and 65535, got 70000
  |
2 | port = 70000
  |        ^~~~~

` + path + `:5:18: "shift+y" is never read; write "Y"
  |
5 | read = ["enter", "shift+y"]
  |                  ^~~~~~~~~`
	if err.Error() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", err, want)
	}
	var ps Problems
	if !errors.As(err, &ps) || len(ps) != 3 {
		t.Fatal("a report unwraps to its problems")
	}
}

// Load shows every mistake in the file where it is, a type mistake in the
// words of the setting.
func TestLoadReportsWhere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	for body, want := range map[string]string{
		"[ui]\nport = \"x\"\n":        `:2:8: ui.port must be a number, got a string`,
		"[tui]\nask_show_cost = 3\n": `:2:17: tui.ask_show_cost must be true or false, got a number`,
		"[tui]\nscope = \"repo\"\n":    `:2:9: tui.scope must be "folder" or "all", got "repo"`,
		"[ui]\ntheme = \"nord\"\n":     `:2:1: unknown key "ui.theme"; it belongs under [tui]`,
		"[web]\nx = 1\n":              `:1:2: unknown table [web]`,
		"[tui\n":                      `:1:5: expected ']' to close table name`,
	} {
		os.WriteFile(path, []byte(body), 0o644)
		_, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", body, err, want)
		}
	}
}
