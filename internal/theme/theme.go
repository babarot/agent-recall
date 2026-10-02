// Package theme holds the TUI's color schemes. Each palette names colors by
// what the TUI uses them for, so a scheme only has to pick values.
package theme

import (
	"slices"
	"strings"
)

// Palette is one color scheme. Values are hex colors or ANSI color numbers.
type Palette struct {
	Surface  string // header bar and selected row background
	Border   string // separators and the detail pane frame
	Muted    string // help text, row count, metadata
	Subtle   string // dates, sizes, labels
	Strong   string // column headers, prompt text
	Text     string // selected row text
	Accent   string // app name, keys, the selection bar, IDs
	Title    string // session titles
	OK       string // confirmations such as "Copied"
	Prompt   string // the filter input and the user's messages
	Worktree string // the ⌥ worktree badge
	Warn     string // errors, a missing folder
	Dim      string // branches, removed worktrees
}

// Auto picks Dark or Light from the terminal background.
const Auto = "auto"

// The two schemes Auto chooses between.
const (
	Dark  = "catppuccin-mocha"
	Light = "catppuccin-latte"
)

var palettes = map[string]Palette{
	// cc360's slightly brightened Catppuccin Mocha.
	"catppuccin-mocha": {
		Surface: "#3b3b52", Border: "#525270", Muted: "#8888a4", Subtle: "#b0b8d1", Strong: "#d0d6e8",
		Text: "#e0e4f0", Accent: "#96bfff", Title: "#d4b0ff", OK: "#b5f0b0", Prompt: "#ffe5a0",
		Worktree: "#ffc49a", Warn: "#f5a3b5", Dim: "#706f87",
	},
	"catppuccin-latte": {
		Surface: "#ccd0da", Border: "#bcc0cc", Muted: "#8c8fa1", Subtle: "#6c6f85", Strong: "#5c5f77",
		Text: "#4c4f69", Accent: "#1e66f5", Title: "#8839ef", OK: "#40a02b", Prompt: "#df8e1d",
		Worktree: "#fe640b", Warn: "#d20f39", Dim: "#9ca0b0",
	},
	"tokyo-night": {
		Surface: "#292e42", Border: "#3b4261", Muted: "#737aa2", Subtle: "#a9b1d6", Strong: "#c0caf5",
		Text: "#c0caf5", Accent: "#7aa2f7", Title: "#bb9af7", OK: "#9ece6a", Prompt: "#e0af68",
		Worktree: "#ff9e64", Warn: "#f7768e", Dim: "#565f89",
	},
	"dracula": {
		Surface: "#44475a", Border: "#6272a4", Muted: "#6272a4", Subtle: "#bfbfbf", Strong: "#f8f8f2",
		Text: "#f8f8f2", Accent: "#8be9fd", Title: "#bd93f9", OK: "#50fa7b", Prompt: "#f1fa8c",
		Worktree: "#ffb86c", Warn: "#ff5555", Dim: "#6272a4",
	},
	"nord": {
		Surface: "#3b4252", Border: "#4c566a", Muted: "#7b88a1", Subtle: "#d8dee9", Strong: "#e5e9f0",
		Text: "#eceff4", Accent: "#88c0d0", Title: "#b48ead", OK: "#a3be8c", Prompt: "#ebcb8b",
		Worktree: "#d08770", Warn: "#bf616a", Dim: "#616e88",
	},
	"gruvbox-dark": {
		Surface: "#3c3836", Border: "#504945", Muted: "#928374", Subtle: "#bdae93", Strong: "#d5c4a1",
		Text: "#ebdbb2", Accent: "#83a598", Title: "#d3869b", OK: "#b8bb26", Prompt: "#fabd2f",
		Worktree: "#fe8019", Warn: "#fb4934", Dim: "#7c6f64",
	},
	// The terminal's own 16 colors, for a terminal theme the others clash with.
	"ansi": {
		Surface: "8", Border: "8", Muted: "8", Subtle: "7", Strong: "15",
		Text: "15", Accent: "12", Title: "13", OK: "10", Prompt: "11",
		Worktree: "3", Warn: "9", Dim: "8",
	},
}

// Names lists the scheme names a config may use, Auto first.
func Names() []string {
	names := make([]string, 0, len(palettes)+1)
	for n := range palettes {
		names = append(names, n)
	}
	slices.Sort(names)
	return append([]string{Auto}, names...)
}

// Valid reports whether name is a scheme or Auto.
func Valid(name string) bool { return slices.Contains(Names(), name) }

// NamesString is Names joined for an error message.
func NamesString() string { return strings.Join(Names(), ", ") }

// Get returns the palette for name. Auto, or an unknown name, picks Dark or
// Light by the terminal background.
func Get(name string, darkBackground bool) Palette {
	if p, ok := palettes[name]; ok {
		return p
	}
	if darkBackground {
		return palettes[Dark]
	}
	return palettes[Light]
}
