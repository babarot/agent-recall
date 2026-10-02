// Package config holds the filesystem locations claude-recall reads and
// writes. They match the paths used by the TypeScript implementation so both
// can run against the same data during the migration.
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
