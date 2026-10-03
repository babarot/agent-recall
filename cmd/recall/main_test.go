package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/babarot/claude-recall/internal/db"
	"github.com/babarot/claude-recall/internal/version"
)

// emptyDB creates an archive with the schema and no sessions.
func emptyDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vault.db")
	d, err := db.Open(path, db.Options{})
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
	return path
}

func runArgs(args ...string) (string, error) {
	var out bytes.Buffer
	err := run(args, &out, &out)
	return out.String(), err
}

func TestVersion(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}} {
		out, err := runArgs(args...)
		if err != nil || out != "recall "+version.Version+"\n" {
			t.Errorf("%v: got %q, %v", args, out, err)
		}
	}
}

// A command's flags default as that command says, not as another command
// that defines a flag of the same name does.
func TestFlagDefaultsPerCommand(t *testing.T) {
	path := emptyDB(t)
	for _, args := range [][]string{
		{"search", "anything"},
		{"list"},
		{"stats"},
		{"export", "a1b2"},
	} {
		if _, err := runArgs(append(args, "--db", path)...); err != nil && !strings.Contains(err.Error(), "exit 1") {
			t.Errorf("%v: %v", args, err)
		}
	}
	out, err := runArgs("list", "--db", path)
	if err != nil || out != "No sessions found.\n" {
		t.Errorf("list: got %q, %v", out, err)
	}
}

// Flags may come after the positional arguments.
func TestFlagsAfterArgs(t *testing.T) {
	path := emptyDB(t)
	out, err := runArgs("search", "terraform", "module", "--limit", "5", "--db", path)
	if err != nil || out != "No results found.\n" {
		t.Errorf("got %q, %v", out, err)
	}
}

// Flags, arguments and values a command does not use are errors, not
// ignored.
func TestRejectsWhatACommandDoesNotTake(t *testing.T) {
	path := emptyDB(t)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"list", "--from", "2026-01-01"}, "unknown flag: --from"},
		{[]string{"tui", "--project", "x"}, "unknown flag: --project"},
		{[]string{"search", "x", "--output", "f"}, "unknown flag: --output"},
		{[]string{"ui", "statsu"}, `unknown command "statsu"`},
		{[]string{"foo"}, `unknown command "foo"`},
		{[]string{"list", "extra"}, "unknown command"},
		{[]string{"search", "x", "--format", "markdown"}, `--format must be text, json, got "markdown"`},
		{[]string{"export", "a1", "--format", "yaml"}, `--format must be markdown, json, text, got "yaml"`},
		{[]string{"search"}, "Usage: recall search <query>"},
		{[]string{"export"}, "Usage: recall export <session-id>"},
	} {
		_, err := runArgs(append(tc.args, "--db", path)...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: got %v, want %q", tc.args, err, tc.want)
		}
	}
}
