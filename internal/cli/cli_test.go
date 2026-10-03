package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/babarot/claude-recall/internal/db"
)

func ptr[T any](v T) *T { return &v }

func TestDisplayProject(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	cases := []struct {
		path    *string
		project string
		want    string
	}{
		{ptr("/home/me/src/app"), "-home-me-src-app", "~/src/app"},
		{ptr("/work/app"), "-work-app", "/work/app"},
		{nil, "-home-me-src-app", "~/src/app"}, // the dir name turned back into a path
		{ptr(""), "-work-app", "/work/app"},
	}
	for _, c := range cases {
		if got := DisplayProject(c.path, c.project); got != c.want {
			t.Errorf("%v %q: got %q, want %q", c.path, c.project, got, c.want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{
		0: "0 B", 1023: "1023 B", 1024: "1.0 KB", 1536: "1.5 KB",
		1024*1024 - 1: "1024.0 KB", 1024 * 1024: "1.0 MB", 5 * 1024 * 1024: "5.0 MB",
	}
	for n, want := range cases {
		if got := formatBytes(n); got != want {
			t.Errorf("%d: got %q, want %q", n, got, want)
		}
	}
}

// Long values are cut to keep the columns: a project from the left, a
// branch and a prompt from the right, and a search snippet at 200.
func TestTruncation(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("HOME", "/home/me")
	var b bytes.Buffer
	List(&b, []db.ListedSession{{
		SessionID:    "abcdef0123456789",
		Project:      "-x",
		ProjectPath:  ptr("/work/" + strings.Repeat("p", 40)),
		GitBranch:    ptr("feature/" + strings.Repeat("b", 40)),
		FirstPrompt:  ptr(strings.Repeat("あ", 70)),
		MessageCount: ptr(int64(3)),
		StartedAt:    ptr("2026-03-01T10:00:00.000Z"),
	}})
	row := strings.Split(b.String(), "\n")[2]
	for _, want := range []string{
		"abcdef01 ",
		"..." + strings.Repeat("p", 25) + " ",
		"feature/" + strings.Repeat("b", 20) + "...",
		strings.Repeat("あ", 60) + "...",
	} {
		if !strings.Contains(row, want) {
			t.Errorf("row %q lacks %q", row, want)
		}
	}

	b.Reset()
	Search(&b, []db.SearchResult{{SessionID: "abcdef0123456789", Project: "-x", Role: "user", Content: strings.Repeat("x", 250)}})
	if !strings.Contains(b.String(), "user: "+strings.Repeat("x", 200)+"...\n") {
		t.Errorf("snippet not cut at 200: %q", b.String())
	}
}

// The title wins over the first prompt in the list.
func TestListPrefersTitle(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var b bytes.Buffer
	List(&b, []db.ListedSession{{SessionID: "abcdef01", Project: "-x", Title: ptr("Fix login"), FirstPrompt: ptr("hello")}})
	if !strings.Contains(b.String(), " Fix login") || strings.Contains(b.String(), "hello") {
		t.Errorf("got %q", b.String())
	}
}
