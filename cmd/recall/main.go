// Command recall is the Go port of agent-recall. During the migration it is
// built as recall-go and only implements what has been ported so far.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	tea "charm.land/bubbletea/v2"

	"github.com/babarot/claude-recall/internal/config"
	"github.com/babarot/claude-recall/internal/db"
	"github.com/babarot/claude-recall/internal/importer"
	"github.com/babarot/claude-recall/internal/tui"
)

const usage = `recall - Archive and search coding agent sessions (Go port, in progress)

Usage:
  recall [tui]                  Browse sessions interactively
  recall import [options]       Import sessions into the vault
  recall search <query> [opts]  Full-text search across sessions

Global Options:
  --db <path>     Database path (default: ~/.claude/vault.db)

Import Options:
  --session <id>  Import a specific session
  --project <name> Import sessions for a project
  --dry-run       Show what would be imported

Search Options:
  --project <name> Limit to a project
  --limit <n>     Max results (default: 20)
  --from <date>   Start date (YYYY-MM-DD)
  --to <date>     End date (YYYY-MM-DD)
  --format <fmt>  Output format: json (text is not ported yet)
`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Fprint(stdout, usage)
		return nil
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return runTUI(args)
	}
	switch args[0] {
	case "tui":
		return runTUI(args[1:])
	case "search":
		return runSearch(args[1:], stdout)
	case "import":
		return runImport(args[1:], stdout)
	default:
		return fmt.Errorf("unknown or not yet ported command: %s", args[0])
	}
}

// parseInterspersed parses flags that may appear before, between or after
// positional arguments, like @std/cli parseArgs in the TypeScript version.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return positional, nil
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
}

func runSearch(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	dbPath := fs.String("db", config.DefaultDBPath(), "database path")
	project := fs.String("project", "", "limit to a project")
	limit := fs.Int("limit", 0, "max results")
	from := fs.String("from", "", "start date")
	to := fs.String("to", "", "end date")
	format := fs.String("format", "text", "output format")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	query := strings.Join(positional, " ")
	if query == "" {
		return errors.New("usage: recall search <query>")
	}
	if *format != "json" {
		return fmt.Errorf("--format %s is not ported yet; use --format json", *format)
	}

	d, err := db.Open(*dbPath, db.Options{ReadOnly: true})
	if err != nil {
		return err
	}
	defer d.Close()

	results, err := d.Search(query, db.SearchOptions{Project: *project, Limit: *limit, From: *from, To: *to})
	if err != nil {
		return err
	}
	if len(results) == 0 {
		fmt.Fprintln(stdout, "No results found.")
		return nil
	}
	return writeJSON(stdout, results)
}

// writeJSON matches JSON.stringify(v, null, 2) followed by console.log.
func writeJSON(w io.Writer, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// guardLiveDB refuses to write the live archive while the TypeScript
// version still writes it: two writers that each delete and reinsert a
// session can leave it half imported. The guard goes away when the Go port
// replaces the TypeScript one.
func guardLiveDB(path string) error {
	if filepath.Clean(path) == filepath.Clean(config.DefaultDBPath()) && os.Getenv("RECALL_ALLOW_LIVE_DB") != "1" {
		return fmt.Errorf("refusing to write %s while the TypeScript version owns it; pass --db with a copy", path)
	}
	return nil
}

func runImport(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	dbPath := fs.String("db", config.DefaultDBPath(), "database path")
	session := fs.String("session", "", "import a specific session")
	project := fs.String("project", "", "import sessions for a project")
	var dryRun bool
	fs.BoolVar(&dryRun, "dry-run", false, "show what would be imported")
	fs.BoolVar(&dryRun, "n", false, "show what would be imported")
	if _, err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if !dryRun {
		if err := guardLiveDB(*dbPath); err != nil {
			return err
		}
	}
	return importer.Run(func() (*db.DB, error) { return db.Open(*dbPath, db.Options{}) }, importer.Options{
		ProjectsDir: config.ProjectsDir(),
		Session:     *session,
		Project:     *project,
		DryRun:      dryRun,
	}, stdout)
}

func runTUI(args []string) error {
	fs := flag.NewFlagSet("tui", flag.ContinueOnError)
	dbPath := fs.String("db", config.DefaultDBPath(), "database path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(config.FilePath())
	if err != nil {
		return err
	}

	d, err := db.Open(*dbPath, db.Options{ReadOnly: true})
	if err != nil {
		return err
	}
	defer d.Close()
	sessions, err := d.Sessions()
	if err != nil {
		return err
	}

	final, err := tea.NewProgram(tui.New(sessions, d, cfg.TUI)).Run()
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
