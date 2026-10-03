package config

import (
	"cmp"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2/unstable"
)

// A mistake in the config file is shown where it is, as linters show
// theirs: the file, line and column, what is wrong, and the line with the
// mistake marked.
//
//	~/.config/claude-recall/config.toml:14:18: "shift+y" is never read; write "Y"
//	   |
//	14 | read = ["enter", "shift+y"]
//	   |                  ^~~~~~~~~

// Problem is a mistake at a setting of the config file.
type Problem struct {
	// Key is the setting, as its table and key: ui, port; keys, list,
	// folders_open.
	Key []string
	// Index is which element of a list the mistake is in, or -1 for the
	// whole value.
	Index int
	// AtKey marks the key rather than its value, as for a key that is not
	// a setting.
	AtKey bool
	// Line and Col place the mistake when the file says where it is
	// itself, as for a file that does not parse; they win over Key.
	Line, Col int
	Message   string
}

// Problems are every mistake found in the config file. Its Error lists
// them without where they are; Report shows them in the file.
type Problems []Problem

func (ps Problems) Error() string {
	msgs := make([]string, len(ps))
	for i, p := range ps {
		msgs[i] = p.Message
	}
	return strings.Join(msgs, "\n")
}

// Report shows problems in the config file at path, each with the line it
// is on; the file is read again to find them.
func Report(path string, ps Problems) error {
	src, _ := os.ReadFile(path)
	return &report{path: path, src: src, problems: ps}
}

type report struct {
	path     string
	src      []byte
	problems Problems
}

func (r *report) Unwrap() error { return r.problems }

func (r *report) Error() string {
	shown := tildePath(r.path)
	lines := strings.Split(string(r.src), "\n")
	// Each where it is, top to bottom; one not in the file goes first.
	type placed struct {
		p                Problem
		line, col, width int
	}
	all := make([]placed, len(r.problems))
	for i, p := range r.problems {
		all[i] = placed{p: p, line: p.Line, col: p.Col, width: 1}
		if p.Line == 0 {
			all[i].line, all[i].col, all[i].width, _ = locate(r.src, p)
		}
	}
	slices.SortStableFunc(all, func(a, b placed) int { return cmp.Or(a.line-b.line, a.col-b.col) })
	var b strings.Builder
	for i, pl := range all {
		if i > 0 {
			b.WriteString("\n")
		}
		p, line, col, width := pl.p, pl.line, pl.col, pl.width
		if line == 0 {
			fmt.Fprintf(&b, "%s: %s\n", shown, p.Message)
			continue
		}
		fmt.Fprintf(&b, "%s:%d:%d: %s\n", shown, line, col, p.Message)
		if line > len(lines) {
			continue
		}
		num := fmt.Sprint(line)
		gutter := strings.Repeat(" ", len(num))
		text := strings.ReplaceAll(lines[line-1], "\t", " ")
		mark := "^" + strings.Repeat("~", max(0, width-1))
		fmt.Fprintf(&b, "%s |\n%s | %s\n%s | %s%s\n", gutter, num, text, gutter, strings.Repeat(" ", max(0, col-1)), mark)
	}
	return strings.TrimRight(b.String(), "\n")
}

// tildePath writes a path under the home directory with ~.
func tildePath(p string) string {
	if h := homeDir(); strings.HasPrefix(p, h+string(os.PathSeparator)) {
		return "~" + p[len(h):]
	}
	return p
}

// locate finds where p is in src: its line, column and width, marking the
// value (or one element of it) or the key. A setting that is not there is
// placed at its table's header when that is.
func locate(src []byte, p Problem) (line, col, width int, ok bool) {
	var parser unstable.Parser
	parser.Reset(src)
	var table []string
	// The parser reuses its nodes for the next expression, so a node is
	// read where it is found, never kept.
	var header struct{ line, col, width int }
	at := func(n *unstable.Node) (int, int, int, bool) {
		s := parser.Shape(n.Raw)
		return s.Start.Line, s.Start.Column, max(1, s.End.Column-s.Start.Column), true
	}
	keyOf := func(n *unstable.Node) (parts []string, last *unstable.Node) {
		for it := n.Key(); it.Next(); {
			last = it.Node()
			parts = append(parts, string(last.Data))
		}
		return parts, last
	}
	for parser.NextExpression() {
		e := parser.Expression()
		switch e.Kind {
		case unstable.Table:
			var last *unstable.Node
			table, last = keyOf(e)
			if len(table) <= len(p.Key) && slices.Equal(table, p.Key[:len(table)]) {
				header.line, header.col, header.width, _ = at(last)
			}
		case unstable.KeyValue:
			parts, keyNode := keyOf(e)
			if !slices.Equal(append(slices.Clone(table), parts...), p.Key) {
				continue
			}
			if p.AtKey {
				return at(keyNode)
			}
			v := e.Value()
			if v.Kind == unstable.Array && p.Index >= 0 {
				i := 0
				for it := v.Children(); it.Next(); i++ {
					if i == p.Index && it.Node().Raw.Length > 0 {
						return at(it.Node())
					}
				}
			}
			if v.Raw.Length > 0 {
				return at(v)
			}
			return at(keyNode)
		}
	}
	if header.line > 0 {
		return header.line, header.col, header.width, true
	}
	return 0, 0, 0, false
}
