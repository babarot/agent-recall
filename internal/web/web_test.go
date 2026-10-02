package web

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/babarot/claude-recall/internal/db"
	"github.com/babarot/claude-recall/internal/importer"
)

func newServer(t *testing.T) *Server {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "vault.db"), db.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return New(d, "")
}

func get(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestEmptyArchiveResponses(t *testing.T) {
	s := newServer(t)
	cases := map[string]string{
		"/api/sessions":                  `[]`,
		"/api/search?q=":                 `[]`,
		"/api/search?q=hello":            `[]`,
		"/api/sessions/nope":             `{"session":null,"messages":[]}`,
		"/api/stats":                     `{"totalSessions":0,"totalMessages":0,"byProject":[],"byMonth":[]}`,
		"/api/status":                    `{"status":"running",`,
		"/api/nope":                      `Not Found`,
		"/api/file?path=relative":        `Bad Request`,
		"/api/image?session=x&message=y": `Not Found`,
	}
	for path, want := range cases {
		rec := get(t, s, path)
		if !strings.HasPrefix(rec.Body.String(), want) {
			t.Errorf("%s: got %d %q, want prefix %q", path, rec.Code, rec.Body.String(), want)
		}
	}
}

func TestShutdownEndpoint(t *testing.T) {
	s := newServer(t)
	called := make(chan struct{})
	s.Shutdown = func() { close(called) }
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/shutdown", nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("code %d", rec.Code)
	}
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("shutdown not called")
	}
}

func TestStreamSendsHelloAndEvents(t *testing.T) {
	s := newServer(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- s.Serve(ctx, ln) }()
	defer func() { cancel(); <-done }()

	resp, err := http.Get("http://" + ln.Addr().String() + "/api/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	r := bufio.NewReader(resp.Body)
	hello, _ := r.ReadString('\n')
	if !strings.HasPrefix(hello, `data: {"type":"connected","timestamp":`) {
		t.Fatalf("hello %q", hello)
	}
	r.ReadString('\n') // blank line ending the frame

	for s.Broadcaster.Count() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	r2 := &importer.Result{Status: importer.New, SessionID: "s1", Project: "p", TotalMessages: 3}
	s.Broadcaster.Broadcast(SessionUpdated{Type: "session_updated", SessionID: r2.SessionID, Project: r2.Project,
		Status: string(r2.Status), AddedMessages: 3, TotalMessages: 3})
	ev, _ := r.ReadString('\n')
	want := `data: {"type":"session_updated","sessionId":"s1","project":"p","status":"new","addedMessages":3,"totalMessages":3}` + "\n"
	if ev != want {
		t.Fatalf("event %q", ev)
	}

	cancel()
	if _, err := io.ReadAll(r); err != nil && !strings.Contains(err.Error(), "EOF") {
		t.Logf("stream closed with %v", err)
	}
}

func TestFileServesOnlyImages(t *testing.T) {
	s := newServer(t)
	dir := t.TempDir()
	png := filepath.Join(dir, "shot.png")
	secret := filepath.Join(dir, "id_ed25519")
	link := filepath.Join(dir, "sneaky.png")
	os.WriteFile(png, []byte("PNG"), 0o644)
	os.WriteFile(secret, []byte("PRIVATE KEY"), 0o600)
	os.Symlink(secret, link)

	cases := []struct {
		path string
		code int
		body string
	}{
		{png, 200, "PNG"},
		{secret, 403, "Forbidden"},
		{link, 403, "Forbidden"},
		{filepath.Join(dir, "missing.png"), 404, "Not Found"},
	}
	for _, c := range cases {
		rec := get(t, s, "/api/file?path="+url.QueryEscape(c.path))
		if rec.Code != c.code || rec.Body.String() != c.body {
			t.Errorf("%s: got %d %q, want %d %q", c.path, rec.Code, rec.Body.String(), c.code, c.body)
		}
	}
}
