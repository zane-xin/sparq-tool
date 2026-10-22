package watch

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	WatchModeNotify = "notify"
	WatchModePoll   = "poll"
)

// WatchOptions 描述文件监听驱动、匹配规则和防抖时间。
type WatchOptions struct {
	Paths        []string
	Extensions   []string
	Files        []string
	Include      []string
	Exclude      []string
	ExcludeGlobs []string
	Mode         string
	PollInterval time.Duration
	Debounce     time.Duration
}

// Validate 校验监听目录、匹配规则和驱动参数。
func (o WatchOptions) Validate() error {
	if len(o.Paths) == 0 {
		return fmt.Errorf("至少需要一个监听目录")
	}
	if len(o.Include) == 0 && len(o.Extensions) == 0 && len(o.Files) == 0 {
		return fmt.Errorf("监听 include、extensions 和 files 不能同时为空")
	}
	if o.Debounce <= 0 {
		return fmt.Errorf("监听 debounce 必须大于 0")
	}
	mode := defaultWatchMode(o.Mode)
	if mode != WatchModeNotify && mode != WatchModePoll {
		return fmt.Errorf("监听 mode 只支持 %q 或 %q: %s", WatchModeNotify, WatchModePoll, o.Mode)
	}
	if mode == WatchModePoll && o.PollInterval <= 0 {
		return fmt.Errorf("poll 模式的 poll_interval 必须大于 0")
	}
	if _, err := newFileMatcher(o); err != nil {
		return err
	}
	for _, path := range o.Paths {
		linkInfo, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("读取监听目录 %s 失败: %w", path, err)
		}
		if linkInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("监听根目录不能是符号链接: %s", path)
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("读取监听目录 %s 失败: %w", path, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("监听路径不是目录: %s", path)
		}
	}
	return nil
}

type reloadWatcher interface {
	Changes() <-chan string
	Errors() <-chan error
	Close() error
}

func newReloadWatcher(options WatchOptions) (reloadWatcher, error) {
	if defaultWatchMode(options.Mode) == WatchModePoll {
		return newPollingWatcher(options)
	}
	return newSourceWatcher(options)
}

type sourceWatcher struct {
	watcher   *fsnotify.Watcher
	roots     []string
	matcher   fileMatcher
	exclude   []string
	changes   chan string
	errors    chan error
	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
}

func newSourceWatcher(options WatchOptions) (*sourceWatcher, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	matcher, err := newFileMatcher(options)
	if err != nil {
		return nil, err
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("创建文件监听器失败: %w", err)
	}
	source := &sourceWatcher{
		watcher: watcher,
		roots:   append([]string(nil), options.Paths...),
		matcher: matcher,
		exclude: cleanPaths(options.Exclude),
		changes: make(chan string, 1),
		errors:  make(chan error, 1),
		done:    make(chan struct{}),
	}
	sortRoots(source.roots)
	for _, root := range source.roots {
		if err := source.addTree(root, root); err != nil {
			_ = watcher.Close()
			return nil, fmt.Errorf("监听目录 %s 失败: %w", root, err)
		}
	}
	go source.run()
	return source, nil
}

func (w *sourceWatcher) Changes() <-chan string { return w.changes }
func (w *sourceWatcher) Errors() <-chan error   { return w.errors }

func (w *sourceWatcher) Close() error {
	w.closeOnce.Do(func() {
		close(w.done)
		w.closeErr = w.watcher.Close()
	})
	return w.closeErr
}

func (w *sourceWatcher) run() {
	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleEvent(event)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.reportError(fmt.Errorf("文件监听失败: %w", err))
		case <-w.done:
			return
		}
	}
}

func (w *sourceWatcher) handleEvent(event fsnotify.Event) {
	if event.Op&fsnotify.Create != 0 {
		if info, err := os.Lstat(event.Name); err == nil && info.IsDir() {
			if root := containingRoot(w.roots, event.Name); root != "" && !isExcludedPath(w.exclude, root, event.Name) {
				if err := w.addTree(root, event.Name); err != nil {
					w.reportError(fmt.Errorf("监听新建目录 %s 失败: %w", event.Name, err))
				} else if path := w.firstMatchingFile(root, event.Name); path != "" {
					w.reportChange(path)
				}
			}
			return
		}
	}
	if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
		return
	}
	root := containingRoot(w.roots, event.Name)
	if root == "" || isExcludedPath(w.exclude, root, event.Name) || !w.matcher.matches(root, event.Name) {
		return
	}
	w.reportChange(event.Name)
}

func (w *sourceWatcher) addTree(root, start string) error {
	return filepath.WalkDir(start, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if path != root && isExcludedPath(w.exclude, root, path) {
			return filepath.SkipDir
		}
		return w.watcher.Add(path)
	})
}

func (w *sourceWatcher) firstMatchingFile(root, start string) string {
	var match string
	_ = filepath.WalkDir(start, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if match != "" {
			return fs.SkipAll
		}
		if entry.IsDir() {
			if path != start && isExcludedPath(w.exclude, root, path) {
				return filepath.SkipDir
			}
			return nil
		}
		if w.matcher.matches(root, path) {
			match = path
		}
		return nil
	})
	return match
}

func (w *sourceWatcher) reportChange(path string) {
	select {
	case w.changes <- path:
	default:
	}
}

func (w *sourceWatcher) reportError(err error) {
	select {
	case w.errors <- err:
	default:
	}
}

func defaultWatchMode(mode string) string {
	if mode == "" {
		return WatchModeNotify
	}
	return mode
}

func sortRoots(roots []string) {
	sort.Slice(roots, func(left, right int) bool {
		return len(roots[left]) > len(roots[right])
	})
}

func containingRoot(roots []string, path string) string {
	path = filepath.Clean(path)
	for _, root := range roots {
		relative, err := filepath.Rel(root, path)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return root
		}
	}
	return ""
}

func isExcludedPath(exclude []string, root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." {
		return false
	}
	for _, excluded := range exclude {
		if relative == excluded || strings.HasPrefix(relative, excluded+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func cleanPaths(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, filepath.Clean(value))
		}
	}
	return result
}
