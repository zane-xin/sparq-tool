package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckStandardWorkspace(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, filepath.Join(root, "go.work"), "use (\n\t./sparq-app\n\t./sparq-tool\n)\n")
	writeWorkspaceFile(t, filepath.Join(root, "sparq-app", "go.mod"), "module app\n")
	writeWorkspaceFile(t, filepath.Join(root, "sparq-app", "cmd", "sparq", "main.go"), "package main\n")
	if err := os.Mkdir(filepath.Join(root, "plugins"), 0o755); err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}
	result, err := Check(CheckOptions{Root: root})
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if result.HasFailure() {
		t.Fatalf("standard workspace should not fail: %#v", result.Items)
	}
}

func TestCheckReportsFailure(t *testing.T) {
	root := t.TempDir()
	result, err := Check(CheckOptions{Root: root})
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if !result.HasFailure() {
		t.Fatalf("workspace without files should fail")
	}
}

func writeWorkspaceFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}
