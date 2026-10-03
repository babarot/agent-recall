// Package config holds the filesystem locations claude-recall reads and
// writes, and the user's config file.
package config

import (
	"os"
	"path/filepath"
)

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return "/tmp"
}

// ClaudeDir is Claude Code's config directory: CLAUDE_CONFIG_DIR, as Claude
// Code itself reads it, or ~/.claude.
func ClaudeDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	return filepath.Join(homeDir(), ".claude")
}

// ProjectsDir holds Claude Code's JSONL transcripts, one directory per project.
func ProjectsDir() string { return filepath.Join(ClaudeDir(), "projects") }

// DefaultDBPath is the archive database. It stays in ~/.claude whatever
// CLAUDE_CONFIG_DIR says: following it would leave an archive that already
// exists behind and start an empty one, and db under [core] moves it.
func DefaultDBPath() string { return filepath.Join(homeDir(), ".claude", "vault.db") }
