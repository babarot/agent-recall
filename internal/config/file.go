package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
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

// File is the user's config file, ~/.config/claude-recall/config.toml.
type File struct {
	TUI TUI `toml:"tui"`
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
	// AskModel is the model `a` (ask Claude) runs claude -p with.
	AskModel string `toml:"ask_model"`
	// AskReasons shows why Claude picked a session, under its row and in
	// Conversation, after an ask.
	AskReasons bool `toml:"ask_reasons"`
}

// Default returns the settings used when the config file is absent.
func Default() File {
	return File{TUI: TUI{DetailPosition: DetailBottom, DetailAutoWidth: 160, Theme: theme.Auto, DetailHeight: 16, Scope: ScopeFolder,
		AskModel: "sonnet", AskReasons: true}}
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
# a asks Claude Code (claude -p, on your Claude plan) to find sessions: the
# model it uses, and whether to show why it picked each one.
# ask_model = "sonnet"
# ask_reasons = true
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

// Load reads the config file at path. A missing file yields the defaults;
// keys left out of the file keep their defaults. A key it does not know is
// an error, since it would otherwise be ignored without a word.
func Load(path string) (File, error) {
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
	switch cfg.TUI.DetailPosition {
	case DetailBottom, DetailRight, DetailAuto:
	default:
		return File{}, fmt.Errorf("%s: tui.detail_position must be %q, %q or %q, got %q",
			path, DetailBottom, DetailRight, DetailAuto, cfg.TUI.DetailPosition)
	}
	if cfg.TUI.Scope != ScopeFolder && cfg.TUI.Scope != ScopeAll {
		return File{}, fmt.Errorf("%s: tui.scope must be %q or %q, got %q", path, ScopeFolder, ScopeAll, cfg.TUI.Scope)
	}
	if !theme.Valid(cfg.TUI.Theme) {
		return File{}, fmt.Errorf("%s: tui.theme must be one of %s, got %q", path, theme.NamesString(), cfg.TUI.Theme)
	}
	if cfg.TUI.DetailHeight < MinDetailHeight {
		return File{}, fmt.Errorf("%s: tui.detail_height must be at least %d", path, MinDetailHeight)
	}
	if strings.TrimSpace(cfg.TUI.AskModel) == "" {
		return File{}, fmt.Errorf("%s: tui.ask_model must not be empty", path)
	}
	if cfg.TUI.DetailAutoWidth <= 0 {
		return File{}, fmt.Errorf("%s: tui.detail_auto_width must be positive", path)
	}
	return cfg, nil
}

// unknownKey explains a key Load does not know, pointing a TUI setting
// written outside [tui] to where it belongs.
func unknownKey(path, key string) error {
	t := reflect.TypeOf(TUI{})
	for i := range t.NumField() {
		if name := t.Field(i).Tag.Get("toml"); name == key {
			return fmt.Errorf("%s: unknown key %q; it belongs under [tui]", path, key)
		}
	}
	return fmt.Errorf("%s: unknown key %q (known: %s)", path, key, strings.Join(knownKeys(), ", "))
}

func knownKeys() []string {
	var out []string
	t := reflect.TypeOf(TUI{})
	for i := range t.NumField() {
		out = append(out, "tui."+t.Field(i).Tag.Get("toml"))
	}
	return out
}
