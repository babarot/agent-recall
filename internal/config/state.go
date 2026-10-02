package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// State is what the TUI remembers between runs, as opposed to File, which
// only the user edits.
type State struct {
	// DetailHeight is the detail pane height the user last chose, in lines.
	DetailHeight int `json:"detail_height,omitempty"`
}

// StatePath returns the state file, honoring XDG_STATE_HOME.
func StatePath() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		dir = filepath.Join(homeDir(), ".local", "state")
	}
	return filepath.Join(dir, "claude-recall", "state.json")
}

// LoadState reads the state file. A missing or unreadable file is an empty
// state: losing it only loses a remembered pane height.
func LoadState(path string) State {
	var s State
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

// SaveState writes the state file, replacing it atomically.
func SaveState(path string, s State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
