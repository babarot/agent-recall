package config

import (
	"os"
	"path/filepath"
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
