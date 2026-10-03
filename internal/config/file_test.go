package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if got != Default() {
		t.Fatalf("got %+v", got)
	}
}

func TestLoadKeepsDefaultsForMissingKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[tui]\ndetail_position = \"auto\"\n"), 0o644)
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.TUI.DetailPosition != DetailAuto || got.TUI.DetailAutoWidth != 160 {
		t.Fatalf("got %+v", got)
	}
}

func TestLoadRejectsUnknownPosition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[tui]\ndetail_position = \"top\"\n"), 0o644)
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error")
	}
}

func TestLoadScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[tui]\nscope = \"all\"\n"), 0o644)
	if got, err := Load(path); err != nil || got.TUI.Scope != ScopeAll {
		t.Fatalf("got %+v, %v", got, err)
	}
	os.WriteFile(path, []byte("[tui]\nscope = \"repo\"\n"), 0o644)
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error for an unknown scope")
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cases := map[string]string{
		"scope = \"all\"\n":        `unknown key "scope"; it belongs under [tui]`,
		"[tui]\nscop = \"all\"\n":  `unknown key "tui.scop" (known: tui.detail_position`,
		"[ui]\ntheme = \"nord\"\n": `unknown key "ui"`,
	}
	for body, want := range cases {
		os.WriteFile(path, []byte(body), 0o644)
		_, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", body, err, want)
		}
	}
}

func TestTemplateLoadsAsTheDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.toml")
	if err := WriteTemplate(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || got != Default() {
		t.Fatalf("got %+v, %v", got, err)
	}
	// It never replaces a file that is there.
	os.WriteFile(path, []byte("[tui]\nscope = \"all\"\n"), 0o644)
	if err := WriteTemplate(path); err == nil {
		t.Fatal("expected an error for an existing file")
	}
	if got, _ := Load(path); got.TUI.Scope != ScopeAll {
		t.Fatal("the existing file was replaced")
	}
	// Every setting is in the template, and docs/tui.md shows the template.
	for _, k := range knownKeys() {
		if !strings.Contains(Template, "# "+strings.TrimPrefix(k, "tui.")+" = ") {
			t.Errorf("template lacks %s", k)
		}
	}
	doc, _ := os.ReadFile("../../docs/tui.md")
	if !strings.Contains(string(doc), Template) {
		t.Error("docs/tui.md should show the config template as written")
	}
}

func TestModelID(t *testing.T) {
	cases := map[string]string{
		"sonnet-5.5":        "claude-sonnet-5-5",
		"Opus-5.5":          "claude-opus-5-5",
		"haiku-4.5":         "claude-haiku-4-5",
		"fable-5.1":         "claude-fable-5-1",
		"claude-sonnet-5-5": "claude-sonnet-5-5",
		"sonnet":            "sonnet",
	}
	for in, want := range cases {
		if got := ModelID(in); got != want {
			t.Errorf("ModelID(%q) = %q, want %q", in, got, want)
		}
	}
}
