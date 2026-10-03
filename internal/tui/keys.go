package tui

import (
	"regexp"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
)

// Keys are looked up in layers, top first, and a layer that takes a key
// stops it there: ctrl+c, then an open box (the key list, the sort menu,
// Ask), then a field being typed in, then the focused pane, then the keys
// that work anywhere. A pane takes only the keys it declares; anything else
// goes to the keys that work anywhere, never to another pane. See
// docs/design/keybindings.md.
//
// The keymap holds the keys of every operation a pane or the whole TUI
// offers. Each operation's keys are ordered: the key hints name them by
// position ({down.1} is j), so the order is part of how they show. esc,
// the keys inside boxes and the keys of a field being typed in are fixed
// and not in it.

// globalKeys work in any pane.
type globalKeys struct {
	Quit, Help, FocusNext, FocusPrev, Ask, Sort, Scope key.Binding
}

// sessionKeys act on the selected session, from the list, a frame or the
// spread conversation.
type sessionKeys struct {
	Resume, Read, CopyID, CopyCommand, Grow, Shrink key.Binding
}

// navKeys mean the same in every pane: move or scroll, search what has
// focus, go through its matches.
type navKeys struct {
	Up, Down, PageUp, PageDown, Top, Bottom, Search, NextMatch, PrevMatch key.Binding
}

// listKeys work on the session list only.
type listKeys struct {
	FoldersOpen, FoldersClose key.Binding
}

// folderKeys work in the folder list only.
type folderKeys struct {
	FoldersBack key.Binding
}

type keyMap struct {
	Global  globalKeys
	Session sessionKeys
	Nav     navKeys
	List    listKeys
	Folders folderKeys
}

func keys(k ...string) key.Binding { return key.NewBinding(key.WithKeys(k...)) }

func defaultKeyMap() keyMap {
	return keyMap{
		Global: globalKeys{
			Quit: keys("q"), Help: keys("?"), FocusNext: keys("tab", "]"), FocusPrev: keys("shift+tab", "["),
			Ask: keys("a"), Sort: keys("s"), Scope: keys("."),
		},
		Session: sessionKeys{
			Resume: keys("enter"), Read: keys("space"), CopyID: keys("y"), CopyCommand: keys("Y"),
			Grow: keys("+", "="), Shrink: keys("-"),
		},
		Nav: navKeys{
			Up: keys("up", "k", "ctrl+p"), Down: keys("down", "j", "ctrl+n"),
			PageUp: keys("pgup", "ctrl+b", "ctrl+u"), PageDown: keys("pgdown", "ctrl+f", "ctrl+d"),
			Top: keys("home", "g"), Bottom: keys("end", "G"),
			Search: keys("/"), NextMatch: keys("n"), PrevMatch: keys("N"),
		},
		List:    listKeys{FoldersOpen: keys("left", "h"), FoldersClose: keys("right", "l")},
		Folders: folderKeys{FoldersBack: keys("right", "l", "enter")},
	}
}

// byName maps each operation's name, as the key hints and the config file
// write it, to its keys.
func (k keyMap) byName() map[string]key.Binding {
	return map[string]key.Binding{
		"quit": k.Global.Quit, "help": k.Global.Help, "focus_next": k.Global.FocusNext, "focus_prev": k.Global.FocusPrev,
		"ask": k.Global.Ask, "sort": k.Global.Sort, "scope": k.Global.Scope,
		"resume": k.Session.Resume, "read": k.Session.Read, "copy_id": k.Session.CopyID,
		"copy_command": k.Session.CopyCommand, "grow": k.Session.Grow, "shrink": k.Session.Shrink,
		"up": k.Nav.Up, "down": k.Nav.Down, "page_up": k.Nav.PageUp, "page_down": k.Nav.PageDown,
		"top": k.Nav.Top, "bottom": k.Nav.Bottom, "search": k.Nav.Search,
		"next_match": k.Nav.NextMatch, "prev_match": k.Nav.PrevMatch,
		"folders_open": k.List.FoldersOpen, "folders_close": k.List.FoldersClose, "folders_back": k.Folders.FoldersBack,
	}
}

// keyContext is where a key that reaches the panes goes: which pane has
// focus, and for the Conversation frame whether it is spread.
type keyContext int

const (
	ctxList    keyContext = iota
	ctxFrame              // a detail frame: Conversation, What was done, Details
	ctxReading            // the Conversation spread over the pane
	ctxFolders
)

func (m Model) keyContext() keyContext {
	switch {
	case m.focus == focusFolders:
		return ctxFolders
	case m.focus == focusConv && m.expanded:
		return ctxReading
	case m.focus != focusList:
		return ctxFrame
	}
	return ctxList
}

