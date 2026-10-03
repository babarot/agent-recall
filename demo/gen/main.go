// Command gen builds the demo that demo/demo.tape records: a home directory
// with a few git repositories and worktrees, Claude Code transcripts of the
// sessions in scenario.go, an archive imported from them, a config file,
// and the answer the stand-in claude (demo/bin/claude) gives to `a`.
//
// The demo lives in claude-recall-demo under the temporary directory, which
// it replaces: outside any git repository, so the TUI sees only the demo's,
// and by its real path, since the TUI resolves symlinks. demo/.out/env.sh
// points a shell at it. Run it from the repository root: go run ./demo/gen
package main

import (
	"crypto/sha1"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/babarot/claude-recall/internal/db"
	"github.com/babarot/claude-recall/internal/importer"
)

func main() {
	tmp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		log.Fatal(err)
	}
	out := flag.String("out", filepath.Join(tmp, "claude-recall-demo"), "directory to build the demo in")
	env := flag.String("env", "demo/.out/env.sh", "where to write the script that points a shell at the demo")
	flag.Parse()
	if err := build(*out, *env, time.Now()); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("demo built in %s: source %s\n", *out, *env)
}

func build(root, envPath string, now time.Time) error {
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	home := filepath.Join(root, "home")
	for _, repo := range []string{api, web, infra, cli, dots} {
		if err := gitRepo(home, repo); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(home, notes), 0o755); err != nil {
		return err
	}

	projects := filepath.Join(root, "claude", "projects")
	for _, s := range sessions {
		if err := writeTranscript(projects, home, s, now); err != nil {
			return err
		}
	}

	dbPath := filepath.Join(root, "demo.db")
	d, err := db.Open(dbPath, db.Options{})
	if err != nil {
		return err
	}
	err = importer.Run(d, importer.Options{ProjectsDir: projects}, os.Stdout)
	d.Close()
	if err != nil {
		return err
	}

	repoRoot, err := filepath.Abs(".")
	if err != nil {
		return err
	}
	files := map[string]string{
		"config/claude-recall/config.toml": fmt.Sprintf("[core]\ndb = %q\n\n[tui]\ntheme = \"catppuccin-mocha\"\nscope = \"all\"\n", dbPath),
		envPath: fmt.Sprintf(`# Sourced by demo/demo.tape: a shell that sees only the demo.
export RECALL_DEMO=%[1]q
export HOME="$RECALL_DEMO/home"
export XDG_CONFIG_HOME="$RECALL_DEMO/config"
export XDG_STATE_HOME="$RECALL_DEMO/state"
export CLAUDE_CONFIG_DIR="$RECALL_DEMO/claude"
export PATH=%[2]q:%[3]q:"$PATH"
export PS1='$ '
`, root, filepath.Join(repoRoot, "demo", "bin"), filepath.Join(repoRoot, "demo", ".out", "bin")),
		"ask.jsonl": askStream(),
	}
	for name, body := range files {
		p := name
		if !filepath.IsAbs(p) && name != envPath {
			p = filepath.Join(root, name)
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// gitRepo creates a repository with one commit, and its linked worktrees.
func gitRepo(home, repo string) error {
	dir := filepath.Join(home, repo)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# "+filepath.Base(repo)+"\n"), 0o644); err != nil {
		return err
	}
	steps := [][]string{{"init", "-q", "-b", "main"}, {"add", "."}, {"commit", "-q", "-m", "Initial commit"}}
	for _, wt := range worktrees[repo] {
		steps = append(steps, []string{"worktree", "add", "-q", "-b", wt.branch, filepath.Join(home, wt.path)})
	}
	for _, args := range steps {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=Demo", "-c", "user.email=demo@example.com",
			"-c", "commit.gpgsign=false"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s in %s: %v: %s", strings.Join(args, " "), repo, err, out)
		}
	}
	return nil
}

// sessionID is a stable UUID for a session, from its title.
func sessionID(s session) string {
	x := fmt.Sprintf("%x", sha1.Sum([]byte(s.title)))
	return x[0:8] + "-" + x[8:12] + "-4" + x[13:16] + "-8" + x[17:20] + "-" + x[20:32]
}

type obj = map[string]any

func writeTranscript(projects, home string, s session, now time.Time) error {
	id := sessionID(s)
	cwd := filepath.Join(home, s.dir)
	// Messages come in bursts with pauses between them, as real work does,
	// so the activity bars are not flat: gap(i) is the time before the i-th.
	gap := func(i int) time.Duration {
		switch {
		case i%37 == 36:
			return time.Duration(4+i%7) * time.Minute
		case i%11 == 10:
			return 90 * time.Second
		}
		return time.Duration(6+i*7%17) * time.Second
	}
	var total time.Duration
	for i := 1; i < s.msgs; i++ {
		total += gap(i)
	}
	t := now.Add(-s.ago).Add(-total)

	var lines []obj
	n := 0
	at := func() string {
		if n > 0 {
			t = t.Add(gap(n))
		}
		n++
		return t.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	user := func(content any) {
		lines = append(lines, obj{"type": "user", "uuid": fmt.Sprintf("%s-%d", id[:8], len(lines)), "sessionId": id,
			"timestamp": at(), "cwd": cwd, "gitBranch": s.branch, "version": "2.1.87",
			"message": obj{"role": "user", "content": content}})
	}
	assistant := func(blocks ...obj) {
		lines = append(lines, obj{"type": "assistant", "uuid": fmt.Sprintf("%s-%d", id[:8], len(lines)), "sessionId": id,
			"timestamp": at(), "cwd": cwd, "gitBranch": s.branch, "version": "2.1.87",
			"message": obj{"role": "assistant", "content": blocks}})
	}
	text := func(t string) obj { return obj{"type": "text", "text": t} }

	// Each thing the user says gets Claude's work and then its answer, as in
	// a real session: a short note before its first tool calls, the calls,
	// and the reply. The tool calls are spread over the turns so the session
	// reaches about msgs messages.
	turns := [][2]string{{s.prompt, s.reply}}
	for i := 0; i+1 < len(s.talk); i += 2 {
		turns = append(turns, [2]string{s.talk[i], s.talk[i+1]})
	}
	turns = append(turns, [2]string{s.last, s.answer})
	perTurn := max(1, (s.msgs-3*len(turns))/(2*len(turns)))
	step := 0
	for _, turn := range turns {
		user(turn[0])
		for i := range perTurn {
			tool, input := toolStep(s, cwd, step)
			toolID := fmt.Sprintf("toolu_%s_%d", id[:8], step)
			call := obj{"type": "tool_use", "id": toolID, "name": tool, "input": input}
			if i < 2 {
				assistant(text(narrate(tool, input, step)), call)
			} else {
				assistant(call)
			}
			user([]obj{{"type": "tool_result", "tool_use_id": toolID, "content": "ok"}})
			step++
		}
		assistant(text(turn[1]))
	}
	lines = append(lines, obj{"type": "ai-title", "aiTitle": s.title, "sessionId": id})

	var b strings.Builder
	for _, l := range lines {
		j, err := json.Marshal(l)
		if err != nil {
			return err
		}
		b.Write(j)
		b.WriteByte('\n')
	}
	// Claude Code names a project's directory after its path, / and . as -.
	dir := filepath.Join(projects, strings.NewReplacer("/", "-", ".", "-").Replace(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(b.String()), 0o644)
}

// toolStep is the step-th tool call of a session: reading and editing its
// files and running its commands, the first file and command most often.
func toolStep(s session, cwd string, step int) (string, obj) {
	pick := func(list []string) string {
		if step%3 == 0 || len(list) == 1 {
			return list[0]
		}
		return list[step%len(list)]
	}
	switch {
	case len(s.files) > 0 && step%4 == 1:
		return "Edit", obj{"file_path": filepath.Join(cwd, pick(s.files)), "old_string": "-", "new_string": "+"}
	case len(s.commands) > 0 && step%4 == 2:
		return "Bash", obj{"command": pick(s.commands)}
	case len(s.files) > 0:
		return "Read", obj{"file_path": filepath.Join(cwd, pick(s.files))}
	case len(s.commands) > 0:
		return "Bash", obj{"command": pick(s.commands)}
	}
	return "Grep", obj{"pattern": "retry", "path": cwd}
}

// narrate is the note Claude writes before a tool call.
func narrate(tool string, input obj, step int) string {
	pick := func(forms ...string) string { return forms[step%len(forms)] }
	switch tool {
	case "Read":
		f := filepath.Base(input["file_path"].(string))
		return fmt.Sprintf(pick("Let me look at %s first.", "Checking how %s handles this.", "Reading %s."), f)
	case "Edit":
		f := filepath.Base(input["file_path"].(string))
		return fmt.Sprintf(pick("Updating %s.", "Making the change in %s.", "Now the edit in %s."), f)
	case "Bash":
		return fmt.Sprintf(pick("Running `%s`.", "Let me check with `%s`.", "Now `%s` to confirm."), input["command"])
	}
	return "Searching for it."
}

// askStream is what the stand-in claude prints for `a`: two searches, then
// the two token refresh sessions with why each matches.
func askStream() string {
	find := func(title string) string {
		for _, s := range sessions {
			if s.title == title {
				return sessionID(s)[:8]
			}
		}
		panic("no session titled " + title)
	}
	events := []obj{
		{"type": "system", "subtype": "init"},
		{"type": "assistant", "message": obj{"content": []obj{{"type": "tool_use", "name": "mcp__recall__recall_search", "input": obj{"query": "logged out several requests at once"}}}}},
		{"type": "assistant", "message": obj{"content": []obj{{"type": "tool_use", "name": "mcp__recall__recall_search", "input": obj{"query": "token refresh concurrent"}}}}},
		{"type": "assistant", "message": obj{"content": []obj{{"type": "tool_use", "name": "StructuredOutput", "input": obj{}}}}},
		{"type": "result", "subtype": "success", "is_error": false, "total_cost_usd": 0.0412,
			"modelUsage": obj{"claude-sonnet-5-5": obj{"costUSD": 0.0412}},
			"structured_output": obj{"sessions": []obj{
				{"session_id": find("Fix the token refresh race on concurrent requests"), "why": "Found that concurrent refreshes revoked each other and fixed it with singleflight."},
				{"session_id": find("Write tests for token refresh under load"), "why": "Added a 50-goroutine test proving only one refresh reaches the provider."},
			}}},
	}
	var b strings.Builder
	for _, e := range events {
		j, _ := json.Marshal(e)
		b.Write(j)
		b.WriteByte('\n')
	}
	return b.String()
}
