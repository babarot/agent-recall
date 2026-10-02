package db

import (
	"path/filepath"
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
