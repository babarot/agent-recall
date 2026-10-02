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

// ClaudeDir is Claude Code's config directory.
func ClaudeDir() string { return filepath.Join(homeDir(), ".claude") }

// ProjectsDir holds Claude Code's JSONL transcripts, one directory per project.
func ProjectsDir() string { return filepath.Join(ClaudeDir(), "projects") }

// DefaultDBPath is the archive database.
func DefaultDBPath() string { return filepath.Join(ClaudeDir(), "vault.db") }
