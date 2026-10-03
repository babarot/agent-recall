package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"

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

// Scrollbar thumbs in the TUI's detail frames, from thinnest to thickest.
const (
	ThumbThin  = "thin"  // │, told apart from the track only by its color
	ThumbHeavy = "heavy" // ┃
	ThumbBlock = "block" // █
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
	Keys Keys `toml:"keys"`
}

// Keys are the operations under [keys], by name. An operation that works
// in one pane only is written in that pane's table, as [keys.list] or
// [keys.folders], and named with it: list.folders_open, folders.back.
type Keys map[string]KeyList

// keysFrom reads [keys] as decoded, naming the operations in a pane's table
// after the table.
func keysFrom(t map[string]any) Keys {
	if len(t) == 0 {
		return nil // an empty [keys] changes nothing
	}
	k := Keys{}
	for name, val := range t {
		if pane, ok := val.(map[string]any); ok {
			for op, pv := range pane {
				k[name+"."+op] = keyListFrom(pv)
			}
			continue
		}
		k[name] = keyListFrom(val)
	}
	return k
}

// KeyList is an operation's keys as written under [keys]: a key or a list
// of keys. A value of another type is kept as an error for the TUI to
// report rather than one that stops every command.
type KeyList struct {
	Keys []string
	Err  error
}

// keyListFrom takes a string or an array of strings.
func keyListFrom(v any) KeyList {
	bad := KeyList{Err: errors.New("must be a key or a list of keys, as strings")}
	switch v := v.(type) {
	case string:
		return KeyList{Keys: []string{v}}
	case []any:
		keys := []string{}
		for _, e := range v {
			s, ok := e.(string)
			if !ok {
				return bad
			}
			keys = append(keys, s)
		}
		return KeyList{Keys: keys}
	}
	return bad
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
	// ScrollbarThumb is ThumbThin, ThumbHeavy or ThumbBlock.
	ScrollbarThumb string `toml:"scrollbar_thumb"`
	// ScrollbarColor colors the thumb: a hex color (#rrggbb) or an ANSI
	// color number (0-255). Empty uses the frame's border color.
	ScrollbarColor string `toml:"scrollbar_color"`
	// Images shows the images pasted into a session where they were
	// pasted, in the spread Conversation, through the Kitty graphics
	// protocol's Unicode placeholders.
	Images bool `toml:"images"`
}

// Default returns the settings used when the config file is absent.
func Default() File {
	return File{UI: UI{Port: DefaultPort}, TUI: TUI{DetailPosition: DetailBottom, DetailAutoWidth: 160, Theme: theme.Auto, DetailHeight: 16, Scope: ScopeFolder,
		AskModel: "sonnet-5.5", AskShowCost: true, AskReasons: true, ScrollbarThumb: ThumbHeavy}}
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
# The scrollbar on the right edge of a detail frame whose content scrolls. The
# thumb: "thin" (│), "heavy" (┃, default) or "block" (█). Its color: a hex
# color such as "#f5a3b5" or an ANSI color number (0-255); empty (default) is
# the frame's border color, so "thin" needs a color to stand out.
# scrollbar_thumb = "heavy"
# scrollbar_color = ""
# Show the images pasted into a session in the spread Conversation (Space),
# where they were pasted. Needs a terminal that draws Kitty graphics with
# Unicode placeholders, such as Ghostty or Kitty; elsewhere they come out as
# stray characters, so it is off by default.
# images = false

[keys]
# Which keys do what in the TUI, by operation: a key or a list of keys,
# replacing the operation's own, or [] to turn it off. Every operation is
# below with its keys; docs/tui.md says how keys are written. ctrl+c and
# esc are fixed. For example, to resume with space and read with enter,
# uncomment those two lines and swap their keys.
#
# Anywhere:
# quit = "q"
# help = "?"
# focus_next = ["tab", "]"]
# focus_prev = ["shift+tab", "["]
# ask = "a"
# sort = "s"
# scope = "."
#
# The selected session, from the list, a frame or the spread conversation:
# resume = "enter"
# read = "space"
# copy_id = "y"
# copy_command = "Y"
# grow = ["+", "="]
# shrink = "-"
#
# Moving and searching, in every pane:
# up = ["up", "k", "ctrl+p"]
# down = ["down", "j", "ctrl+n"]
# page_up = ["pgup", "ctrl+b", "ctrl+u"]
# page_down = ["pgdown", "ctrl+f", "ctrl+d"]
# top = ["home", "g"]
# bottom = ["end", "G"]
# search = "/"
# next_match = "n"
# prev_match = "N"
#
# Keys that work in one pane go in its table, after the lines above.
# The session list:
# [keys.list]
# folders_open = ["left", "h"]
# folders_close = ["right", "l"]
#
# The folder list:
# [keys.folders]
# back = ["right", "l", "enter"]
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
	if ps := cfg.TUI.check(); len(ps) > 0 {
		return File{}, Report(path, ps)
	}
	return cfg, nil
}

// LoadCore reads the config file at path like Load, for the commands other
// than the TUI: a value under [tui] it does not check, so a mistake there
// does not stop the MCP server or an import.
func LoadCore(path string) (File, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Default(), nil
		}
		return File{}, fmt.Errorf("read %s: %w", path, err)
	}
	cfg := Default()
	// [keys] is read as it is and checked by the TUI.
	doc := struct {
		Core Core           `toml:"core"`
		UI   UI             `toml:"ui"`
		TUI  TUI            `toml:"tui"`
		Keys map[string]any `toml:"keys"`
	}{Core: cfg.Core, UI: cfg.UI, TUI: cfg.TUI}
	if err := toml.NewDecoder(bytes.NewReader(src)).DisallowUnknownFields().Decode(&doc); err != nil {
		return File{}, Report(path, decodeProblems(err))
	}
	cfg = File{Core: doc.Core, UI: doc.UI, TUI: doc.TUI, Keys: keysFrom(doc.Keys)}
	var ps Problems
	if db := cfg.Core.DB; db != "" && !strings.HasPrefix(db, "~/") && !filepath.IsAbs(db) {
		ps = append(ps, problem("core.db", "core.db must be an absolute path or start with ~/, got %q", db))
	}
	if p := cfg.UI.Port; p < 1 || p > 65535 {
		ps = append(ps, problem("ui.port", "ui.port must be between 1 and 65535, got %d", p))
	}
	if len(ps) > 0 {
		return File{}, Report(path, ps)
	}
	return cfg, nil
}

