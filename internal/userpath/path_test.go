package userpath

import (
	"path/filepath"
	"testing"
)

func TestRootUsesEnvironment(t *testing.T) {
	root := t.TempDir()
	t.Setenv(Environment, root)

	actual, err := Root()
	if err != nil {
		t.Fatalf("Root failed: %v", err)
	}
	if actual != root {
		t.Fatalf("Root = %q, want %q", actual, root)
	}
}

func TestRootUsesHiddenDirectoryInUserHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv(Environment, "")
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)

	actual, err := Root()
	if err != nil {
		t.Fatalf("Root failed: %v", err)
	}
	expected := filepath.Join(root, ".sparq")
	if actual != expected {
		t.Fatalf("Root = %q, want %q", actual, expected)
	}
}

func TestToolDirUsesSharedRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv(Environment, root)

	actual, err := ToolDir()
	if err != nil {
		t.Fatalf("ToolDir failed: %v", err)
	}
	expected := filepath.Join(root, "sparq-tool")
	if actual != expected {
		t.Fatalf("ToolDir = %q, want %q", actual, expected)
	}
}
