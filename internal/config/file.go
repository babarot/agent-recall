package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/babarot/claude-recall/internal/theme"
)

// Detail pane positions in the TUI.
const (
	DetailBottom = "bottom"
	DetailRight  = "right"
	DetailAuto   = "auto" // right when the terminal is at least DetailAutoWidth wide
)

// Which sessions the TUI lists when it starts.
const (
	ScopeFolder = "folder" // the folder it was started in, when that has sessions
	ScopeAll    = "all"
)

// MinDetailHeight is the smallest detail pane, in lines, that still shows
// each of its three frames.
const MinDetailHeight = 10

// DefaultPort is where the web UI listens unless the config file or --port
// says otherwise.
const DefaultPort = 6276

// File is the user's config file, ~/.config/claude-recall/config.toml.
type File struct {
	Core Core `toml:"core"`
	UI   UI   `toml:"ui"`
	TUI  TUI  `toml:"tui"`
	// Keys changes which keys do what in the TUI, by operation name. The TUI
	// checks the names and keys when it starts, so a mistake here stops only
	// the TUI, as one under [tui] does.
	Keys map[string]KeyList `toml:"keys"`
}

// KeyList is an operation's keys as written under [keys]: a key or a list
// of keys. A value of another type is kept as an error for the TUI to
// report rather than one that stops every command.
type KeyList struct {
	Keys []string
	Err  error
}

// UnmarshalTOML takes a string or an array of strings.
func (k *KeyList) UnmarshalTOML(v any) error {
	switch v := v.(type) {
	case string:
		k.Keys = []string{v}
		return nil
	case []any:
		k.Keys = []string{}
		for _, e := range v {
			s, ok := e.(string)
			if !ok {
				k.Keys, k.Err = nil, errors.New("must be a key or a list of keys, as strings")
				return nil
			}
			k.Keys = append(k.Keys, s)
		}
		return nil
	}
	k.Err = errors.New("must be a key or a list of keys, as strings")
	return nil
}

// Core configures what every command uses.
type Core struct {
	// DB is the archive database: an absolute path or one starting with ~/.
	// Empty means DefaultDBPath.
	DB string `toml:"db"`
}

// UI configures the web UI.
type UI struct {
	// Port is where recall ui listens, and where ui stop and ui status look.
	Port int `toml:"port"`
}

// TUI configures the interactive session list.
type TUI struct {
	// DetailPosition is DetailBottom, DetailRight or DetailAuto.
	DetailPosition string `toml:"detail_position"`
	// DetailAutoWidth is the terminal width, in columns, at which "auto"
	// moves the detail pane to the right.
	DetailAutoWidth int `toml:"detail_auto_width"`
	// Theme is a color scheme name from theme.Names, or "auto" to follow
	// the terminal background.
	Theme string `toml:"theme"`
	// DetailHeight is the detail pane's height in lines when it sits below
	// the list, until it is resized with + / - or the mouse; the TUI then
	// remembers that height instead.
	DetailHeight int `toml:"detail_height"`
	// Scope is ScopeFolder to start with only the sessions of the folder
	// (repository and its worktrees) the TUI is started in, or ScopeAll.
	Scope string `toml:"scope"`
	// AskModel is the model `a` (ask Claude) runs claude -p with: a family
	// and version (sonnet-5.5), a full model ID, or an alias claude knows.
	AskModel string `toml:"ask_model"`
	// AskShowCost shows what an answer cost, as claude reports it.
	AskShowCost bool `toml:"ask_show_cost"`
	// AskReasons shows why Claude picked a session, under its row and in
	// Conversation, after an ask.
	AskReasons bool `toml:"ask_reasons"`
}

// Default returns the settings used when the config file is absent.
func Default() File {
	return File{UI: UI{Port: DefaultPort}, TUI: TUI{DetailPosition: DetailBottom, DetailAutoWidth: 160, Theme: theme.Auto, DetailHeight: 16, Scope: ScopeFolder,
		AskModel: "sonnet-5.5", AskShowCost: true, AskReasons: true}}
}

// DBPath is the archive database the file names, with ~/ expanded, or
// DefaultDBPath.
func (f File) DBPath() string {
	switch {
	case f.Core.DB == "":
		return DefaultDBPath()
	case strings.HasPrefix(f.Core.DB, "~/"):
		return filepath.Join(homeDir(), f.Core.DB[2:])
	}
	return f.Core.DB
}

// FilePath returns the config file location, honoring XDG_CONFIG_HOME.
func FilePath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		dir = filepath.Join(homeDir(), ".config")
	}
	return filepath.Join(dir, "claude-recall", "config.toml")
}