// Key hints.

// keyGlyphs are how key names show in hints.
var keyGlyphs = map[string]string{
	"up": "↑", "down": "↓", "left": "←", "right": "→", "shift+tab": "⇧tab", "pgup": "PgUp", "pgdown": "PgDn",
}

func keyGlyph(k string) string {
	if g, ok := keyGlyphs[k]; ok {
		return g
	}
	return k
}

var keyRef = regexp.MustCompile(`\{([a-z_]+)\.(\d+)\}`)

// hintKeys fills in a key hint template: {op.N} is operation op's N-th key
// (from 0) and the rest is printed as written, so a template lays the keys
// out as the hint needs ("{up.0}{down.0}" is ↑↓, "{down.1} {up.1}" is j k).
// A reference to a key that is not there, as after a remap that gives the
// operation fewer keys, is left out with the separator before it (after
// it, when it comes first).
func (m Model) hintKeys(tmpl string) string {
	ops := m.km.byName()
	type part struct {
		text  string
		isRef bool
	}
	var parts []part
	last := 0
	for _, loc := range keyRef.FindAllStringSubmatchIndex(tmpl, -1) {
		parts = append(parts, part{text: tmpl[last:loc[0]]})
		name, n := tmpl[loc[2]:loc[3]], tmpl[loc[4]:loc[5]]
		i, _ := strconv.Atoi(n)
		text := ""
		if b, ok := ops[name]; ok && i < len(b.Keys()) {
			text = keyGlyph(b.Keys()[i])
		}
		parts = append(parts, part{text: text, isRef: true})
		last = loc[1]
	}
	parts = append(parts, part{text: tmpl[last:]})
	const seps = " /"
	for i, p := range parts {
		if !p.isRef || p.text != "" {
			continue
		}
		// parts alternate text and references, so the text either side of
		// a reference is parts[i-1] and parts[i+1].
		if before := parts[i-1].text; strings.TrimRight(before, seps) != before {
			parts[i-1].text = strings.TrimRight(before, seps)
		} else {
			parts[i+1].text = strings.TrimLeft(parts[i+1].text, seps)
		}
	}
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.text)
	}
	return b.String()
}

// Conflicts.

// namedKey is an operation's name and keys, for finding two operations
// that one key would reach.
type namedKey struct {
	name string
	b    key.Binding
}

func pick(all map[string]key.Binding, names ...string) []namedKey {
	out := make([]namedKey, len(names))
	for i, n := range names {
		out[i] = namedKey{n, all[n]}
	}
	return out
}

// keyScopes are the places a key is looked up in, each with every
// operation it can reach there and the fixed keys it also takes: two of
// them sharing a key in one place is a conflict.
func (k keyMap) keyScopes() []struct {
	name  string
	ops   []namedKey
	fixed []string
} {
	all := k.byName()
	global := []string{"quit", "help", "focus_next", "focus_prev", "ask", "sort", "scope"}
	session := []string{"resume", "read", "copy_id", "copy_command", "grow", "shrink"}
	nav := []string{"up", "down", "page_up", "page_down", "top", "bottom", "search", "next_match", "prev_match"}
	cat := func(lists ...[]string) []string {
		var out []string
		for _, l := range lists {
			out = append(out, l...)
		}
		return out
	}
	return []struct {
		name  string
		ops   []namedKey
		fixed []string
	}{
		{"the session list", pick(all, cat(global, session, nav, []string{"folders_open", "folders_close"})...), []string{"esc"}},
		{"a frame or the spread conversation", pick(all, cat(global, session, nav)...), []string{"esc"}},
		{"the folder list", pick(all, cat(global, nav, []string{"folders_back"})...), []string{"esc"}},
		// Boxes close on the key that opened them, beside their own keys.
		{"the key list", pick(all, "help"), []string{"esc", "q"}},
		{"the sort menu", pick(all, "sort"), []string{"down", "j", "ctrl+n", "tab", "up", "k", "ctrl+p", "shift+tab",
			"enter", "space", "esc", "q", "1", "2", "3", "4"}},
	}
}

// conflicts lists every key two operations, or an operation and a fixed
// key, share in one place.
func (k keyMap) conflicts() []string {
	var out []string
	for _, sc := range k.keyScopes() {
		owner := map[string]string{}
		for _, f := range sc.fixed {
			owner[f] = "a fixed key"
		}
		for _, op := range sc.ops {
			for _, kk := range op.b.Keys() {
				if prev, ok := owner[kk]; ok && prev != op.name {
					out = append(out, kk+" is both "+prev+" and "+op.name+" in "+sc.name)
					continue
				}
				owner[kk] = op.name
			}
		}
	}
	return out
}
