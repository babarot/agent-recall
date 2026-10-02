// Command recall archives Claude Code sessions in SQLite and lets you find
// them again: a TUI to browse, a CLI to search, an MCP server for agents and
// a web UI.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/babarot/claude-recall/internal/cli"
	"github.com/babarot/claude-recall/internal/config"
	"github.com/babarot/claude-recall/internal/db"
	"github.com/babarot/claude-recall/internal/importer"
	"github.com/babarot/claude-recall/internal/mcp"
	"github.com/babarot/claude-recall/internal/tui"
	"github.com/babarot/claude-recall/internal/version"
	"github.com/babarot/claude-recall/internal/watcher"
	"github.com/babarot/claude-recall/internal/web"
)

const usage = `recall - Archive and search coding agent sessions

Usage:
  recall [tui]                  Browse sessions interactively
  recall import [options]       Import sessions into the vault
  recall search <query> [opts]  Full-text search across sessions
  recall list [options]         List archived sessions
  recall export <id> [options]  Export a session
  recall stats [options]        Show archive statistics
  recall mcp                    Start MCP server (stdio transport)
  recall ui [--port <n>]        Start web UI in background (default: 6276)
  recall ui --foreground        Start web UI in foreground
  recall ui stop                Stop running UI server
  recall ui status              Show UI server status
  recall version                Show the version

Global Options:
  --db <path>     Database path (default: ~/.claude/vault.db)
  --help          Show this help

Import Options:
  --session <id>  Import a specific session
  --project <name> Import sessions for a project
  --dry-run       Show what would be imported

Search Options:
  --project <name> Limit to a project
  --limit <n>     Max results (default: 20)
  --from <date>   Start date (YYYY-MM-DD)
  --to <date>     End date (YYYY-MM-DD)
  --format <fmt>  Output format: text, json (default: text)

List Options:
  --project <name> Filter by project
  --limit <n>     Max sessions (default: 50)
  --format <fmt>  Output format: text, json (default: text)

Export Options:
  --format <fmt>  Output format: markdown, json, text (default: markdown)
  --output <file> Write to file instead of stdout
`

// exitError carries an exit status for errors already reported to stderr.
type exitError int

func (e exitError) Error() string { return "exit " + strconv.Itoa(int(e)) }

func main() {
	err := run(os.Args[1:], os.Stdout, os.Stderr)
	var code exitError
	switch {
	case errors.As(err, &code):
		os.Exit(int(code))
	case err != nil:
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// options are the flags every subcommand accepts. They may appear anywhere
// on the command line.
type options struct {
	db, session, project, format, from, to, output, port, limit string
	help, dryRun, foreground, version                           bool
	limitSet, portSet                                           bool
	positional                                                  []string
}

func parse(args []string) (*options, error) {
	o := &options{}
	fs := flag.NewFlagSet("recall", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.db, "db", config.DefaultDBPath(), "")
	fs.StringVar(&o.session, "session", "", "")
	fs.StringVar(&o.project, "project", "", "")
	fs.StringVar(&o.format, "format", "", "")
	fs.StringVar(&o.from, "from", "", "")
	fs.StringVar(&o.to, "to", "", "")
	fs.StringVar(&o.output, "output", "", "")
	fs.StringVar(&o.port, "port", "", "")
	fs.StringVar(&o.limit, "limit", "", "")
	fs.BoolVar(&o.help, "help", false, "")
	fs.BoolVar(&o.help, "h", false, "")
	fs.BoolVar(&o.dryRun, "dry-run", false, "")
	fs.BoolVar(&o.dryRun, "n", false, "")
	fs.BoolVar(&o.foreground, "foreground", false, "")
	fs.BoolVar(&o.version, "version", false, "")
	// Flags may appear before, between or after positional arguments.
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			break
		}
		o.positional = append(o.positional, args[0])
		args = args[1:]
	}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "limit":
			o.limitSet = true
		case "port":
			o.portSet = true
		}
	})
	return o, nil
}

// limitArg is `args.limit ? Number(args.limit) : undefined`.
func (o *options) limitArg() (*int, error) {
	if !o.limitSet || o.limit == "" {
		return nil, nil
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(o.limit), 64)
	if err != nil {
		return nil, fmt.Errorf("--limit: not a number: %s", o.limit)
	}
	n := int(f)
	return &n, nil
}

func (o *options) portArg() (int, error) {
	if !o.portSet || o.port == "" {
		return web.DefaultPort, nil
	}
	return strconv.Atoi(o.port)
}

