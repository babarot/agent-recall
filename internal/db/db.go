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
// schema, then any pending migrations.
func Open(path string, opts Options) (*DB, error) {
	q := url.Values{}
	if opts.ReadOnly {
		q.Add("_pragma", "busy_timeout(5000)")
		q.Set("mode", "ro")
	} else {
		// Every Claude Code session runs its own `recall mcp`, and each one
		// imports on startup and on every transcript change, so writers
		// queue up. Taking the write lock at BEGIN (not at the first write)
		// lets them wait for each other through busy_timeout, instead of a
		// transaction failing at once when another writer committed since it
		// began reading. A full import from many processes at once can keep
		// a writer waiting for a while, hence the long timeout.
		q.Add("_pragma", "busy_timeout(60000)")
		q.Add("_pragma", "journal_mode(WAL)")
		q.Set("_txlock", "immediate")
	}
	dsn := "file:" + path + "?" + q.Encode()

	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	// One connection keeps pragmas and the schema on the same handle and
	// serializes access; every query is short.
	sqlDB.SetMaxOpenConns(1)

	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	d := &DB{sql: sqlDB}
	if !opts.ReadOnly {
		if _, err := sqlDB.Exec(schemaSQL); err != nil {
			sqlDB.Close()
			return nil, fmt.Errorf("apply schema: %w", err)
		}
		if err := d.migrate(); err != nil {
			sqlDB.Close()
			return nil, fmt.Errorf("migrate: %w", err)
		}
	}
	return d, nil
}

// migrations upgrade the schema one PRAGMA user_version at a time. They only
// ever add: the archive keeps sessions whose transcripts are gone, so it can
// never be dropped and rebuilt.
var migrations = []string{
	// 1: the session title Claude Code writes (custom-title or ai-title).
	// NULL means the session was imported before titles were; the importer
	// imports it again. An empty string means it has no title.
	`ALTER TABLE sessions ADD COLUMN title TEXT`,
}

func (d *DB) migrate() error {
	var version int
	if err := d.sql.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		tx, err := d.sql.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Close closes the database.
func (d *DB) Close() error { return d.sql.Close() }

// SearchOptions narrows a full-text search.
type SearchOptions struct {
	Project string // substring of the encoded project dir or the project path
	Limit   *int   // nil means 20
	From    string // inclusive lower bound on the message timestamp
	To      string // inclusive upper bound on the message timestamp
}

// SearchResult is one matching message. Field order and JSON names match the
// output of `search --format json` from before the Go port.
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
	limit := 20
	if opts.Limit != nil {
		limit = *opts.Limit
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

// SessionsWithText returns the IDs of the sessions whose conversation text
// (what the user and Claude wrote) contains text, ignoring ASCII case. It
// matches anywhere in a word, which the FTS index cannot do for Japanese and
// other unspaced scripts, at the cost of reading every message.
func (d *DB) SessionsWithText(text string) ([]string, error) {
	pattern := "%" + likeEscaper.Replace(text) + "%"
	rows, err := d.sql.Query(`SELECT DISTINCT session_id FROM messages
        WHERE block_type = 'text' AND content LIKE ? ESCAPE '\'`, pattern)
	if err != nil {
		return nil, fmt.Errorf("sessions with text: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("sessions with text: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// likeEscaper makes % and _ in a LIKE pattern literal.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
