package watch

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSourceWatcherFiltersIncludedAndExcludedFiles(t *testing.T) {
	root := t.TempDir()
	storage := filepath.Join(root, "storage")
	if err := os.Mkdir(storage, 0o755); err != nil {
		t.Fatal(err)
	}
	watcher, err := newSourceWatcher(WatchOptions{
		Paths:      []string{root},
		Extensions: []string{".go"},
		Files:      []string{"go.mod"},
		Exclude:    []string{"storage"},
		Debounce:   10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()

	included := filepath.Join(root, "main.go")
	if err := os.WriteFile(included, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case path := <-watcher.Changes():
		if path != included {
			t.Fatalf("change path = %q, want %q", path, included)
		}
	case err := <-watcher.Errors():
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("included Go file change was not reported")
	}
	drainWatcherChanges(watcher, 50*time.Millisecond)

	if err := os.WriteFile(filepath.Join(storage, "ignored.go"), []byte("package ignored\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ignored.ts"), []byte("export {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case path := <-watcher.Changes():
		t.Fatalf("excluded change was reported: %s", path)
	case err := <-watcher.Errors():
		t.Fatal(err)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestSourceWatcherReportsAtomicFileReplacement(t *testing.T) {
	root := t.TempDir()
	watcher, err := newSourceWatcher(WatchOptions{
		Paths: []string{root}, Include: []string{"**/*.go"}, Debounce: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()

	target := filepath.Join(root, "main.go")
	if err := os.WriteFile(target, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForWatcherPath(t, watcher, target)
	drainWatcherChanges(watcher, 50*time.Millisecond)

	temporary := filepath.Join(root, ".main.go.swap")
	if err := os.WriteFile(temporary, []byte("package changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temporary, target); err != nil {
		t.Fatal(err)
	}
	waitForWatcherPath(t, watcher, target)
}

func TestWatchOptionsRejectsSymlinkRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlink creation requires environment-specific privileges")
	}
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "linked-project")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	err := (WatchOptions{
		Paths: []string{link}, Extensions: []string{".go"}, Debounce: time.Millisecond,
	}).Validate()
	if err == nil || !strings.Contains(err.Error(), "符号链接") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestSourceWatcherAddsNewDirectories(t *testing.T) {
	root := t.TempDir()
	watcher, err := newSourceWatcher(WatchOptions{
		Paths:      []string{root},
		Extensions: []string{".go"},
		Debounce:   10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()

	createdDir := filepath.Join(root, "internal")
	if err := os.Mkdir(createdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(createdDir, "service.go")
	if err := os.WriteFile(file, []byte("package internal\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case path := <-watcher.Changes():
		if path != file {
			t.Fatalf("change path = %q, want %q", path, file)
		}
	case err := <-watcher.Errors():
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("change in newly created directory was not reported")
	}
}

func drainWatcherChanges(watcher *sourceWatcher, quiet time.Duration) {
	timer := time.NewTimer(quiet)
	defer timer.Stop()
	for {
		select {
		case <-watcher.Changes():
			if !timer.Stop() {
				<-timer.C
			}
			timer.Reset(quiet)
		case <-timer.C:
			return
		}
	}
}
