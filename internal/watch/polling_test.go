package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPollingWatcherReportsCreatedAndRemovedFiles(t *testing.T) {
	root := t.TempDir()
	watcher, err := newPollingWatcher(WatchOptions{
		Paths: []string{root}, Include: []string{"**/*.go"}, Mode: WatchModePoll,
		PollInterval: 10 * time.Millisecond, Debounce: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()

	filename := filepath.Join(root, "main.go")
	if err := os.WriteFile(filename, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForWatcherPath(t, watcher, filename)
	if err := os.Remove(filename); err != nil {
		t.Fatal(err)
	}
	waitForWatcherPath(t, watcher, filename)
}

func waitForWatcherPath(t *testing.T, watcher reloadWatcher, expected string) {
	t.Helper()
	select {
	case path := <-watcher.Changes():
		if path != expected {
			t.Fatalf("change path = %q, want %q", path, expected)
		}
	case err := <-watcher.Errors():
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatalf("change %s was not reported", expected)
	}
}
