package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/babarot/claude-recall/internal/fixture"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// golden compares got with testdata/<name>.golden, or writes it with -update.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
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
		t.Fatalf("%v (run go test ./cmd/recall -update to write it)", err)
	}
	if got != string(want) {
		t.Errorf("%s differs from %s:\n%s", name, path, got)
	}
}

// dbSize is the one line of stats that depends on how SQLite lays out pages.
var dbSize = regexp.MustCompile(`Database size:   .*`)

// What search, list, stats and export print stays as it is: other tools and
// scripts read it, and it matched the TypeScript version byte for byte.
func TestOutput(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	path := fixture.Archive(t)
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"search", []string{"search", "terraform"}},
		{"search-json", []string{"search", "terraform", "--format", "json"}},
		{"search-limit", []string{"search", "terraform", "--limit", "1"}},
		{"search-from", []string{"search", "the", "--from", "2026-04-01"}},
		{"search-to", []string{"search", "the", "--to", "2026-03-31"}},
		{"search-project", []string{"search", "the", "--project", "api"}},
		{"search-none", []string{"search", "kubernetes"}},
		{"list", []string{"list"}},
		{"list-json", []string{"list", "--format", "json"}},
		{"list-limit", []string{"list", "--limit", "1"}},
		{"list-project", []string{"list", "--project", "app"}},
		{"stats", []string{"stats"}},
		{"stats-project", []string{"stats", "--project", "api"}},
		{"export", []string{"export", fixture.APISession}},
		{"export-json", []string{"export", fixture.APISession, "--format", "json"}},
		{"export-text", []string{"export", fixture.APISession, "--format", "text"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runArgs(append(tc.args, "--db", path)...)
			if err != nil {
				t.Fatal(err)
			}
			golden(t, tc.name, dbSize.ReplaceAllString(out, "Database size:   <size>"))
		})
	}
}

func TestOutputWithoutColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	path := fixture.Archive(t)
	for _, args := range [][]string{{"search", "terraform"}, {"list"}, {"stats"}} {
		out, err := runArgs(append(args, "--db", path)...)
		if err != nil || strings.Contains(out, "\x1b[") {
			t.Errorf("%v: got %q, %v", args, out, err)
		}
	}
}

func TestExport(t *testing.T) {
	path := fixture.Archive(t)

	// An ID prefix finds the session.
	full, _ := runArgs("export", fixture.APISession, "--db", path)
	if out, err := runArgs("export", fixture.APISession[:4], "--db", path); err != nil || out != full {
		t.Errorf("prefix: got %q, %v", out, err)
	}

	// --output writes what would be printed, less the final newline.
	file := filepath.Join(t.TempDir(), "session.md")
	out, err := runArgs("export", fixture.APISession, "--output", file, "--db", path)
	if err != nil || out != "Exported to "+file+"\n" {
		t.Errorf("--output: got %q, %v", out, err)
	}
	if b, _ := os.ReadFile(file); string(b)+"\n" != full {
		t.Errorf("--output wrote %q", b)
	}

	// A session that is not there is reported, with exit status 1.
	out, err = runArgs("export", "ffff", "--db", path)
	var code exitError
	if !errors.As(err, &code) || code != 1 || out != "Session not found: ffff\n" {
		t.Errorf("not found: got %q, %v", out, err)
	}
}

func TestImport(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Dir(fixture.Projects(t)))
	path := filepath.Join(t.TempDir(), "vault.db")

	// A dry run lists what it would import and leaves no archive behind.
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "Would import 2 session files:\n  " + fixture.APISession + " (-work-api)\n  " + fixture.AppSession + " (-work-app)\n"},
		{[]string{"--session", fixture.AppSession[:6]}, "Would import 1 session files:\n  " + fixture.AppSession + " (-work-app)\n"},
		{[]string{"--project", "API"}, "Would import 1 session files:\n  " + fixture.APISession + " (-work-api)\n"},
		{[]string{"--project", "nope"}, "No sessions found to import.\n"},
	} {
		args := append([]string{"import", "-n", "--db", path}, tc.args...)
		if out, err := runArgs(args...); err != nil || out != tc.want {
			t.Errorf("%v: got %q, %v", tc.args, out, err)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a dry run created the archive: %v", err)
	}

	out, err := runArgs("import", "--session", fixture.AppSession[:6], "--db", path)
	if err != nil || out != "Syncing 1 sessions...\nImported 1 sessions (4 messages).\n" {
		t.Errorf("import: got %q, %v", out, err)
	}
	out, err = runArgs("import", "--db", path)
	if err != nil || out != "Syncing 2 sessions...\nImported 1 sessions (3 messages). 1 unchanged.\n" {
		t.Errorf("import again: got %q, %v", out, err)
	}
}

// A --limit given, even 0, is passed on; without one the command's default
// applies.
func TestLimitGiven(t *testing.T) {
	path := fixture.Archive(t)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"search", "terraform", "--limit", "0"}, "No results found.\n"},
		{[]string{"list", "--limit", "0"}, "No sessions found.\n"},
	} {
		if out, err := runArgs(append(tc.args, "--db", path)...); err != nil || out != tc.want {
			t.Errorf("%v: got %q, %v", tc.args, out, err)
		}
	}
	if _, err := runArgs("list", "--limit", "5.5", "--db", path); err == nil {
		t.Error("--limit 5.5 should be an error")
	}
}
