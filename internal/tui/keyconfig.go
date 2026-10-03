package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"

	"github.com/babarot/claude-recall/internal/config"
)

// The keys under [keys] in the config file replace operations' keys. A key
// given to an operation leaves the operations that have it by default and
// would meet it in one place: continue = "enter" takes enter from resume,
// as if enter were set to continue. Every mistake is reported rather than
// ignored, since a key that is silently never matched looks like a bug in
// the TUI: an unknown operation, a key written in a way no key press is
// ever read as, a fixed key, and two operations set in the file sharing a
// key in one place.

// fixedKeys cannot be given to an operation: ctrl+c quits from anywhere and
// esc steps back one level everywhere.
var fixedKeys = map[string]string{
	"ctrl+c": "quits from anywhere",
	"esc":    "steps back one level everywhere",
}

// namedKeys are the keys besides a single character, as key presses are
// read: bubbletea writes space as "space", not " ".
var namedKeys = map[string]bool{
	"enter": true, "space": true, "tab": true, "backspace": true, "esc": true,
	"up": true, "down": true, "left": true, "right": true,
	"home": true, "end": true, "pgup": true, "pgdown": true, "insert": true, "delete": true,
}

func init() {
	for i := 1; i <= 12; i++ {
		namedKeys[fmt.Sprintf("f%d", i)] = true
	}
}

// keyModifiers are the modifiers, in the order bubbletea writes them.
var keyModifiers = []string{"ctrl", "alt", "shift"}

// checkKey reports why a key written in the config file would never match
// a key press, with how to write it instead when there is one.
func checkKey(k string) error {
	if why, ok := fixedKeys[k]; ok {
		return fmt.Errorf("%s is fixed: it %s", k, why)
	}
	if k == "" || k == " " {
		return errors.New(`an empty key; write space as "space"`)
	}
	// Split off the modifiers; a + at the end is the + key (ctrl++).
	var mods []string
	base := k
	for {
		i := strings.Index(base, "+")
		if i <= 0 || i == len(base)-1 {
			break
		}
		mods, base = append(mods, base[:i]), base[i+1:]
	}
	if len(mods) == 0 && len(k) > 1 && strings.Contains(k, "-") {
		if alt := strings.ReplaceAll(k, "-", "+"); checkKey(alt) == nil {
			return fmt.Errorf("%q is not a key; write %q", k, alt)
		}
	}
	has := map[string]bool{}
	for _, m := range mods {
		if !slices.Contains(keyModifiers, m) || has[m] {
			return fmt.Errorf("%q is not a key: %q is not one of ctrl, alt, shift, each once", k, m)
		}
		has[m] = true
	}
	// bubbletea writes the modifiers in one order.
	var ordered []string
	for _, m := range keyModifiers {
		if has[m] {
			ordered = append(ordered, m)
		}
	}
	join := func(mods []string, base string) string { return strings.Join(append(slices.Clone(mods), base), "+") }
	if !namedKeys[base] {
		if lower := strings.ToLower(base); namedKeys[lower] {
			return fmt.Errorf("%q is not a key; write %q", k, join(ordered, lower))
		}
		r, size := utf8.DecodeRuneInString(base)
		if size != len(base) || !unicode.IsPrint(r) || unicode.IsSpace(r) {
			return fmt.Errorf("%q is not a key", k)
		}
		switch {
		case len(ordered) == 1 && has["shift"]:
			// A shifted character is read as the character: shift+y is Y.
			return fmt.Errorf("%q is never read: a shifted character arrives as itself; write %q", k, string(unicode.ToUpper(r)))
		case len(ordered) > 0 && unicode.IsUpper(r):
			// With ctrl or alt, a letter is read lower-cased, with shift+
			// for the shift key: ctrl+Y arrives as ctrl+shift+y.
			want := ordered
			if !has["shift"] {
				want = append(slices.Clone(ordered), "shift")
				slices.SortFunc(want, func(a, b string) int { return slices.Index(keyModifiers, a) - slices.Index(keyModifiers, b) })
			}
			return fmt.Errorf("%q is never read; write %q", k, join(want, string(unicode.ToLower(r))))
		}
	}
	if !slices.Equal(mods, ordered) {
		return fmt.Errorf("%q is never read; write %q", k, join(ordered, base))
	}
	return nil
}

