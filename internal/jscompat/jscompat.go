// Package jscompat reproduces the JavaScript string and JSON behavior the
// TypeScript importer relied on, so the Go port stores byte-identical rows:
// String.prototype.trim, length and slice (which count UTF-16 code units),
// and JSON.stringify.
package jscompat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// isSpace reports whether r is WhiteSpace or a LineTerminator in ECMAScript.
// It differs from unicode.IsSpace: U+FEFF counts, U+0085 does not.
func isSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// Trim is String.prototype.trim.
func Trim(s string) string { return strings.TrimFunc(s, isSpace) }

// Len is String.prototype.length: the number of UTF-16 code units.
func Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// Slice is s.slice(0, n). When the cut falls inside a surrogate pair, the
// lone high surrogate becomes U+FFFD, which is what it turns into when V8
// writes the string to SQLite as UTF-8.
func Slice(s string, n int) string {
	if n <= 0 {
		return ""
	}
	units := 0
	for i, r := range s {
		w := 1
		if r >= 0x10000 {
			w = 2
		}
		if units+w > n {
			if units < n { // half of a surrogate pair fits
				return s[:i] + "�"
			}
			return s[:i]
		}
		units += w
	}
	return s
}

// Stringify is JSON.stringify(JSON.parse(raw)): compact output with
// JavaScript's key order, number formatting and string escaping.
func Stringify(raw []byte) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, err := decode(dec)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	encode(&b, v)
	return b.String(), nil
}

// Truthy reports whether a JSON value is truthy in JavaScript. An absent
// value (empty raw) is undefined, which is falsy.
func Truthy(raw []byte) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return false
	}
	switch raw[0] {
	case 'n', 'f': // null, false
		return false
	case '"':
		return !bytes.Equal(raw, []byte(`""`))
	case '{', '[', 't':
		return true
	}
	f, err := strconv.ParseFloat(string(raw), 64)
	return err != nil || (f != 0 && !math.IsNaN(f))
}

// object keeps JavaScript's property order for a parsed JSON object.
type object struct {
	keys []string
	vals map[string]any
}

func decode(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			o := &object{vals: map[string]any{}}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k := kt.(string)
				v, err := decode(dec)
				if err != nil {
					return nil, err
				}
				if _, dup := o.vals[k]; !dup {
					o.keys = append(o.keys, k)
				}
				o.vals[k] = v // a repeated key keeps its first position, last value
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			o.order()
			return o, nil
		case '[':
			var a []any
			for dec.More() {
				v, err := decode(dec)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return a, nil
		}
		return nil, fmt.Errorf("unexpected %v", t)
	default:
		return tok, nil // string, json.Number, bool, nil
	}
}

// arrayIndex reports whether k is a canonical array index ("0", "17", not
// "01"), which JavaScript orders before other keys, ascending.
func arrayIndex(k string) (uint32, bool) {
	if k == "" || len(k) > 10 || (len(k) > 1 && k[0] == '0') {
		return 0, false
	}
	n, err := strconv.ParseUint(k, 10, 32)
	if err != nil || n == math.MaxUint32 {
		return 0, false
	}
	return uint32(n), true
}

func (o *object) order() {
	var idx, rest []string
	for _, k := range o.keys {
		if _, ok := arrayIndex(k); ok {
			idx = append(idx, k)
		} else {
			rest = append(rest, k)
		}
	}
	slices.SortFunc(idx, func(a, b string) int {
		x, _ := arrayIndex(a)
		y, _ := arrayIndex(b)
		return int(int64(x) - int64(y))
	})
	o.keys = append(idx, rest...)
}

func encode(b *strings.Builder, v any) {
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(t))
	case json.Number:
		f, err := strconv.ParseFloat(string(t), 64)
		if err != nil && !math.IsInf(f, 0) {
			b.WriteString("null")
			return
		}
		b.WriteString(Number(f))
	case string:
		quote(b, t)
	case []any:
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			encode(b, e)
		}
		b.WriteByte(']')
	case *object:
		b.WriteByte('{')
		for i, k := range t.keys {
			if i > 0 {
				b.WriteByte(',')
			}
			quote(b, k)
			b.WriteByte(':')
			encode(b, t.vals[k])
		}
		b.WriteByte('}')
	}
}

