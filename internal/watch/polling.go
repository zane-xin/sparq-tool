package watch

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sync"
	"time"
)

type fileState struct {
	size    int64
	modTime int64
	mode    fs.FileMode
}

type pollingWatcher struct {
	roots     []string
	matcher   fileMatcher
	exclude   []string
	interval  time.Duration
	snapshot  map[string]fileState
	changes   chan string
	errors    chan error
	done      chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
}

func newPollingWatcher(options WatchOptions) (*pollingWatcher, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	matcher, err := newFileMatcher(options)
	if err != nil {
		return nil, err
	}
	watcher := &pollingWatcher{
		roots:    append([]string(nil), options.Paths...),
		matcher:  matcher,
		exclude:  cleanPaths(options.Exclude),
		interval: options.PollInterval,
		changes:  make(chan string, 1),
		errors:   make(chan error, 1),
		done:     make(chan struct{}),
		closed:   make(chan struct{}),
	}
	sortRoots(watcher.roots)
	watcher.snapshot, err = watcher.scan()
	if err != nil {
		return nil, err
	}
	go watcher.run()
	return watcher, nil
}

func (w *pollingWatcher) Changes() <-chan string { return w.changes }
func (w *pollingWatcher) Errors() <-chan error   { return w.errors }

func (w *pollingWatcher) Close() error {
	w.closeOnce.Do(func() {
		close(w.done)
		<-w.closed
	})
	return nil
}

func (w *pollingWatcher) run() {
	defer close(w.closed)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-w.done:
			return
		case <-ticker.C:
			next, err := w.scan()
			if err != nil {
				w.reportError(err)
				continue
			}
			if changed := changedFile(w.snapshot, next); changed != "" {
				w.reportChange(changed)
			}
			w.snapshot = next
		}
	}
}

func (w *pollingWatcher) scan() (map[string]fileState, error) {
	snapshot := make(map[string]fileState)
	for _, root := range w.roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if path != root && isExcludedPath(w.exclude, root, path) {
					return filepath.SkipDir
				}
				return nil
			}
			if !w.matcher.matches(root, path) {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			snapshot[path] = fileState{size: info.Size(), modTime: info.ModTime().UnixNano(), mode: info.Mode()}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("轮询监听目录 %s 失败: %w", root, err)
		}
	}
	return snapshot, nil
}

func changedFile(previous, next map[string]fileState) string {
	for path, state := range next {
		if old, exists := previous[path]; !exists || old != state {
			return path
		}
	}
	for path := range previous {
		if _, exists := next[path]; !exists {
			return path
		}
	}
	return ""
}

func (w *pollingWatcher) reportChange(path string) {
	select {
	case w.changes <- path:
	default:
	}
}

func (w *pollingWatcher) reportError(err error) {
	select {
	case w.errors <- err:
	default:
	}
}