// refs maps each operation's name to its keys, to set them.
func (k *keyMap) refs() map[string]*key.Binding {
	return map[string]*key.Binding{
		"quit": &k.Global.Quit, "help": &k.Global.Help, "focus_next": &k.Global.FocusNext, "focus_prev": &k.Global.FocusPrev,
		"ask": &k.Global.Ask, "sort": &k.Global.Sort, "scope": &k.Global.Scope,
		"resume": &k.Session.Resume, "continue": &k.Session.Continue, "read": &k.Session.Read, "copy_id": &k.Session.CopyID,
		"copy_command": &k.Session.CopyCommand, "grow": &k.Session.Grow, "shrink": &k.Session.Shrink,
		"up": &k.Nav.Up, "down": &k.Nav.Down, "page_up": &k.Nav.PageUp, "page_down": &k.Nav.PageDown,
		"top": &k.Nav.Top, "bottom": &k.Nav.Bottom, "search": &k.Nav.Search,
		"next_match": &k.Nav.NextMatch, "prev_match": &k.Nav.PrevMatch,
		"list.folders_open": &k.List.FoldersOpen, "list.folders_close": &k.List.FoldersClose, "folders.back": &k.Folders.FoldersBack,
	}
}

// keySetting is where operation name is set in the config file.
func keySetting(name string) []string { return append([]string{"keys"}, strings.Split(name, ".")...) }

// applyKeys returns the keymap with the operations in set given those keys,
// or every mistake in set, each where it is in the config file.
func applyKeys(base keyMap, set config.Keys) (keyMap, config.Problems) {
	k := base
	refs := k.refs()
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	slices.Sort(names)
	var ps config.Problems
	at := func(name string, index int, atKey bool, format string, args ...any) {
		ps = append(ps, config.Problem{Key: keySetting(name), Index: index, AtKey: atKey,
			Message: "keys." + name + ": " + fmt.Sprintf(format, args...)})
	}
	for _, name := range names {
		v := set[name]
		ref, ok := refs[name]
		switch {
		case !ok:
			at(name, -1, true, "%s", unknownOperation(name, refs))
			continue
		case v.Err != nil:
			at(name, -1, false, "%v", v.Err)
			continue
		}
		bad := false
		for i, kk := range v.Keys {
			if err := checkKey(kk); err != nil {
				at(name, i, false, "%v", err)
				bad = true
			}
		}
		if !bad {
			*ref = keys(v.Keys...)
		}
	}
	if len(ps) == 0 {
		// A key set in the file wins over a default it meets: the default
		// operation lets it go, everywhere, keeping its other keys.
		for _, c := range k.conflicts() {
			_, firstSet := set[c.first]
			_, secondSet := set[c.second]
			loser := ""
			switch {
			case c.first == "a fixed key" || firstSet == secondSet:
				continue
			case firstSet:
				loser = c.second
			default:
				loser = c.first
			}
			ref := refs[loser]
			*ref = keys(slices.DeleteFunc(slices.Clone(ref.Keys()), func(kk string) bool { return kk == c.key })...)
		}
		// What is left is a fixed key or two operations set in the file:
		// one problem per key and pair of operations, naming every place
		// they meet in.
		type pair struct{ key, first, second string }
		var order []pair
		places := map[pair][]string{}
		for _, c := range k.conflicts() {
			p := pair{c.key, c.first, c.second}
			if _, ok := places[p]; !ok {
				order = append(order, p)
			}
			places[p] = append(places[p], c.place)
		}
		for _, p := range order {
			// Point at an operation set in the file: the second, or the
			// one beside a fixed key.
			blame := p.second
			if _, ok := set[blame]; !ok {
				blame = p.first
			}
			index := -1
			if v, ok := set[blame]; ok {
				index = slices.Index(v.Keys, p.key)
			}
			c := conflict{p.key, p.first, p.second, strings.Join(places[p], " and ")}
			ps = append(ps, config.Problem{Key: keySetting(blame), Index: index, Message: "keys: " + c.String()})
		}
	}
	return k, ps
}

// unknownOperation explains a name under [keys] that is no operation,
// pointing one written in the wrong place to where it goes: a key that
// works anywhere goes directly under [keys], before any pane's table, and
// one that works in a pane goes in that pane's table.
func unknownOperation(name string, refs map[string]*key.Binding) string {
	op := name[strings.LastIndex(name, ".")+1:]
	var known []string
	for n := range refs {
		known = append(known, n)
		if n == op {
			return fmt.Sprintf("no such operation; %s goes directly under [keys], before [keys.list] and [keys.folders]", op)
		}
		if pane, o, ok := strings.Cut(n, "."); ok && o == op {
			return fmt.Sprintf("no such operation; %s goes under [keys.%s]", o, pane)
		}
	}
	slices.Sort(known)
	return "no such operation (known: " + strings.Join(known, ", ") + ")"
}

// WithKeys gives operations the keys set under [keys] in the config file,
// keeping the rest as they are. It reports every mistake in set, with the
// model unchanged.
func (m Model) WithKeys(set config.Keys) (Model, error) {
	k, ps := applyKeys(m.km, set)
	if len(ps) > 0 {
		return m, ps
	}
	m.km = k
	return m, nil
}
