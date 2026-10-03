package main

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/babarot/claude-recall/internal/db"
	"github.com/babarot/claude-recall/internal/version"
)

// TestMain keeps the tests away from the developer's own config file.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "recall-config")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// writeConfig writes the config file the commands read.
func writeConfig(t *testing.T, body string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "claude-recall"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "claude-recall", "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

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

// db under [core] is the archive unless --db says otherwise, and a missing
// file at the path given is an error rather than the default archive.
func TestConfigDB(t *testing.T) {
	path := emptyDB(t)
	writeConfig(t, "[core]\ndb = \""+path+"\"\n")
	if out, err := runArgs("list"); err != nil || out != "No sessions found.\n" {
		t.Errorf("list: got %q, %v", out, err)
	}
	missing := filepath.Join(t.TempDir(), "missing.db")
	if _, err := runArgs("list", "--db", missing); err == nil {
		t.Error("--db should win over the config file")
	}
}

// port under [ui] is where ui status looks unless --port says otherwise.
func TestConfigPort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/status" {
			fmt.Fprint(w, `{"pid":42,"port":1}`)
		}
	}))
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	writeConfig(t, fmt.Sprintf("[ui]\nport = %d\n", port))
	if out, err := runArgs("ui", "status"); err != nil || out != "UI server is running (pid: 42, port: 1).\n" {
		t.Errorf("config port: got %q, %v", out, err)
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	closedPort := closed.Listener.Addr().(*net.TCPAddr).Port
	closed.Close()
	if out, err := runArgs("ui", "status", "--port", strconv.Itoa(closedPort)); err != nil || out != "UI server is not running.\n" {
		t.Errorf("--port: got %q, %v", out, err)
	}
}

// A mistake under [tui] does not stop the other commands; one that keeps
// the file from being read does.
func TestConfigErrors(t *testing.T) {
	path := emptyDB(t)
	writeConfig(t, "[tui]\ntheme = \"nope\"\n")
	if _, err := runArgs("list", "--db", path); err != nil {
		t.Errorf("a bad [tui] value stopped list: %v", err)
	}
	writeConfig(t, "[core]\ndb = \"vault.db\"\n")
	if _, err := runArgs("list", "--db", path); err == nil || !strings.Contains(err.Error(), "core.db") {
		t.Errorf("got %v", err)
	}
}
