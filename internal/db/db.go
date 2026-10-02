// Package db reads and writes the archive database (~/.claude/vault.db).
package db

import (
	"database/sql"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	_ "modernc.org/sqlite"
)

// DB wraps the archive database.
type DB struct {
	sql *sql.DB
}

// Options controls how the database is opened.
type Options struct {
	// ReadOnly opens the file with mode=ro and skips applying the schema, so
	// the archive is never modified.
	ReadOnly bool
}

// Open opens the archive at path. A writable database gets WAL mode and the
// schema, like the TypeScript VaultDB constructor.
func Open(path string, opts Options) (*DB, error) {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	if opts.ReadOnly {
		q.Set("mode", "ro")
	} else {
		q.Add("_pragma", "journal_mode(WAL)")
	}
	dsn := "file:" + path + "?" + q.Encode()

	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	// One connection keeps pragmas and the schema on the same handle, and
	// matches the single synchronous handle the TypeScript version uses.
	sqlDB.SetMaxOpenConns(1)

	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if !opts.ReadOnly {
		if _, err := sqlDB.Exec(schemaSQL); err != nil {
			sqlDB.Close()
			return nil, fmt.Errorf("apply schema: %w", err)
		}
	}
	return &DB{sql: sqlDB}, nil
}

// Close closes the database.
func (d *DB) Close() error { return d.sql.Close() }

// SearchOptions narrows a full-text search.
type SearchOptions struct {
	Project string // substring of the encoded project dir or the project path
	Limit   int    // defaults to 20
	From    string // inclusive lower bound on the message timestamp
	To      string // inclusive upper bound on the message timestamp
}

// SearchResult is one matching message. Field order and JSON names match the
// TypeScript output so `search --format json` stays byte-compatible.
type SearchResult struct {
	SessionID   string  `json:"sessionId"`
	Project     string  `json:"project"`
	ProjectPath *string `json:"projectPath"`
	GitBranch   *string `json:"gitBranch"`
	StartedAt   *string `json:"startedAt"`
	Role        string  `json:"role"`
	Content     string  `json:"content"`
	Timestamp   *string `json:"timestamp"`
}

var (
	quotedPhrase = regexp.MustCompile(`^".*"$`)
	ftsOperator  = regexp.MustCompile(`\b(AND|OR|NOT)\b`)
)

// ftsQuery quotes a query as a single phrase so characters like hyphens are
// not read as FTS5 syntax, unless the user already quoted it or used an
// explicit AND/OR/NOT.
func ftsQuery(query string) string {
	if quotedPhrase.MatchString(query) || ftsOperator.MatchString(query) {
		return query
	}
	return `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
}

// Search runs an FTS5 search across message text, best matches first.
func (d *DB) Search(query string, opts SearchOptions) ([]SearchResult, error) {
	limit := opts.Limit
	if limit == 0 {
		limit = 20
	}

	conds := []string{"messages_fts MATCH ?"}
	args := []any{ftsQuery(query)}
	if opts.Project != "" {
		conds = append(conds, "(s.project LIKE ? OR s.project_path LIKE ?)")
		args = append(args, "%"+opts.Project+"%", "%"+opts.Project+"%")
	}
	if opts.From != "" {
		conds = append(conds, "m.timestamp >= ?")
		args = append(args, opts.From)
	}
	if opts.To != "" {
		conds = append(conds, "m.timestamp <= ?")
		args = append(args, opts.To)
	}
	args = append(args, limit)

	rows, err := d.sql.Query(`
      SELECT s.session_id, s.project, s.project_path, s.git_branch,
             s.started_at, m.role, m.content, m.timestamp
      FROM messages_fts
      JOIN messages m ON m.id = messages_fts.rowid
      JOIN sessions s ON s.session_id = m.session_id
      WHERE `+strings.Join(conds, " AND ")+`
      ORDER BY rank
      LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()

	results := []SearchResult{}
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.SessionID, &r.Project, &r.ProjectPath, &r.GitBranch,
			&r.StartedAt, &r.Role, &r.Content, &r.Timestamp); err != nil {
			return nil, fmt.Errorf("search: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}
