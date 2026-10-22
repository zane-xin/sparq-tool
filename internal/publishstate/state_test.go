package publishstate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateRoundTripUsesPrivatePermissions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SPARQ_HOME", home)
	source := filepath.Join(t.TempDir(), "plugin")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	state := State{
		Kind: "plugin", Endpoint: "https://cloud.example.test/", Profile: "work", SourceDir: source,
		OriginalVersion: "1.0.0", TargetVersion: "1.0.1", IdempotencyKey: "attempt:test",
	}
	if err := Save(state); err != nil {
		t.Fatal(err)
	}
	loaded, found, err := Load("plugin", "https://cloud.example.test", "work", source)
	if err != nil || !found || loaded.TargetVersion != "1.0.1" {
		t.Fatalf("Load() = %+v, %v, %v", loaded, found, err)
	}
	directory := filepath.Join(home, "sparq-tool", directoryName)
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatalf("ReadDir() = %d, %v", len(entries), err)
	}
	directoryInfo, err := os.Stat(directory)
	if err != nil || directoryInfo.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode = %v, %v", directoryInfo.Mode().Perm(), err)
	}
	fileInfo, err := os.Stat(filepath.Join(directory, entries[0].Name()))
	if err != nil || fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v, %v", fileInfo.Mode().Perm(), err)
	}
	if err := Delete("plugin", "https://cloud.example.test", "work", source); err != nil {
		t.Fatal(err)
	}
	if _, found, err := Load("plugin", "https://cloud.example.test", "work", source); err != nil || found {
		t.Fatalf("state remains after Delete(): found=%v err=%v", found, err)
	}
}

func TestNewAttemptKeyIsRandom(t *testing.T) {
	first, err := NewAttemptKey()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewAttemptKey()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || len(first) > 128 {
		t.Fatalf("attempt keys are not distinct: %q %q", first, second)
	}
}
