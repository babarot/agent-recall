package jscompat

import "testing"

// Expected values were produced by running JSON.stringify(JSON.parse(raw)),
// trim and slice in Deno.

func TestStringify(t *testing.T) {
	cases := []struct{ raw, want string }{
		{`{"b":1,"a":2,"10":3,"2":4,"01":5,"-1":6,"4294967295":7,"4294967294":8}`,
			`{"2":4,"10":3,"4294967294":8,"b":1,"a":2,"01":5,"-1":6,"4294967295":7}`},
		{`{"x":1.0,"y":1.50,"z":-0,"w":1e21,"v":1e-7,"u":123456789012345678901234,"t":0.000001,"s":1.5e300,"r":5e-324,"q":100,"p":-2.5e-10,"o":1e400}`,
			`{"x":1,"y":1.5,"z":0,"w":1e+21,"v":1e-7,"u":1.2345678901234569e+23,"t":0.000001,"s":1.5e+300,"r":5e-324,"q":100,"p":-2.5e-10,"o":null}`},
		{`{"s":"line\nbreak\ttab \u0001 \u001f \u007f \u2028 é 😀 \"q\" back\\slash \b\f\r / \/"}`,
			"{\"s\":\"line\\nbreak\\ttab \\u0001 \\u001f \u007f \u2028 é 😀 \\\"q\\\" back\\\\slash \\b\\f\\r / /\"}"},
		{`{"dup":1,"other":2,"dup":3}`, `{"dup":3,"other":2}`},
		{`[1,"a",null,true,false,{"k":[]},[]]`, `[1,"a",null,true,false,{"k":[]},[]]`},
		{`"just a string"`, `"just a string"`},
		{`{"nested":{"9":"a","1":"b","z":{"3":1,"y":2}}}`, `{"nested":{"1":"b","9":"a","z":{"3":1,"y":2}}}`},
		{` { "spaced" : [ 1 , 2 ] } `, `{"spaced":[1,2]}`},
	}
	for _, c := range cases {
		got, err := Stringify([]byte(c.raw))
		if err != nil {
			t.Errorf("Stringify(%s): %v", c.raw, err)
			continue
		}
		if got != c.want {
			t.Errorf("Stringify(%s)\n got %s\nwant %s", c.raw, got, c.want)
		}
	}
}

func TestTrim(t *testing.T) {
	cases := map[string]string{
		"\uFEFF hi \u0085": "hi \u0085", // BOM is whitespace, NEL is not
		"\u00a0x\u3000":    "x",
		"\u2028y\u2029":    "y",
		"\u200bz":          "\u200bz", // zero width space is not whitespace
	}
	for in, want := range cases {
		if got := Trim(in); got != want {
			t.Errorf("Trim(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLenAndSlice(t *testing.T) {
	s := "ab😀cd😀"
	if got := Len(s); got != 8 {
		t.Fatalf("Len = %d, want 8", got)
	}
	want := []string{"", "a", "ab", "ab�", "ab😀", "ab😀c", "ab😀cd", "ab😀cd�", "ab😀cd😀", "ab😀cd😀"}
	for n, w := range want {
		if got := Slice(s, n); got != w {
			t.Errorf("Slice(%d) = %q, want %q", n, got, w)
		}
	}
}

func TestTruthy(t *testing.T) {
	cases := map[string]bool{
		``: false, `null`: false, `false`: false, `0`: false, `-0`: false, `""`: false,
		`"x"`: true, `{}`: true, `[]`: true, `true`: true, `1`: true, `0.5`: true,
	}
	for raw, want := range cases {
		if got := Truthy([]byte(raw)); got != want {
			t.Errorf("Truthy(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestMarshal(t *testing.T) {
	got, err := Marshal(map[string]any{"s": "<a>& \\u2028\b"}, "")
	if err != nil {
		t.Fatal(err)
	}
	// JSON.stringify({s: "<a>& \\u2028\b"})
	if want := "{\"s\":\"<a>& \\\\u2028\\b\"}"; string(got) != want {
		t.Fatalf("got %s want %s", got, want)
	}
	got, _ = Marshal([]int{1}, "  ")
	if string(got) != "[\n  1\n]" {
		t.Fatalf("indent: %q", got)
	}
}

func TestToFixed(t *testing.T) {
	// (1.25).toFixed(1), (0.75).toFixed(1), (2.5).toFixed(0), (1.005).toFixed(2), (0.04).toFixed(1)
	cases := []struct {
		x    float64
		d    int
		want string
	}{{1.25, 1, "1.3"}, {0.75, 1, "0.8"}, {2.5, 0, "3"}, {1.005, 2, "1.00"}, {0.04, 1, "0.0"}, {1023.96, 1, "1024.0"}}
	for _, c := range cases {
		if got := ToFixed(c.x, c.d); got != c.want {
			t.Errorf("ToFixed(%v, %d) = %s, want %s", c.x, c.d, got, c.want)
		}
	}
}

func TestPadAndSliceFrom(t *testing.T) {
	if got := PadEnd("😀", 4); got != "😀  " {
		t.Errorf("PadEnd %q", got)
	}
	if got := PadStart("7", 3); got != "  7" {
		t.Errorf("PadStart %q", got)
	}
	s := "ab😀cd"
	for start, want := range map[int]string{-2: "cd", -3: "�cd", -4: "😀cd", -10: s, 0: s, 2: "😀cd", 3: "�cd"} {
		if got := SliceFrom(s, start); got != want {
			t.Errorf("SliceFrom(%d) = %q, want %q", start, got, want)
		}
	}
}