func run(args []string, stdout, stderr io.Writer) error {
	o, err := parse(args)
	if err != nil {
		return err
	}
	if o.version || (len(o.positional) == 1 && o.positional[0] == "version") {
		fmt.Fprintf(stdout, "recall %s\n", version.Version)
		return nil
	}
	if o.help {
		fmt.Fprint(stdout, usage+"\n") // console.log(USAGE)
		return nil
	}
	if len(o.positional) == 0 {
		return runTUI(o)
	}
	switch sub := o.positional[0]; sub {
	case "tui":
		return runTUI(o)
	case "import":
		return runImport(o, stdout)
	case "search":
		return runSearch(o, stdout, stderr)
	case "list":
		return runList(o, stdout)
	case "export":
		return runExport(o, stdout, stderr)
	case "stats":
		return runStats(o, stdout)
	case "mcp":
		return runMCP(o)
	case "ui":
		return runUI(o, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "Unknown command: %s\n", sub)
		fmt.Fprint(stdout, usage+"\n")
		return exitError(1)
	}
}

func openRead(o *options) (*db.DB, error) { return db.Open(o.db, db.Options{ReadOnly: true}) }

func openWrite(o *options) (*db.DB, error) { return db.Open(o.db, db.Options{}) }

// catchUp imports what changed while no server ran. It runs beside the
// server, which answers right away: a first import after an upgrade can
// take a while, and Claude Code gives an MCP server 30 seconds to start. A
// failed import is reported and the server keeps running; the watcher
// imports the session again when it changes.
func catchUp(d *db.DB, w io.Writer) {
	if err := importer.Run(d, importer.Options{ProjectsDir: config.ProjectsDir()}, w); err != nil {
		fmt.Fprintln(os.Stderr, "recall: import:", err)
	}
}

func runImport(o *options, stdout io.Writer) error {
	opts := importer.Options{
		ProjectsDir: config.ProjectsDir(),
		Session:     o.session,
		Project:     o.project,
		DryRun:      o.dryRun,
	}
	if o.dryRun {
		return importer.Run(nil, opts, stdout)
	}
	d, err := openWrite(o)
	if err != nil {
		return err
	}
	defer d.Close()
	return importer.Run(d, opts, stdout)
}

func runSearch(o *options, stdout, stderr io.Writer) error {
	query := strings.Join(o.positional[1:], " ")
	if query == "" {
		fmt.Fprintln(stderr, "Usage: recall search <query>")
		return exitError(1)
	}
	limit, err := o.limitArg()
	if err != nil {
		return err
	}
	d, err := openRead(o)
	if err != nil {
		return err
	}
	results, err := d.Search(query, db.SearchOptions{Project: o.project, Limit: limit, From: o.from, To: o.to})
	d.Close()
	if err != nil {
		return err
	}
	if len(results) == 0 {
		fmt.Fprintln(stdout, "No results found.")
		return nil
	}
	if o.format == "json" {
		return cli.WriteJSON(stdout, results)
	}
	cli.Search(stdout, results)
	return nil
}

func runList(o *options, stdout io.Writer) error {
	limit, err := o.limitArg()
	if err != nil {
		return err
	}
	d, err := openRead(o)
	if err != nil {
		return err
	}
	sessions, err := d.ListSessions(db.ListOptions{Project: o.project, Limit: limit})
	d.Close()
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		fmt.Fprintln(stdout, "No sessions found.")
		return nil
	}
	if o.format == "json" {
		return cli.WriteJSON(stdout, sessions)
	}
	cli.List(stdout, sessions)
	return nil
}

