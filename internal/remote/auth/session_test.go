package auth

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zane-xin/sparq-tool/internal/userpath"
)

func TestSaveResolveAndRemoveProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "auth.json")
	first := Session{Endpoint: "https://cloud.example.com", AccessToken: "first", Account: Account{ID: 1}}
	second := Session{Endpoint: "https://staging.example.com", AccessToken: "second", Account: Account{ID: 2}}
	if _, err := SaveSession(path, "production", first); err != nil {
		t.Fatalf("SaveSession production failed: %v", err)
	}
	if _, err := SaveSession(path, "staging", second); err != nil {
		t.Fatalf("SaveSession staging failed: %v", err)
	}

	resolved, profile, loadedPath, err := ResolveSession(path, "")
	if err != nil {
		t.Fatalf("ResolveSession failed: %v", err)
	}
	if loadedPath != path || profile != "staging" || resolved.AccessToken != "second" {
		t.Fatalf("unexpected resolved session: %+v %s %s", resolved, profile, loadedPath)
	}
	if mode := fileMode(t, path); mode != 0o600 {
		t.Fatalf("config mode = %o, want 600", mode)
	}
	if mode := fileMode(t, filepath.Dir(path)); mode != 0o700 {
		t.Fatalf("config directory mode = %o, want 700", mode)
	}

	removed, _, err := RemoveSession(path, "staging")
	if err != nil || removed != "staging" {
		t.Fatalf("RemoveSession staging = %q, %v", removed, err)
	}
	resolved, profile, _, err = ResolveSession(path, "")
	if err != nil || profile != "production" || resolved.AccessToken != "first" {
		t.Fatalf("unexpected fallback profile: %+v %s %v", resolved, profile, err)
	}
	if _, _, err := RemoveSession(path, "production"); err != nil {
		t.Fatalf("RemoveSession production failed: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("auth config should be removed, stat error = %v", err)
	}
}

func TestSaveSessionPreservesInvalidExistingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveSession(path, DefaultProfile, Session{Endpoint: "https://cloud.example.com", AccessToken: "token"}); err == nil {
		t.Fatal("SaveSession should not overwrite invalid config")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "invalid" {
		t.Fatalf("existing config changed: %q, %v", data, err)
	}
}

func TestSaveSessionDoesNotChangeExistingParentPermissions(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "auth.json")
	if _, err := SaveSession(path, DefaultProfile, Session{Endpoint: "https://cloud.example.com", AccessToken: "token"}); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}
	if mode := fileMode(t, directory); mode != 0o755 {
		t.Fatalf("existing config directory mode = %o, want 755", mode)
	}
	if mode := fileMode(t, path); mode != 0o600 {
		t.Fatalf("config mode = %o, want 600", mode)
	}
}

func TestResolveSessionRejectsUnknownProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if _, err := SaveSession(path, DefaultProfile, Session{Endpoint: "https://cloud.example.com", AccessToken: "token"}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := ResolveSession(path, "missing"); err == nil {
		t.Fatal("ResolveSession should reject unknown profile")
	}
}

func TestResolveConfigPathUsesSparqHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv(userpath.Environment, root)

	path, err := resolveConfigPath("")
	if err != nil {
		t.Fatalf("resolveConfigPath failed: %v", err)
	}
	expected := filepath.Join(root, "sparq-tool", "auth.json")
	if path != expected {
		t.Fatalf("config path = %q, want %q", path, expected)
	}
}

func TestResolveConfigPathPrefersExplicitPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv(userpath.Environment, filepath.Join(root, "home"))
	explicit := filepath.Join(root, "custom", "auth.json")

	path, err := resolveConfigPath(explicit)
	if err != nil {
		t.Fatalf("resolveConfigPath failed: %v", err)
	}
	if path != explicit {
		t.Fatalf("config path = %q, want %q", path, explicit)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
