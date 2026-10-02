package tui

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Asking Claude runs Claude Code itself, `claude -p`, so it uses whatever
// the user is signed in with (a Claude plan included) and no API key of
// ours. It gets recall's MCP server and nothing else: no built-in tools, no
// other MCP servers, no session saved. Its steps stream back as it works,
// and its answer comes back as JSON in the shape askSchema asks for.

// askHit is a session Claude picked and why.
type askHit struct {
	id, why string
}

// askAnswer is what an ask found, what it cost as claude reports it (in US
// dollars), and the model that answered.
type askAnswer struct {
	hits  []askHit
	cost  float64
	model string
}

// askRunner asks question and returns what was found; progress hears about
// each step as it happens.
type askRunner func(ctx context.Context, question string, progress func(string)) (askAnswer, error)

const askSchema = `{"type":"object","properties":{"sessions":{"type":"array","items":{"type":"object","properties":{"session_id":{"type":"string"},"why":{"type":"string"}},"required":["session_id","why"]}}},"required":["sessions"]}`

const askInstructions = `You find sessions in the user's archive of past Claude Code sessions, using the recall tools: recall_search for full-text search and recall_list to list sessions. Do not export whole sessions. Search with several phrasings, in the language of the question and in English. Return the sessions that match the question, best first, at most 10, each with its session ID (the first 8 characters are enough) and one sentence, in the language of the question, on why it matches. Return an empty list when nothing matches.`

// claudeRunner asks with the claude CLI, giving it recall's MCP server as
// the command line recall (for example recall mcp --db PATH), from dir.
func claudeRunner(recall []string, model, dir string) askRunner {
	return func(ctx context.Context, question string, progress func(string)) (askAnswer, error) {
		bin, err := exec.LookPath("claude")
		if err != nil {
			return askAnswer{}, errors.New("claude (Claude Code) is not on PATH")
		}
		mcp, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{
			"recall": map[string]any{"command": recall[0], "args": recall[1:]},
		}})
		cmd := exec.CommandContext(ctx, bin, "-p", question,
			"--model", model,
			"--output-format", "stream-json", "--verbose",
			"--no-session-persistence",
			"--strict-mcp-config", "--mcp-config", string(mcp),
			"--tools", "",
			"--allowedTools", "mcp__recall__recall_search,mcp__recall__recall_list",
			"--json-schema", askSchema,
			"--append-system-prompt", askInstructions,
		)
		if dir != "" && os.MkdirAll(dir, 0o755) == nil {
			cmd.Dir = dir
		}
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.StdoutPipe()
		if err != nil {
			return askAnswer{}, err
		}
		if err := cmd.Start(); err != nil {
			return askAnswer{}, err
		}
		answer, readErr := readAskStream(out, progress)
		waitErr := cmd.Wait()
		if ctx.Err() != nil {
			return askAnswer{}, ctx.Err()
		}
		if readErr != nil {
			return askAnswer{}, readErr
		}
		if waitErr != nil {
			msg := strings.TrimSpace(stderr.String())
			if i := strings.LastIndex(msg, "\n"); i >= 0 {
				msg = msg[i+1:]
			}
			return askAnswer{}, fmt.Errorf("claude: %v %s", waitErr, msg)
		}
		return answer, nil
	}
}

// streamEvent is the part of a stream-json line that asking reads.
type streamEvent struct {
	Type       string                     `json:"type"`
	IsError    bool                       `json:"is_error"`
	Result     string                     `json:"result"`
	Cost       float64                    `json:"total_cost_usd"`
	ModelUsage map[string]json.RawMessage `json:"modelUsage"`
	Message    struct {
		Content []struct {
			Type  string          `json:"type"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	} `json:"message"`
	Structured *struct {
		Sessions []struct {
			ID  string `json:"session_id"`
			Why string `json:"why"`
		} `json:"sessions"`
	} `json:"structured_output"`
}

// readAskStream follows claude's stream-json output to its result: each
// recall call is reported to progress, and the structured answer returned.
func readAskStream(r io.Reader, progress func(string)) (askAnswer, error) {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var ev streamEvent
			if json.Unmarshal(line, &ev) == nil {
				switch ev.Type {
				case "assistant":
					for _, c := range ev.Message.Content {
						if c.Type == "tool_use" {
							if step := describeStep(c.Name, c.Input); step != "" {
								progress(step)
							}
						}
					}
				case "result":
					if ev.IsError {
						return askAnswer{}, fmt.Errorf("claude: %s", ev.Result)
					}
					if ev.Structured == nil {
						return askAnswer{}, errors.New("claude gave no structured answer")
					}
					a := askAnswer{cost: ev.Cost}
					for _, s := range ev.Structured.Sessions {
						a.hits = append(a.hits, askHit{strings.TrimSpace(s.ID), strings.TrimSpace(s.Why)})
					}
					// The model that cost the most, when several worked on it.
					top := -1.0
					for name, raw := range ev.ModelUsage {
						var u struct {
							Cost float64 `json:"costUSD"`
						}
						_ = json.Unmarshal(raw, &u)
						if u.Cost > top || u.Cost == top && name < a.model {
							a.model, top = name, u.Cost
						}
					}
					return a, nil
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				return askAnswer{}, errors.New("claude ended without an answer")
			}
			return askAnswer{}, err
		}
	}
}

// describeStep says what a recall call looks for.
func describeStep(name string, input json.RawMessage) string {
	var in map[string]any
	_ = json.Unmarshal(input, &in)
	tool := strings.TrimPrefix(name, "mcp__recall__")
	switch tool {
	case "recall_search":
		return fmt.Sprintf("search %q", in["query"])
	case "recall_list":
		var parts []string
		for _, k := range []string{"project", "limit"} {
			if v, ok := in[k]; ok {
				parts = append(parts, fmt.Sprintf("%s:%v", k, v))
			}
		}
		return strings.TrimSpace("list " + strings.Join(parts, " "))
	case "StructuredOutput":
		return ""
	}
	return tool
}
