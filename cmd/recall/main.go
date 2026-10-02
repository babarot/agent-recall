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
	"strings"

	"github.com/babarot/claude-recall/internal/config"
	"github.com/babarot/claude-recall/internal/db"
)

const usage = `recall - Archive and search coding agent sessions (Go port, in progress)

Usage:
  recall search <query> [opts]  Full-text search across sessions

Global Options:
  --db <path>     Database path (default: ~/.claude/vault.db)

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
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stdout, usage)
		return nil
	}
	switch args[0] {
	case "search":
		return runSearch(args[1:], stdout)
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