func runExport(o *options, stdout, stderr io.Writer) error {
	if len(o.positional) < 2 {
		fmt.Fprintln(stderr, "Usage: recall export <session-id>")
		return exitError(1)
	}
	id := o.positional[1]
	d, err := openRead(o)
	if err != nil {
		return err
	}
	s, msgs, err := d.ExportSession(id)
	d.Close()
	if err != nil {
		return err
	}
	if s == nil {
		fmt.Fprintf(stderr, "Session not found: %s\n", id)
		return exitError(1)
	}
	var out string
	switch o.format {
	case "json":
		var b strings.Builder
		if err := cli.WriteJSON(&b, cli.ExportJSON{Session: s, Messages: msgs}); err != nil {
			return err
		}
		out = strings.TrimSuffix(b.String(), "\n")
	case "text":
		out = cli.Text(s, msgs)
	default:
		out = cli.Markdown(s, msgs)
	}
	if o.output != "" {
		if err := os.WriteFile(o.output, []byte(out), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Exported to %s\n", o.output)
		return nil
	}
	fmt.Fprintln(stdout, out)
	return nil
}

func runStats(o *options, stdout io.Writer) error {
	d, err := openRead(o)
	if err != nil {
		return err
	}
	s, err := d.Stats(o.project)
	d.Close()
	if err != nil {
		return err
	}
	cli.Stats(stdout, s)
	return nil
}

func runMCP(o *options) error {
	d, err := openWrite(o)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	w := &watcher.Watcher{DB: d, ProjectsDir: config.ProjectsDir()}
	go w.Run(ctx)
	// The import summary goes to stderr: stdout carries the protocol.
	go catchUp(d, os.Stderr)
	return mcp.Run(ctx, d)
}

func runUI(o *options, stdout, stderr io.Writer) error {
	port, err := o.portArg()
	if err != nil {
		return fmt.Errorf("--port: %w", err)
	}
	action := "start"
	if len(o.positional) > 1 {
		action = o.positional[1]
	}
	addr := fmt.Sprintf("http://localhost:%d", port)
	switch {
	case action == "stop":
		resp, err := http.Post(addr+"/api/shutdown", "", nil)
		if err != nil {
			fmt.Fprintln(stdout, "UI server is not running.")
			return nil
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusAccepted {
			fmt.Fprintf(stdout, "Shutdown request sent to %s.\n", addr)
		} else {
			fmt.Fprintf(stdout, "Unexpected response: %d\n", resp.StatusCode)
		}
		return nil
	case action == "status":
		var st struct {
			PID  int `json:"pid"`
			Port int `json:"port"`
		}
		if err := getStatus(addr, &st); err != nil {
			fmt.Fprintln(stdout, "UI server is not running.")
			return nil
		}
		fmt.Fprintf(stdout, "UI server is running (pid: %d, port: %d).\n", st.PID, st.Port)
		return nil
	case o.foreground:
		return serveUI(o, port, stdout)
	default:
		return startBackground(o, port, addr, stdout, stderr)
	}
}

func getStatus(addr string, v any) error {
	resp, err := http.Get(addr + "/api/status")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func serveUI(o *options, port int, stdout io.Writer) error {
	d, err := openWrite(o)
	if err != nil {
		return err
	}
	defer d.Close()

	// Loopback only: the API serves transcripts and local images, which
	// must not be reachable from the network.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "recall UI: http://localhost:%d\n", ln.Addr().(*net.TCPAddr).Port)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	s := web.New(d, config.ProjectsDir())
	s.Shutdown = stop
	go catchUp(d, stdout)
	return s.Serve(ctx, ln)
}

// startBackground runs `recall ui --foreground` detached and waits for it
// to answer.
func startBackground(o *options, port int, addr string, stdout, stderr io.Writer) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(self, "ui", "--foreground", "--port", strconv.Itoa(port), "--db", o.db)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	cmd.Process.Release()

	for range 20 {
		time.Sleep(250 * time.Millisecond)
		var st struct {
			PID int `json:"pid"`
		}
		if getStatus(addr, &st) == nil {
			fmt.Fprintf(stdout, "recall UI: %s (pid: %d)\n", addr, st.PID)
			return nil
		}
	}
	fmt.Fprintln(stderr, "Failed to start UI server.")
	return nil
}

func runTUI(o *options) error {
	// The first run leaves a commented config to edit; one that is there,
	// or a directory that cannot be written, is left alone.
	_ = config.WriteTemplate(config.FilePath())
	cfg, err := config.Load(config.FilePath())
	if err != nil {
		return err
	}
	d, err := openRead(o)
	if err != nil {
		return err
	}
	defer d.Close()
	sessions, err := d.Sessions()
	if err != nil {
		return err
	}

	model := tui.New(sessions, d, cfg.TUI).RememberIn(config.StatePath())
	if wd, err := os.Getwd(); err == nil {
		model = model.StartIn(wd)
	}
	final, err := tea.NewProgram(model).Run()
	if err != nil {
		return err
	}
	m, ok := final.(tui.Model)
	if !ok || m.Result == nil {
		return nil
	}
	d.Close()
	return resume(*m.Result)
}

// resume replaces this process with claude -r, run from the session's folder
// so Claude Code finds the transcript.
func resume(r tui.Resume) error {
	claude, err := exec.LookPath("claude")
	if err != nil {
		return errors.New("claude is not on PATH")
	}
	if err := os.Chdir(r.Dir); err != nil {
		return fmt.Errorf("cd %s: %w", r.Dir, err)
	}
	return syscall.Exec(claude, []string{"claude", "-r", r.SessionID}, os.Environ())
}
