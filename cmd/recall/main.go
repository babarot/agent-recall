// Command recall archives Claude Code sessions in SQLite and lets you find
// them again: a TUI to browse, a CLI to search, an MCP server for agents and
// a web UI.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

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

func run(args []string, stdout, stderr io.Writer) error {
	root := newRootCmd()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	return root.Execute()
}

// options are the flags of every subcommand, each defined only on the
// commands it applies to.
type options struct {
	db, session, project, format, from, to, output string
	limit, port                                    int
	dryRun, foreground                             bool
	limitSet                                       bool
}

// limitArg is the --limit given, or nil to use the command's default.
func (o *options) limitArg() *int {
	if !o.limitSet {
		return nil
	}
	n := o.limit
	return &n
}

func newRootCmd() *cobra.Command {
	o := &options{}
	root := &cobra.Command{
		Use:   "recall",
		Short: "Archive and search coding agent sessions",
		Long: `recall archives Claude Code sessions in SQLite and lets you find them again:
a TUI to browse them, a CLI to search them, an MCP server for agents and a web UI.

Run without a command, it opens the TUI.`,
		Version:       version.Version,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          func(c *cobra.Command, _ []string) error { return runTUI(o, c) },
	}
	root.SetVersionTemplate("recall {{.Version}}\n")
	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return fmt.Errorf("%w\nRun '%s --help' for usage.", err, c.CommandPath())
	})
	root.PersistentFlags().StringVar(&o.db, "db", "", "database path (default: db under [core] in the config file, or ~/.claude/vault.db)")
	// The config file gives the defaults of --db and --port. Every command
	// reads it, so the MCP server and the SessionEnd import use the same
	// archive as the TUI.
	root.PersistentPreRunE = func(c *cobra.Command, _ []string) error {
		cfg, err := config.LoadCore(config.FilePath())
		if err != nil {
			return err
		}
		if !c.Flags().Changed("db") {
			o.db = cfg.DBPath()
		}
		if f := c.Flags().Lookup("port"); f != nil && !f.Changed {
			o.port = cfg.UI.Port
		}
		return nil
	}

	project := func(c *cobra.Command, what string) {
		c.Flags().StringVar(&o.project, "project", "", what)
	}
	// --limit and --format default differently per command, so each command
	// has its own variable and copies it into o when it runs: pflag writes
	// the default when a flag is defined, so a shared variable would end up
	// with the last command's default.
	limit := func(c *cobra.Command, def int, what string) func() {
		n := c.Flags().Int("limit", def, what)
		return func() { o.limit, o.limitSet = *n, c.Flags().Changed("limit") }
	}
	format := func(c *cobra.Command, allowed ...string) func() error {
		f := c.Flags().String("format", allowed[0], "output format: "+strings.Join(allowed, ", "))
		return func() error {
			if !slices.Contains(allowed, *f) {
				return fmt.Errorf("--format must be %s, got %q", strings.Join(allowed, ", "), *f)
			}
			o.format = *f
			return nil
		}
	}

	tuiCmd := &cobra.Command{
		Use:   "tui",
		Short: "Browse sessions interactively (the default)",
		Args:  cobra.NoArgs,
		RunE:  func(c *cobra.Command, _ []string) error { return runTUI(o, c) },
	}
	// --all is the TUI's: on recall itself, not on every command.
	for _, c := range []*cobra.Command{root, tuiCmd} {
		c.Flags().Bool("all", false, "start with every folder's sessions (default: scope under [tui] in the config file)")
	}

	importCmd := &cobra.Command{
		Use:   "import",
		Short: "Import sessions into the vault",
		Args:  cobra.NoArgs,
		RunE:  func(c *cobra.Command, _ []string) error { return runImport(o, c.OutOrStdout()) },
	}
	importCmd.Flags().StringVar(&o.session, "session", "", "import a specific session (an ID prefix works)")
	project(importCmd, "import sessions whose project matches")
	importCmd.Flags().BoolVarP(&o.dryRun, "dry-run", "n", false, "show what would be imported without writing")

	searchCmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Full-text search across sessions",
		Example: `  recall search "terraform module"
  recall search deploy --project oksskolten --from 2026-03-01`,
		Args: usageArgs(cobra.MinimumNArgs(1)),
	}
	project(searchCmd, "limit to a project")
	searchLimit := limit(searchCmd, 20, "max results")
	searchCmd.Flags().StringVar(&o.from, "from", "", "start date (YYYY-MM-DD)")
	searchCmd.Flags().StringVar(&o.to, "to", "", "end date (YYYY-MM-DD)")
	searchFormat := format(searchCmd, "text", "json")
	searchCmd.RunE = func(c *cobra.Command, args []string) error {
		searchLimit()
		if err := searchFormat(); err != nil {
			return err
		}
		return runSearch(o, strings.Join(args, " "), c.OutOrStdout())
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List archived sessions",
		Args:  cobra.NoArgs,
	}
	project(listCmd, "filter by project")
	listLimit := limit(listCmd, 50, "max sessions")
	listFormat := format(listCmd, "text", "json")
	listCmd.RunE = func(c *cobra.Command, _ []string) error {
		listLimit()
		if err := listFormat(); err != nil {
			return err
		}
		return runList(o, c.OutOrStdout())
	}

	exportCmd := &cobra.Command{
		Use:     "export <session-id>",
		Short:   "Export a session",
		Long:    "Export a session's conversation. A session ID prefix works.",
		Example: "  recall export a1b2 --format json --output session.json",
		Args:    usageArgs(cobra.ExactArgs(1)),
	}
	exportFormat := format(exportCmd, "markdown", "json", "text")
	exportCmd.Flags().StringVar(&o.output, "output", "", "write to a file instead of stdout")
	exportCmd.RunE = func(c *cobra.Command, args []string) error {
		if err := exportFormat(); err != nil {
			return err
		}
		return runExport(o, args[0], c.OutOrStdout(), c.ErrOrStderr())
	}

	statsCmd := &cobra.Command{
		Use:   "stats",
		Short: "Show archive statistics",
		Args:  cobra.NoArgs,
		RunE:  func(c *cobra.Command, _ []string) error { return runStats(o, c.OutOrStdout()) },
	}
	project(statsCmd, "limit to a project")

	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "Start the MCP server (stdio transport)",
		Args:  cobra.NoArgs,
		RunE:  func(*cobra.Command, []string) error { return runMCP(o) },
	}

	uiCmd := &cobra.Command{
		Use:   "ui",
		Short: "Start the web UI in the background",
		Long: fmt.Sprintf(`Start the web UI in the background, on http://localhost:%d unless port under
[ui] in the config file or --port says otherwise. If it is already running,
print its URL.`, config.DefaultPort),
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if o.foreground {
				return serveUI(o, c.OutOrStdout())
			}
			return startBackground(o, c.OutOrStdout(), c.ErrOrStderr())
		},
	}
	uiCmd.PersistentFlags().IntVar(&o.port, "port", 0, "port of the web UI (default: port under [ui] in the config file, or 6276)")
	uiCmd.Flags().BoolVar(&o.foreground, "foreground", false, "run in the foreground")
	uiCmd.AddCommand(
		&cobra.Command{
			Use:   "stop",
			Short: "Stop the running web UI",
			Args:  cobra.NoArgs,
			RunE:  func(c *cobra.Command, _ []string) error { return stopUI(o, c.OutOrStdout()) },
		},
		&cobra.Command{
			Use:   "status",
			Short: "Show whether the web UI is running",
			Args:  cobra.NoArgs,
			RunE:  func(c *cobra.Command, _ []string) error { return uiStatus(o, c.OutOrStdout()) },
		},
	)

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Show the version",
		Args:  cobra.NoArgs,
		Run:   func(c *cobra.Command, _ []string) { fmt.Fprintf(c.OutOrStdout(), "recall %s\n", version.Version) },
	}

	root.AddCommand(tuiCmd, importCmd, searchCmd, listCmd, exportCmd, statsCmd, mcpCmd, uiCmd, versionCmd)
	return root
}

