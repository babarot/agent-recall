package db

import (
	"path/filepath"
	"strings"
	"testing"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "vault.db"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func seedSession(t *testing.T, d *DB, id, project, projectPath string) {
	t.Helper()
	_, err := d.sql.Exec(`INSERT INTO sessions (session_id, project, project_path, git_branch, first_prompt, message_count, started_at, ended_at, claude_version)
		VALUES (?, ?, ?, 'main', ?, 0, '2026-01-01T00:00:00Z', '2026-01-01T00:10:00Z', '2.1.87')`,
		id, project, projectPath, "prompt for "+id)
	if err != nil {
		t.Fatal(err)
	}
}

func seedMessage(t *testing.T, d *DB, sessionID, uuid, role, content, timestamp string, turn int) {
	t.Helper()
	_, err := d.sql.Exec(`INSERT OR IGNORE INTO messages (session_id, uuid, role, block_type, block_index, content, timestamp, turn_index)
		VALUES (?, ?, ?, 'text', 0, ?, ?, ?)`, sessionID, uuid, role, content, timestamp, turn)
	if err != nil {
		t.Fatal(err)
	}
}

const ts = "2026-01-01T00:00:00Z"

func search(t *testing.T, d *DB, q string, opts SearchOptions) []SearchResult {
	t.Helper()
	r, err := d.Search(q, opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestSearchFindsMatchingMessages(t *testing.T) {
	d := newTestDB(t)
	seedSession(t, d, "s1", "test-project", "/home/user/test-project")
	seedMessage(t, d, "s1", "m1", "user", "deploy terraform infrastructure", ts, 0)
	seedMessage(t, d, "s1", "m2", "assistant", "deployment complete", ts, 1)

	r := search(t, d, "terraform", SearchOptions{})
	if len(r) != 1 || r[0].Content != "deploy terraform infrastructure" {
		t.Fatalf("got %+v", r)
	}
}

func TestSearchPorterStemmer(t *testing.T) {
	d := newTestDB(t)
	seedSession(t, d, "s1", "test-project", "/home/user/test-project")
	seedMessage(t, d, "s1", "m1", "user", "running the tests", ts, 0)

	if r := search(t, d, "run", SearchOptions{}); len(r) != 1 {
		t.Fatalf("got %d results, want 1", len(r))
	}
}

func TestSearchFiltersByProject(t *testing.T) {
	d := newTestDB(t)
	seedSession(t, d, "s1", "project-alpha", "/home/user/project-alpha")
	seedSession(t, d, "s2", "project-beta", "/home/user/project-beta")
	seedMessage(t, d, "s1", "m1", "user", "hello world", ts, 0)
	seedMessage(t, d, "s2", "m2", "user", "hello world", ts, 0)

	r := search(t, d, "hello", SearchOptions{Project: "alpha"})
	if len(r) != 1 || r[0].SessionID != "s1" {
		t.Fatalf("got %+v", r)
	}
}

func TestSearchFiltersByProjectPath(t *testing.T) {
	d := newTestDB(t)
	seedSession(t, d, "s1", "-Users-bob-src-alpha", "/Users/bob/src/alpha")
	seedSession(t, d, "s2", "-Users-bob-src-beta", "/Users/bob/src/beta")
	seedMessage(t, d, "s1", "m1", "user", "hello world", ts, 0)
	seedMessage(t, d, "s2", "m2", "user", "hello world", ts, 0)

	r := search(t, d, "hello", SearchOptions{Project: "/Users/bob/src/alpha"})
	if len(r) != 1 || r[0].SessionID != "s1" {
		t.Fatalf("got %+v", r)
	}
}

func TestSearchFiltersByDateRange(t *testing.T) {
	d := newTestDB(t)
	seedSession(t, d, "s1", "test-project", "/home/user/test-project")
	seedMessage(t, d, "s1", "m1", "user", "early message", "2026-01-01T00:00:00Z", 0)
	seedMessage(t, d, "s1", "m2", "user", "late message", "2026-06-01T00:00:00Z", 1)

	r := search(t, d, "message", SearchOptions{From: "2026-03-01"})
	if len(r) != 1 || r[0].Content != "late message" {
		t.Fatalf("got %+v", r)
	}
}

func TestSearchRespectsLimit(t *testing.T) {
	d := newTestDB(t)
	seedSession(t, d, "s1", "test-project", "/home/user/test-project")
	for i := range 10 {
		seedMessage(t, d, "s1", "m"+string(rune('0'+i)), "user", "item number "+string(rune('0'+i)), ts, i)
	}

	if r := search(t, d, "item", SearchOptions{Limit: new(3)}); len(r) != 3 {
		t.Fatalf("got %d results, want 3", len(r))
	}
}

func TestSearchNoMatches(t *testing.T) {
	d := newTestDB(t)
	seedSession(t, d, "s1", "test-project", "/home/user/test-project")
	seedMessage(t, d, "s1", "m1", "user", "hello world", ts, 0)

	if r := search(t, d, "nonexistent", SearchOptions{}); len(r) != 0 {
		t.Fatalf("got %d results, want 0", len(r))
	}
}

func TestSearchHyphenatedQuery(t *testing.T) {
	d := newTestDB(t)
	seedSession(t, d, "s1", "test-project", "/home/user/test-project")
	seedMessage(t, d, "s1", "m1", "user", "session 43968160-3681-46c0", ts, 0)

	if r := search(t, d, "43968160-3681", SearchOptions{}); len(r) != 1 {
		t.Fatalf("got %d results, want 1", len(r))
	}
}

func TestSearchExplicitOperators(t *testing.T) {
	d := newTestDB(t)
	seedSession(t, d, "s1", "test-project", "/home/user/test-project")
	seedMessage(t, d, "s1", "m1", "user", "terraform module deploy", ts, 0)

	if r := search(t, d, "terraform AND module", SearchOptions{}); len(r) != 1 {
		t.Fatalf("got %d results, want 1", len(r))
	}
	if r := search(t, d, "terraform AND nonexistent", SearchOptions{}); len(r) != 0 {
		t.Fatalf("got %d results, want 0", len(r))
	}
}

func TestSearchQuotedPhrase(t *testing.T) {
	d := newTestDB(t)
	seedSession(t, d, "s1", "test-project", "/home/user/test-project")
	seedMessage(t, d, "s1", "m1", "user", "terraform state migration plan", ts, 0)

	if r := search(t, d, `"state migration"`, SearchOptions{}); len(r) != 1 {
		t.Fatalf("got %d results, want 1", len(r))
	}
}

func TestOpenReadOnlyDoesNotWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.db")
	w, err := Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	w.Close()

	r, err := Open(path, Options{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.sql.Exec(`INSERT INTO sessions (session_id, project) VALUES ('x', 'p')`); err == nil {
		t.Fatal("write succeeded on a read-only database")
	}
}

func TestFTSQuery(t *testing.T) {
	cases := map[string]string{
		`terraform`:            `"terraform"`,
		`43968160-3681`:        `"43968160-3681"`,
		`say "hi"`:             `"say ""hi"""`,
		`"state migration"`:    `"state migration"`,
		`terraform AND module`: `terraform AND module`,
		`ANDROID`:              `"ANDROID"`,
	}
	for in, want := range cases {
		if got := ftsQuery(in); got != want {
			t.Errorf("ftsQuery(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMigrationAddsTitleToAnExistingArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.db")
	// An archive from before migrations: the base schema only.
	old, err := Open(path, Options{ReadOnly: false})
	if err != nil {
		t.Fatal(err)
	}
	old.sql.Exec(`ALTER TABLE sessions DROP COLUMN title`)
	old.sql.Exec(`PRAGMA user_version = 0`)
	seedSession(t, old, "s1", "p", "/p")
	old.Close()

	d, err := Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var version int
	d.sql.QueryRow(`PRAGMA user_version`).Scan(&version)
	if version != 1 {
		t.Fatalf("user_version %d", version)
	}
	fi, err := d.GetFileInfo("s1")
	if err != nil || fi == nil || fi.HasTitle {
		t.Fatalf("existing rows must keep a NULL title: %+v %v", fi, err)
	}
	// Opening again must not run the migration twice.
	d.Close()
	if d, err = Open(path, Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestSessionDetail(t *testing.T) {
	d := newTestDB(t)
	seedSession(t, d, "s1", "p", "/p")
	seedMessage(t, d, "s1", "u1", "user", "fix the login bug", "2026-01-01T00:00:00Z", 0)
	seedMessage(t, d, "s1", "a1", "assistant", "looking", "2026-01-01T00:01:00Z", 1)
	tool := func(uuid, name, input string, turn int) {
		t.Helper()
		if _, err := d.sql.Exec(`INSERT INTO messages (session_id, uuid, role, block_type, block_index, content, tool_name, tool_input, timestamp, turn_index)
			VALUES ('s1', ?, 'assistant', 'tool_use', 0, ?, ?, ?, '2026-01-01T00:02:00Z', ?)`, uuid, name, name, input, turn); err != nil {
			t.Fatal(err)
		}
	}
	tool("t1", "Edit", `{"file_path":"/p/a.go"}`, 2)
	tool("t2", "Edit", `{"file_path":"/p/a.go"}`, 3)
	tool("t3", "Write", `{"file_path":"/p/b.go"}`, 4)
	tool("t4", "Bash", `{"command":"go test ./..."}`, 5)
	tool("t5", "Bash", `{"command":"git status"}`, 6)
	seedMessage(t, d, "s1", "u2", "user", "thanks", "2026-01-01T00:10:00Z", 7)

	got, err := d.SessionDetail("s1")
	if err != nil {
		t.Fatal(err)
	}
	if got.You != 2 || got.Claude != 1 || got.Tools != 5 {
		t.Errorf("counts %+v", got)
	}
	if len(got.TopTools) == 0 || got.TopTools[0] != (Count{"Bash", 2}) && got.TopTools[0] != (Count{"Edit", 2}) {
		t.Errorf("top tools %+v", got.TopTools)
	}
	if got.FileCount != 2 || got.Files[0] != (Count{"/p/a.go", 2}) {
		t.Errorf("files %+v", got.Files)
	}
	if len(got.Commands) != 2 || got.Commands[0] != "git status" {
		t.Errorf("commands newest first: %+v", got.Commands)
	}
	if got.First == nil || got.First.Content != "fix the login bug" || got.Tail[len(got.Tail)-1].Content != "thanks" {
		t.Errorf("conversation %+v %+v", got.First, got.Tail)
	}
	if len(got.Activity) != 24 {
		t.Errorf("activity %v", got.Activity)
	}
}

func TestSessionsWithText(t *testing.T) {
	d := newTestDB(t)
	seedSession(t, d, "s1", "p", "/p")
	seedSession(t, d, "s2", "p", "/p")
	seedMessage(t, d, "s1", "m1", "user", "ドキュメントを作成して", ts, 0)
	seedMessage(t, d, "s1", "m2", "assistant", "Created the Worktree", ts, 1)
	seedMessage(t, d, "s2", "m3", "user", "100% done_now", ts, 0)
	cases := map[string]string{
		"ドキュメント":   "s1", // inside a run of Japanese, which FTS cannot split
		"キュメ":      "s1",
		"worktree": "s1", // ASCII case is ignored
		"100%":     "s2",
		"e_n":      "s2", // _ is literal
		"0%d":      "",   // % is literal
	}
	for q, want := range cases {
		ids, err := d.SessionsWithText(q)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(ids, ","); got != want {
			t.Errorf("SessionsWithText(%q) = %q, want %q", q, got, want)
		}
	}
}

// The detail pane's counts read the index alone, not the message text.
func TestDetailCountsUseACoveringIndex(t *testing.T) {
	d := newTestDB(t)
	for _, q := range []string{
		`SELECT role, block_type, COUNT(*) FROM messages WHERE session_id = ? GROUP BY role, block_type`,
		`SELECT COALESCE(tool_name, ''), COUNT(*) AS n FROM messages
        WHERE session_id = ? AND block_type = 'tool_use' GROUP BY tool_name ORDER BY n DESC, tool_name LIMIT ?`,
	} {
		rows, err := d.sql.Query("EXPLAIN QUERY PLAN "+q, "s", 6)
		if err != nil {
			t.Fatal(err)
		}
		var plan strings.Builder
		for rows.Next() {
			var id, parent, notused int
			var detail string
			rows.Scan(&id, &parent, &notused, &detail)
			plan.WriteString(detail + "\n")
		}
		rows.Close()
		if !strings.Contains(plan.String(), "COVERING INDEX idx_messages_session_kind") {
			t.Errorf("%s\nplan:\n%s", q, plan.String())
		}
	}
}
