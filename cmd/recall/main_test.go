package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/babarot/claude-recall/internal/db"
	"github.com/babarot/claude-recall/internal/tui"
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

// --all and --all=false win over tui.scope, on recall and on recall tui.
func TestTUIAll(t *testing.T) {
	writeConfig(t, "[tui]\nscope = \"all\"\n")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "all"},
		{[]string{"--all=false"}, "folder"},
		{[]string{"tui", "--all=false"}, "folder"},
	} {
		if got := tuiScope(t, tc.args...); got != tc.want {
			t.Errorf("%v: got %q, want %q", tc.args, got, tc.want)
		}
	}
	writeConfig(t, "")
	for _, args := range [][]string{{"--all"}, {"tui", "--all"}} {
		if got := tuiScope(t, args...); got != "all" {
			t.Errorf("%v: got %q, want all", args, got)
		}
	}
	if got := tuiScope(t); got != "folder" {
		t.Errorf("no flag: got %q, want folder", got)
	}
}

// tuiScope is the scope the TUI would start with for args.
func tuiScope(t *testing.T, args ...string) string {
	t.Helper()
	c, rest, err := newRootCmd().Find(args)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ParseFlags(rest); err != nil {
		t.Fatal(err)
	}
	cfg, err := tuiConfig(c)
	if err != nil {
		t.Fatal(err)
	}
	return cfg.TUI.Scope
}

// ui stop reports what the server answered.
func TestUIStop(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
	}{
		{http.StatusAccepted, "Shutdown request sent to http://localhost:%d.\n"},
		{http.StatusInternalServerError, "Unexpected response: 500\n"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/api/shutdown" {
				w.WriteHeader(tc.status)
			}
		}))
		port := srv.Listener.Addr().(*net.TCPAddr).Port
		want := tc.want
		if strings.Contains(want, "%d") {
			want = fmt.Sprintf(want, port)
		}
		if out, err := runArgs("ui", "stop", "--port", strconv.Itoa(port)); err != nil || out != want {
			t.Errorf("%d: got %q, %v", tc.status, out, err)
		}
		srv.Close()
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	port := closed.Listener.Addr().(*net.TCPAddr).Port
	closed.Close()
	if out, err := runArgs("ui", "stop", "--port", strconv.Itoa(port)); err != nil || out != "UI server is not running.\n" {
		t.Errorf("not running: got %q, %v", out, err)
	}
}

// The web UI listens on the loopback address only: its API serves
// transcripts and local images.
func TestServeUILoopbackOnly(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	path := emptyDB(t)
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- run([]string{"ui", "--foreground", "--port", "0", "--db", path}, pw, io.Discard) }()

	line, err := bufio.NewReader(pr).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	go io.Copy(io.Discard, pr)
	port := strings.TrimSpace(line[strings.LastIndex(line, ":")+1:])
	if !strings.HasPrefix(line, "recall UI: http://localhost:") {
		t.Fatalf("got %q", line)
	}

	if resp, err := http.Get("http://127.0.0.1:" + port + "/api/status"); err != nil {
		t.Fatal(err)
	} else {
		resp.Body.Close()
	}
	if ip := nonLoopbackIP(); ip != "" {
		if conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, port), time.Second); err == nil {
			conn.Close()
			t.Errorf("the UI answered on %s", ip)
		}
	}

	resp, err := http.Post("http://127.0.0.1:"+port+"/api/shutdown", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the UI did not stop")
	}
	pw.Close()
}

// nonLoopbackIP is an address of this machine other than loopback, or "".
func nonLoopbackIP() string {
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && !n.IP.IsLoopback() && n.IP.To4() != nil {
			return n.IP.String()
		}
	}
	return ""
}

// A mistake under [keys] stops the TUI with what is wrong, before it
// draws, and no other command.
func TestConfigKeysErrors(t *testing.T) {
	path := emptyDB(t)
	writeConfig(t, "[keys]\nresume = \"j\"\nread = \"shift+y\"\n")
	_, err := runArgs("tui", "--db", path)
	if err == nil || !strings.Contains(err.Error(), "config.toml") || !strings.Contains(err.Error(), `keys.read: "shift+y" is never read`) {
		t.Fatalf("got %v", err)
	}
	if out, err := runArgs("list", "--db", path); err != nil || out != "No sessions found.\n" {
		t.Errorf("list should not care: %q, %v", out, err)
	}
}

func TestContinueArgs(t *testing.T) {
	recall := []string{"/bin/recall", "mcp", "--db", "/tmp/vault.db"}
	args := continueArgs(recall, tui.Continue{SessionID: "abc-123"})
	if len(args) != 5 || args[1] != "--mcp-config" || args[3] != "--allowedTools" {
		t.Fatalf("args %q", args)
	}
	// The prompt comes before the flags that take every argument after them.
	if !strings.Contains(args[0], "recall session abc-123") || !strings.Contains(args[0], "where it left off") {
		t.Fatalf("prompt %q", args[0])
	}
	if want := `{"mcpServers":{"recall":{"args":["mcp","--db","/tmp/vault.db"],"command":"/bin/recall"}}}`; args[2] != want {
		t.Fatalf("mcp config %s, want %s", args[2], want)
	}
	if args[4] != "mcp__recall__recall_search,mcp__recall__recall_list,mcp__recall__recall_export" {
		t.Fatalf("allowed tools %q", args[4])
	}
	topic := continueArgs(recall, tui.Continue{SessionID: "abc-123", Topic: "the retry policy"})[0]
	if !strings.Contains(topic, "recall session abc-123") || !strings.Contains(topic, "about: the retry policy") {
		t.Fatalf("prompt %q", topic)
	}
}