// Template is the config file written on first run: every setting at its
// default, commented out, so a default changed later still applies until
// the user picks a value.
const Template = `# claude-recall settings. Uncomment a line to change it.

[core]
# The archive database, for every command, the MCP server and the web UI,
# unless --db says otherwise: an absolute path or one starting with ~/.
# db = "~/.claude/vault.db"

[ui]
# Where the web UI (recall ui) listens, and where recall ui stop and
# recall ui status look for it, unless --port says otherwise.
# port = 6276

[tui]
# Where the detail pane goes: "bottom" (default), "right", or "auto" to put it
# on the right when the terminal is at least detail_auto_width columns wide.
# detail_position = "bottom"
# detail_auto_width = 160
# Initial height of the detail pane below the list, in lines (at least 10).
# detail_height = 16
# Color scheme: "auto" (default) picks catppuccin-mocha on a dark terminal and
# catppuccin-latte on a light one. Also: tokyo-night, dracula, nord,
# gruvbox-dark, and ansi (the terminal's own 16 colors).
# theme = "auto"
# Which sessions to start with: "folder" (default) for the repository recall is
# started in, when it has sessions, or "all".
# scope = "folder"
# a asks Claude Code (claude -p, on your Claude plan) to find sessions.
# The model: a family and version such as "sonnet-5.5", "opus-5.5" or
# "haiku-4.5", or a full model ID. Whether to show what an answer cost (the
# price claude reports; on a Claude plan it counts toward your usage rather
# than being billed), and why Claude picked each session.
# ask_model = "sonnet-5.5"
# ask_show_cost = true
# ask_reasons = true

[keys]
# Which keys do what in the TUI, by operation: a key or a list of keys,
# replacing the operation's own, or [] to turn it off. docs/tui.md lists
# the operations and their keys. For example, to resume with space and read
# the conversation with enter:
# resume = "space"
# read = "enter"
`

// WriteTemplate writes Template to path unless a file is already there.
func WriteTemplate(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(Template); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Load reads the config file at path for the TUI. A missing file yields the
// defaults; keys left out of the file keep their defaults. A key it does not
// know is an error, since it would otherwise be ignored without a word.
func Load(path string) (File, error) {
	cfg, err := LoadCore(path)
	if err != nil {
		return File{}, err
	}
	if err := cfg.TUI.check(path); err != nil {
		return File{}, err
	}
	return cfg, nil
}

// LoadCore reads the config file at path like Load, for the commands other
// than the TUI: a value under [tui] it does not check, so a mistake there
// does not stop the MCP server or an import.
func LoadCore(path string) (File, error) {
	cfg := Default()
	md, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Default(), nil
		}
		return File{}, fmt.Errorf("read %s: %w", path, err)
	}
	if keys := md.Undecoded(); len(keys) > 0 {
		return File{}, unknownKey(path, keys[0].String())
	}
	if len(cfg.Keys) == 0 {
		cfg.Keys = nil // an empty [keys] changes nothing
	}
	if db := cfg.Core.DB; db != "" && !strings.HasPrefix(db, "~/") && !filepath.IsAbs(db) {
		return File{}, fmt.Errorf("%s: core.db must be an absolute path or start with ~/, got %q", path, db)
	}
	if p := cfg.UI.Port; p < 1 || p > 65535 {
		return File{}, fmt.Errorf("%s: ui.port must be between 1 and 65535, got %d", path, p)
	}
	return cfg, nil
}

func (t TUI) check(path string) error {
	switch t.DetailPosition {
	case DetailBottom, DetailRight, DetailAuto:
	default:
		return fmt.Errorf("%s: tui.detail_position must be %q, %q or %q, got %q",
			path, DetailBottom, DetailRight, DetailAuto, t.DetailPosition)
	}
	if t.Scope != ScopeFolder && t.Scope != ScopeAll {
		return fmt.Errorf("%s: tui.scope must be %q or %q, got %q", path, ScopeFolder, ScopeAll, t.Scope)
	}
	if !theme.Valid(t.Theme) {
		return fmt.Errorf("%s: tui.theme must be one of %s, got %q", path, theme.NamesString(), t.Theme)
	}
	if t.DetailHeight < MinDetailHeight {
		return fmt.Errorf("%s: tui.detail_height must be at least %d", path, MinDetailHeight)
	}
	if strings.TrimSpace(t.AskModel) == "" {
		return fmt.Errorf("%s: tui.ask_model must not be empty", path)
	}
	if t.DetailAutoWidth <= 0 {
		return fmt.Errorf("%s: tui.detail_auto_width must be positive", path)
	}
	return nil
}

// unknownKey explains a key Load does not know, pointing a setting written
// in the wrong section to where it belongs.
func unknownKey(path, key string) error {
	name := key[strings.LastIndex(key, ".")+1:]
	for _, k := range knownKeys() {
		if section, n, _ := strings.Cut(k, "."); n == name {
			return fmt.Errorf("%s: unknown key %q; it belongs under [%s]", path, key, section)
		}
	}
	return fmt.Errorf("%s: unknown key %q (known: %s)", path, key, strings.Join(knownKeys(), ", "))
}

// knownKeys lists every setting as section.key.
func knownKeys() []string {
	var out []string
	f := reflect.TypeOf(File{})
	for i := range f.NumField() {
		section := f.Field(i).Tag.Get("toml")
		t := f.Field(i).Type
		if t.Kind() != reflect.Struct {
			continue // [keys], whose names the TUI knows
		}
		for j := range t.NumField() {
			out = append(out, section+"."+t.Field(j).Tag.Get("toml"))
		}
	}
	return out
}

var familyVersion = regexp.MustCompile(`^(sonnet|opus|haiku|fable)-(\d+)\.(\d+)$`)

// ModelID turns a family and version (sonnet-5.5) into the model ID claude
// takes (claude-sonnet-5-5); anything else is passed on as written.
func ModelID(name string) string {
	name = strings.TrimSpace(name)
	if m := familyVersion.FindStringSubmatch(strings.ToLower(name)); m != nil {
		return fmt.Sprintf("claude-%s-%s-%s", m[1], m[2], m[3])
	}
	return name
}
