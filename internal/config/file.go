package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

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
}

// Default returns the settings used when the config file is absent.
func Default() File {
	return File{TUI: TUI{DetailPosition: DetailBottom, DetailAutoWidth: 160, Theme: theme.Auto, DetailHeight: 16, Scope: ScopeFolder}}
}

// FilePath returns the config file location, honoring XDG_CONFIG_HOME.
func FilePath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		dir = filepath.Join(homeDir(), ".config")
	}
	return filepath.Join(dir, "claude-recall", "config.toml")
}

// Load reads the config file at path. A missing file yields the defaults;
// keys left out of the file keep their defaults.
func Load(path string) (File, error) {
	cfg := Default()
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Default(), nil
		}
		return File{}, fmt.Errorf("read %s: %w", path, err)
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
	if cfg.TUI.DetailAutoWidth <= 0 {
		return File{}, fmt.Errorf("%s: tui.detail_auto_width must be positive", path)
	}
	return cfg, nil
}
