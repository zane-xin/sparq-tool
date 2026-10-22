package checksum

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateWriteAndVerify(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "a.txt"), "hello")
	writeTestFile(t, filepath.Join(root, "nested", "b.txt"), "world")
	entries, err := Create([]string{root})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entry count = %d, want 2", len(entries))
	}
	output := filepath.Join(root, "checksums.txt")
	if err := Write(output, entries); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := Verify(output); err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
}

func TestVerifyDetectsMismatch(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "a.txt")
	writeTestFile(t, target, "hello")
	entries, err := Create([]string{target})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	output := filepath.Join(root, "checksums.txt")
	if err := Write(output, entries); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	writeTestFile(t, target, "changed")
	if err := Verify(output); err == nil {
		t.Fatalf("Verify should fail after file change")
	}
}

func TestRelativeToMakesEntriesPortable(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "a.txt")
	writeTestFile(t, target, "hello")
	entries, err := Create([]string{target})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	entries, err = RelativeTo(root, entries)
	if err != nil {
		t.Fatalf("RelativeTo failed: %v", err)
	}
	if entries[0].Path != "a.txt" {
		t.Fatalf("entry path = %s, want a.txt", entries[0].Path)
	}
	output := filepath.Join(root, "checksums.txt")
	if err := Write(output, entries); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := Verify(output); err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
}

func writeTestFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}
