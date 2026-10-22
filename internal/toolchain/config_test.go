package toolchain

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUserUsesDefaultsWhenConfigIsMissing(t *testing.T) {
	t.Setenv("SPARQ_HOME", t.TempDir())
	config, path, err := LoadUser()
	if err != nil {
		t.Fatal(err)
	}
	if config.Go.Command != "go" || config.Frontend.Command != "pnpm" {
		t.Fatalf("defaults = %+v", config)
	}
	if path != filepath.Join(os.Getenv("SPARQ_HOME"), "sparq-tool", "config.yaml") {
		t.Fatalf("config path = %q", path)
	}
}

func TestLoadUserReadsToolchainOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SPARQ_HOME", home)
	path := filepath.Join(home, "sparq-tool", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("toolchain:\n  go:\n    command: go1.24\n    args: [-mod=vendor]\n  frontend:\n    command: corepack\n    args: [pnpm]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config, _, err := LoadUser()
	if err != nil {
		t.Fatal(err)
	}
	if config.Go.Command != "go1.24" || len(config.Go.Args) != 1 || config.Go.Args[0] != "-mod=vendor" {
		t.Fatalf("go config = %+v", config.Go)
	}
	if config.Frontend.Command != "corepack" || len(config.Frontend.Args) != 1 || config.Frontend.Args[0] != "pnpm" {
		t.Fatalf("frontend config = %+v", config.Frontend)
	}
}

func TestLoadUserRejectsUnknownFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SPARQ_HOME", home)
	path := filepath.Join(home, "sparq-tool", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("toolchain:\n  nope: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadUser(); err == nil {
		t.Fatal("expected unknown field error")
	}
}
