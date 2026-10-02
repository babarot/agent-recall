// Package web serves the web UI, its JSON API and server-sent events.
package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/babarot/claude-recall/internal/api"
	"github.com/babarot/claude-recall/internal/db"
	"github.com/babarot/claude-recall/internal/importer"
	"github.com/babarot/claude-recall/internal/jscompat"
	"github.com/babarot/claude-recall/internal/watcher"
	"github.com/babarot/claude-recall/internal/webui"
)

// DefaultPort is where the UI listens unless --port says otherwise.
const DefaultPort = 6276

const keepAlive = 15 * time.Second

// Broadcaster fans server-sent events out to every connected client.
type Broadcaster struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

// NewBroadcaster returns a broadcaster with no clients.
func NewBroadcaster() *Broadcaster { return &Broadcaster{clients: map[chan []byte]struct{}{}} }

func (b *Broadcaster) add() chan []byte {
	ch := make(chan []byte, 64)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *Broadcaster) remove(ch chan []byte) {
	b.mu.Lock()
	if _, ok := b.clients[ch]; ok {
		delete(b.clients, ch)
		close(ch)
	}
	b.mu.Unlock()
}

// Count is the number of connected clients.
func (b *Broadcaster) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.clients)
}

// Broadcast sends one `data: <json>` frame to every client. A client whose
// buffer is full is dropped rather than blocking the others.
func (b *Broadcaster) Broadcast(event any) {
	payload, err := jscompat.Marshal(event, "")
	if err != nil {
		return
	}
	frame := []byte("data: " + string(payload) + "\n\n")
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.clients {
		select {
		case ch <- frame:
		default:
			delete(b.clients, ch)
			close(ch)
		}
	}
}

// CloseAll disconnects every client.
func (b *Broadcaster) CloseAll() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.clients {
		delete(b.clients, ch)
		close(ch)
	}
}

// SessionUpdated is the event pushed when the watcher imports a session.
type SessionUpdated struct {
	Type          string `json:"type"`
	SessionID     string `json:"sessionId"`
	Project       string `json:"project"`
	Status        string `json:"status"`
	AddedMessages int    `json:"addedMessages"`
	TotalMessages int    `json:"totalMessages"`
}

// Server is a running UI server.
type Server struct {
	DB          *db.DB
	Broadcaster *Broadcaster
	// Watcher is nil when watching is disabled.
	Watcher *watcher.Watcher
	// Shutdown is called by POST /api/shutdown.
	Shutdown func()

	port int
}

// New prepares a server. When projectsDir is not empty, a watcher keeps the
// archive current and pushes updates to SSE clients.
func New(d *db.DB, projectsDir string) *Server {
	s := &Server{DB: d, Broadcaster: NewBroadcaster()}
	if projectsDir != "" {
		s.Watcher = &watcher.Watcher{DB: d, ProjectsDir: projectsDir, OnImport: func(r *importer.Result) {
			s.Broadcaster.Broadcast(SessionUpdated{Type: "session_updated", SessionID: r.SessionID, Project: r.Project,
				Status: string(r.Status), AddedMessages: r.TotalMessages, TotalMessages: r.TotalMessages})
		}}
	}
	return s
}

// Serve accepts connections on ln until ctx is canceled, then shuts down
// gracefully.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	s.port = ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: s}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	if s.Watcher != nil {
		wg.Go(func() { s.Watcher.Run(ctx) })
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case <-ctx.Done():
	case err := <-errc:
		cancel()
		wg.Wait()
		return err
	}
	s.Broadcaster.CloseAll()
	shutCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	err := srv.Shutdown(shutCtx)
	wg.Wait()
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return err
}

// statusResponse is the /api/status body.
type statusResponse struct {
	Status     string         `json:"status"`
	PID        int            `json:"pid"`
	Port       int            `json:"port"`
	SSEClients int            `json:"sseClients"`
	Watcher    watcher.Status `json:"watcher"`
}

func writeJSON(w http.ResponseWriter, v any) {
	b, err := jscompat.Marshal(v, "")
	if err != nil {
		internalError(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

func text(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "text/plain;charset=UTF-8")
	w.WriteHeader(code)
	io.WriteString(w, body)
}

func internalError(w http.ResponseWriter) {
	text(w, http.StatusInternalServerError, "Internal Server Error")
}

// ServeHTTP routes a request.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.EscapedPath()
	switch {
	case r.Method == http.MethodPost && path == "/api/shutdown":
		w.WriteHeader(http.StatusAccepted)
		if s.Shutdown != nil {
			time.AfterFunc(100*time.Millisecond, s.Shutdown)
		}
	case path == "/api/status":
		st := watcher.Status{}
		if s.Watcher != nil {
			st = s.Watcher.Status()
		}
		writeJSON(w, statusResponse{Status: "running", PID: os.Getpid(), Port: s.port, SSEClients: s.Broadcaster.Count(), Watcher: st})
	case path == "/api/stream":
		s.stream(w, r)
	case strings.HasPrefix(path, "/api/"):
		if err := s.api(w, r, path); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			internalError(w)
		}
	default:
		serveAsset(w, path)
	}
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		internalError(w)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ch := s.Broadcaster.add()
	defer s.Broadcaster.remove(ch)

	hello, _ := jscompat.Marshal(struct {
		Type      string `json:"type"`
		Timestamp int64  `json:"timestamp"`
	}{"connected", time.Now().UnixMilli()}, "")
	fmt.Fprintf(w, "data: %s\n\n", hello)
	flusher.Flush()

	ping := time.NewTicker(keepAlive)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case frame, ok := <-ch:
			if !ok {
				return
			}
			if _, err := w.Write(frame); err != nil {
				return
			}
		case <-ping.C:
			if _, err := io.WriteString(w, ":ping\n\n"); err != nil {
				return
			}
		}
		flusher.Flush()
	}
}