func quote(b *strings.Builder, s string) {
	b.WriteByte('"')
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\f':
			b.WriteString(`\f`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20:
			fmt.Fprintf(b, `\u%04x`, r)
		default:
			b.WriteString(s[:size])
		}
		s = s[size:]
	}
	b.WriteByte('"')
}

// Number is Number.prototype.toString for a finite or infinite float64, as
// JSON.stringify writes it (infinities become null).
func Number(f float64) string {
	switch {
	case math.IsNaN(f) || math.IsInf(f, 0):
		return "null"
	case f == 0:
		return "0" // also -0
	}
	sign := ""
	if f < 0 {
		sign, f = "-", -f
	}
	// Shortest round-trip digits and exponent: f = 0.d1d2...dk × 10^n.
	e := strconv.FormatFloat(f, 'e', -1, 64)
	mant, exp, _ := strings.Cut(e, "e")
	digits := strings.Replace(mant, ".", "", 1)
	x, _ := strconv.Atoi(exp)
	k, n := len(digits), x+1

	switch {
	case k <= n && n <= 21:
		return sign + digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		return sign + digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		return sign + "0." + strings.Repeat("0", -n) + digits
	}
	es := "+"
	if n-1 < 0 {
		es = "-"
	}
	ev := strconv.Itoa(abs(n - 1))
	if k == 1 {
		return sign + digits + "e" + es + ev
	}
	return sign + digits[:1] + "." + digits[1:] + "e" + es + ev
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Marshal is JSON.stringify(v) (indent "") or JSON.stringify(v, null, 2)
// (indent "  ") for values encoding/json can encode. Unlike encoding/json,
// it leaves <, >, &, U+2028 and U+2029 unescaped, as JavaScript does.
func Marshal(v any, indent string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent != "" {
		enc.SetIndent("", indent)
	}
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return unescapeLineSeparators(bytes.TrimSuffix(buf.Bytes(), []byte("\n"))), nil
}

// unescapeLineSeparators turns encoding/json's   and   escapes back
// into the characters. It walks escapes pairwise so an escaped backslash
// followed by "u2028" text is left alone.
func unescapeLineSeparators(b []byte) []byte {
	if !bytes.Contains(b, []byte(`\u202`)) {
		return b
	}
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] != '\\' || i+1 >= len(b) {
			out = append(out, b[i])
			continue
		}
		if b[i+1] == 'u' && i+5 < len(b) && string(b[i+2:i+5]) == "202" && (b[i+5] == '8' || b[i+5] == '9') {
			if b[i+5] == '8' {
				out = append(out, " "...)
			} else {
				out = append(out, " "...)
			}
			i += 5
			continue
		}
		out = append(out, b[i], b[i+1])
		i++
	}
	return out
}

// ToFixed is Number.prototype.toFixed for non-negative x: halfway cases
// round up, judged on the exact binary value.
func ToFixed(x float64, digits int) string {
	f := new(big.Float).SetPrec(2048).SetFloat64(x)
	scale := new(big.Float).SetPrec(2048).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(digits)), nil))
	f.Mul(f, scale).Add(f, big.NewFloat(0.5))
	n, _ := f.Int(nil) // truncates toward zero: floor for x >= 0
	s := n.String()
	if digits == 0 {
		return s
	}
	if len(s) <= digits {
		s = strings.Repeat("0", digits-len(s)+1) + s
	}
	return s[:len(s)-digits] + "." + s[len(s)-digits:]
}

// PadEnd is s.padEnd(n) with spaces.
func PadEnd(s string, n int) string {
	if l := Len(s); l < n {
		return s + strings.Repeat(" ", n-l)
	}
	return s
}

// PadStart is s.padStart(n) with spaces.
func PadStart(s string, n int) string {
	if l := Len(s); l < n {
		return strings.Repeat(" ", n-l) + s
	}
	return s
}

// SliceFrom is s.slice(start) for start >= 0 or s.slice(-n) for a negative
// start, counting UTF-16 code units. A surrogate pair cut in half leaves
// U+FFFD in its place.
func SliceFrom(s string, start int) string {
	if start < 0 {
		start = max(0, Len(s)+start)
	}
	units := 0
	for i, r := range s {
		w := 1
		if r >= 0x10000 {
			w = 2
		}
		if units >= start {
			return s[i:]
		}
		if units+w > start { // start falls inside this pair
			return "�" + s[i+utf8.RuneLen(r):]
		}
		units += w
	}
	return ""
}