// usageArgs adds the usage line to an argument count error.
func usageArgs(check cobra.PositionalArgs) cobra.PositionalArgs {
	return func(c *cobra.Command, args []string) error {
		if err := check(c, args); err != nil {
			return fmt.Errorf("%w\nUsage: %s", err, c.UseLine())
		}
		return nil
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

func runSearch(o *options, query string, stdout io.Writer) error {
	d, err := openRead(o)
	if err != nil {
		return err
	}
	results, err := d.Search(query, db.SearchOptions{Project: o.project, Limit: o.limitArg(), From: o.from, To: o.to})
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
	d, err := openRead(o)
	if err != nil {
		return err
	}
	sessions, err := d.ListSessions(db.ListOptions{Project: o.project, Limit: o.limitArg()})
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

func runExport(o *options, id string, stdout, stderr io.Writer) error {
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

func uiAddr(o *options) string { return fmt.Sprintf("http://localhost:%d", o.port) }

func stopUI(o *options, stdout io.Writer) error {
	addr := uiAddr(o)
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
}

func uiStatus(o *options, stdout io.Writer) error {
	var st struct {
		PID  int `json:"pid"`
		Port int `json:"port"`
	}
	if err := getStatus(uiAddr(o), &st); err != nil {
		fmt.Fprintln(stdout, "UI server is not running.")
		return nil
	}
	fmt.Fprintf(stdout, "UI server is running (pid: %d, port: %d).\n", st.PID, st.Port)
	return nil
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

func serveUI(o *options, stdout io.Writer) error {
	d, err := openWrite(o)
	if err != nil {
		return err
	}
	defer d.Close()

	// Loopback only: the API serves transcripts and local images, which
	// must not be reachable from the network.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", o.port))
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
func startBackground(o *options, stdout, stderr io.Writer) error {
	addr := uiAddr(o)
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(self, "ui", "--foreground", "--port", strconv.Itoa(o.port), "--db", o.db)
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

// tuiConfig reads the TUI's settings, with --all, or --all=false, in place
// of tui.scope for this run. The first run leaves a commented config to
// edit; one that is there, or a directory that cannot be written, is left
// alone.
func tuiConfig(c *cobra.Command) (config.TUI, error) {
	_ = config.WriteTemplate(config.FilePath())
	cfg, err := config.Load(config.FilePath())
	if err != nil {
		return config.TUI{}, err
	}
	if c.Flags().Changed("all") {
		cfg.TUI.Scope = config.ScopeFolder
		if all, _ := c.Flags().GetBool("all"); all {
			cfg.TUI.Scope = config.ScopeAll
		}
	}
	return cfg.TUI, nil
}

func runTUI(o *options, c *cobra.Command) error {
	cfg, err := tuiConfig(c)
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

	model := tui.New(sessions, d, cfg).RememberIn(config.StatePath()).SettleSize().LoadInBackground()
	if wd, err := os.Getwd(); err == nil {
		model = model.StartIn(wd)
	}
	// Asking Claude gives it this recall's MCP server on the same database,
	// run from a directory of its own so no project's settings apply.
	if exe, err := os.Executable(); err == nil {
		model = model.AskWith([]string{exe, "mcp", "--db", o.db}, filepath.Join(filepath.Dir(config.StatePath()), "ask"))
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