// jsNumber is Number(s) for the integer query parameters the API reads.
func jsNumber(s string) (int, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, nil
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil {
		return 0, fmt.Errorf("not a number: %q", s)
	}
	return int(f), nil
}

func param(q map[string][]string, key string) (string, bool) {
	v, ok := q[key]
	if !ok || len(v) == 0 {
		return "", false
	}
	return v[0], true
}

func numParam(q map[string][]string, key string, def int) (int, error) {
	v, ok := param(q, key)
	if !ok {
		return def, nil
	}
	return jsNumber(v)
}

var sessionPath = regexp.MustCompile(`^/api/sessions/(.+)$`)

func (s *Server) api(w http.ResponseWriter, r *http.Request, path string) error {
	q := r.URL.Query()
	switch path {
	case "/api/search":
		query, _ := param(q, "q")
		if query == "" {
			writeJSON(w, []any{})
			return nil
		}
		limit, err := numParam(q, "limit", 50)
		if err != nil {
			return err
		}
		project, _ := param(q, "project")
		from, _ := param(q, "from")
		to, _ := param(q, "to")
		results, err := s.DB.Search(query, db.SearchOptions{Project: project, Limit: &limit, From: from, To: to})
		if err != nil {
			return err
		}
		writeJSON(w, api.SearchHits(results, false))
		return nil

	case "/api/sessions":
		limit, err := numParam(q, "limit", 50)
		if err != nil {
			return err
		}
		offset, err := numParam(q, "offset", 0)
		if err != nil {
			return err
		}
		project, _ := param(q, "project")
		sessions, err := s.DB.ListSessions(db.ListOptions{Project: project, Limit: &limit, Offset: offset})
		if err != nil {
			return err
		}
		out, err := api.WebSessions(s.DB, sessions)
		if err != nil {
			return err
		}
		writeJSON(w, out)
		return nil

	case "/api/stats":
		project, _ := param(q, "project")
		st, err := s.DB.Stats(project)
		if err != nil {
			return err
		}
		writeJSON(w, api.WebStatsResult(st))
		return nil

	case "/api/image":
		session, _ := param(q, "session")
		message, _ := param(q, "message")
		index, err := numParam(q, "index", 0)
		if err != nil {
			return err
		}
		mediaType, data, ok, err := s.DB.GetImage(session, message, index)
		if err != nil {
			return err
		}
		if !ok {
			text(w, http.StatusNotFound, "Not Found")
			return nil
		}
		w.Header().Set("Content-Type", mediaType)
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(data)
		return nil

	case "/api/file":
		serveFile(w, q)
		return nil
	}

	if m := sessionPath.FindStringSubmatch(path); m != nil {
		sess, msgs, err := s.DB.ExportSession(m[1])
		if err != nil {
			return err
		}
		writeJSON(w, api.WebSessionDetailResult(sess, msgs))
		return nil
	}
	text(w, http.StatusNotFound, "Not Found")
	return nil
}

var fileTypes = map[string]string{
	"png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg",
	"gif": "image/gif", "webp": "image/webp", "svg": "image/svg+xml",
}

// serveFile serves a local image named in a transcript
// ([Image: source: /path/to/file]). Only image files are served: the UI
// needs nothing else, and any other file (an SSH key, a token) must not be
// readable through the server.
func serveFile(w http.ResponseWriter, q map[string][]string) {
	path, _ := param(q, "path")
	if path == "" || !strings.HasPrefix(path, "/") {
		text(w, http.StatusBadRequest, "Bad Request")
		return
	}
	typ, ok := fileTypes[strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))]
	if !ok {
		text(w, http.StatusForbidden, "Forbidden")
		return
	}
	// Resolve symlinks so a link named x.png cannot point at another file.
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		text(w, http.StatusNotFound, "Not Found")
		return
	}
	if _, ok := fileTypes[strings.ToLower(strings.TrimPrefix(filepath.Ext(real), "."))]; !ok {
		text(w, http.StatusForbidden, "Forbidden")
		return
	}
	data, err := os.ReadFile(real)
	if err != nil {
		text(w, http.StatusNotFound, "Not Found")
		return
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(data)
}

func serveAsset(w http.ResponseWriter, path string) {
	if b, ok := webui.Get(path); ok {
		w.Header().Set("Content-Type", webui.ContentType(path))
		w.Write(b)
		return
	}
	// Single-page app: any other path gets index.html.
	if b, ok := webui.Get("/index.html"); ok {
		w.Header().Set("Content-Type", webui.ContentType("/index.html"))
		w.Write(b)
		return
	}
	if !webui.Embedded {
		text(w, http.StatusNotFound, "Not Found (this binary was built without the web UI; build with `make go-build`)")
		return
	}
	text(w, http.StatusNotFound, "Not Found")
}