// problem is a mistake in the value of setting (section.key).
func problem(setting, format string, args ...any) Problem {
	return Problem{Key: strings.Split(setting, "."), Index: -1, Message: fmt.Sprintf(format, args...)}
}

// decodeProblems turns what the decoder found wrong into problems: keys
// that are no setting, each pointed to where it belongs when it is in the
// wrong section, or a file that does not parse or has a value of the wrong
// type, where the decoder says it is.
func decodeProblems(err error) Problems {
	var strict *toml.StrictMissingError
	if errors.As(err, &strict) {
		var ps Problems
		for _, e := range strict.Errors {
			key := []string(e.Key())
			ps = append(ps, Problem{Key: key, Index: -1, AtKey: true, Message: unknownKey(strings.Join(key, "."))})
		}
		return ps
	}
	var de *toml.DecodeError
	if errors.As(err, &de) {
		msg := strings.TrimPrefix(de.Error(), "toml: ")
		if m := wrongType.FindStringSubmatch(msg); m != nil && len(de.Key()) > 0 {
			setting := strings.Join(de.Key(), ".")
			return Problems{problem(setting, "%s must be %s, got %s", setting, goTypeWords(m[2]), tomlTypeWords(m[1]))}
		}
		line, col := de.Position()
		return Problems{{Line: line, Col: col, Index: -1, Message: msg}}
	}
	return Problems{{Index: -1, Message: err.Error()}}
}

// wrongType is how the decoder says a value has the wrong type.
var wrongType = regexp.MustCompile(`^cannot decode TOML (\w+) into struct field \S+ of type (\S+)$`)

// goTypeWords says what a setting's Go type takes.
func goTypeWords(t string) string {
	switch {
	case t == "bool":
		return "true or false"
	case t == "string":
		return "a string"
	case strings.HasPrefix(t, "int"), strings.HasPrefix(t, "uint"), strings.HasPrefix(t, "float"):
		return "a number"
	}
	return "a " + t
}

// tomlTypeWords names the type of a TOML value as the decoder reports it.
func tomlTypeWords(t string) string {
	switch t {
	case "string":
		return "a string"
	case "integer", "float":
		return "a number"
	case "boolean", "bool":
		return "true or false"
	case "array":
		return "a list"
	}
	return "a " + t
}

func (t TUI) check() Problems {
	var ps Problems
	add := func(setting, format string, args ...any) { ps = append(ps, problem(setting, format, args...)) }
	switch t.DetailPosition {
	case DetailBottom, DetailRight, DetailAuto:
	default:
		add("tui.detail_position", "tui.detail_position must be %q, %q or %q, got %q", DetailBottom, DetailRight, DetailAuto, t.DetailPosition)
	}
	if t.Scope != ScopeFolder && t.Scope != ScopeAll {
		add("tui.scope", "tui.scope must be %q or %q, got %q", ScopeFolder, ScopeAll, t.Scope)
	}
	if !theme.Valid(t.Theme) {
		add("tui.theme", "tui.theme must be one of %s, got %q", theme.NamesString(), t.Theme)
	}
	if t.DetailHeight < MinDetailHeight {
		add("tui.detail_height", "tui.detail_height must be at least %d", MinDetailHeight)
	}
	if strings.TrimSpace(t.AskModel) == "" {
		add("tui.ask_model", "tui.ask_model must not be empty")
	}
	if t.DetailAutoWidth <= 0 {
		add("tui.detail_auto_width", "tui.detail_auto_width must be positive")
	}
	switch t.ScrollbarThumb {
	case ThumbThin, ThumbHeavy, ThumbBlock:
	default:
		add("tui.scrollbar_thumb", "tui.scrollbar_thumb must be %q, %q or %q, got %q", ThumbThin, ThumbHeavy, ThumbBlock, t.ScrollbarThumb)
	}
	if c := t.ScrollbarColor; c != "" && !hexColor.MatchString(c) && !ansiColor(c) {
		add("tui.scrollbar_color", "tui.scrollbar_color must be a hex color such as \"#f5a3b5\" or an ANSI color number (0-255), got %q", c)
	}
	return ps
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func ansiColor(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 0 && n <= 255 && strconv.Itoa(n) == s
}

// unknownKey explains a key Load does not know, pointing a setting written
// in the wrong section to where it belongs.
func unknownKey(key string) string {
	if !strings.Contains(key, ".") && !slices.ContainsFunc(knownKeys(), func(k string) bool { return strings.HasSuffix(k, "."+key) }) {
		return fmt.Sprintf("unknown table [%s] (known: [core], [ui], [tui], [keys])", key)
	}
	name := key[strings.LastIndex(key, ".")+1:]
	for _, k := range knownKeys() {
		if section, n, _ := strings.Cut(k, "."); n == name {
			return fmt.Sprintf("unknown key %q; it belongs under [%s]", key, section)
		}
	}
	return fmt.Sprintf("unknown key %q (known: %s)", key, strings.Join(knownKeys(), ", "))
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
