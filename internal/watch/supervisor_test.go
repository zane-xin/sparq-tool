package watch

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSupervisorKeepsCurrentProcessWhenBuildFails(t *testing.T) {
	watcher := newFakeReloadWatcher()
	lifecycle := &fakeProcessLifecycle{buildResults: []error{nil, errors.New("compile failed")}}
	supervisor, err := startSupervisor(context.Background(), testOptions(io.Discard), lifecycle, watcher)
	if err != nil {
		t.Fatal(err)
	}
	watcher.changes <- "main.go"
	waitForLifecycleCount(t, lifecycle, func(state lifecycleState) bool { return state.builds == 2 })
	state := lifecycle.state()
	if state.starts != 1 || state.stops != 0 || state.ready != 1 {
		t.Fatalf("lifecycle after failed build = %+v", state)
	}
	if err := supervisor.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSupervisorCanSkipOnlyInitialBuild(t *testing.T) {
	watcher := newFakeReloadWatcher()
	lifecycle := &fakeProcessLifecycle{buildResults: []error{nil}}
	options := testOptions(io.Discard)
	options.SkipInitialBuild = true
	supervisor, err := startSupervisor(context.Background(), options, lifecycle, watcher)
	if err != nil {
		t.Fatal(err)
	}
	if state := lifecycle.state(); state.builds != 0 || state.starts != 1 || state.ready != 1 {
		t.Fatalf("initial lifecycle = %+v", state)
	}
	watcher.changes <- "main.go"
	waitForLifecycleCount(t, lifecycle, func(state lifecycleState) bool {
		return state.builds == 1 && state.starts == 2 && state.ready == 2
	})
	if err := supervisor.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSupervisorReplacesProcessAfterSuccessfulBuild(t *testing.T) {
	watcher := newFakeReloadWatcher()
	lifecycle := &fakeProcessLifecycle{buildResults: []error{nil, nil}}
	supervisor, err := startSupervisor(context.Background(), testOptions(io.Discard), lifecycle, watcher)
	if err != nil {
		t.Fatal(err)
	}
	watcher.changes <- "main.go"
	waitForLifecycleCount(t, lifecycle, func(state lifecycleState) bool {
		return state.builds == 2 && state.starts == 2 && state.stops == 1 && state.ready == 2
	})
	if err := supervisor.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSupervisorRestartsAfterProcessExits(t *testing.T) {
	watcher := newFakeReloadWatcher()
	lifecycle := &fakeProcessLifecycle{buildResults: []error{nil, nil}}
	log := &lockedLog{}
	supervisor, err := startSupervisor(context.Background(), testOptions(log), lifecycle, watcher)
	if err != nil {
		t.Fatal(err)
	}
	process := lifecycle.process(0)
	process.exitErr = errors.New("process crashed")
	close(process.done)
	waitForCondition(t, func() bool { return log.contains("异常退出") }, "supervisor did not observe process exit")
	watcher.changes <- "main.go"
	waitForLifecycleCount(t, lifecycle, func(state lifecycleState) bool {
		return state.builds == 2 && state.starts == 2 && state.stops == 1 && state.ready == 2
	})
	if err := supervisor.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSupervisorCanEndSessionWhenProcessExits(t *testing.T) {
	watcher := newFakeReloadWatcher()
	lifecycle := &fakeProcessLifecycle{buildResults: []error{nil}}
	options := testOptions(io.Discard)
	options.ExitOnProcessExit = true
	supervisor, err := startSupervisor(context.Background(), options, lifecycle, watcher)
	if err != nil {
		t.Fatal(err)
	}
	process := lifecycle.process(0)
	process.exitErr = errors.New("process crashed")
	close(process.done)
	select {
	case <-supervisor.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not end after process exit")
	}
	if err := supervisor.Err(); err == nil || !strings.Contains(err.Error(), "process crashed") {
		t.Fatalf("Supervisor.Err() = %v", err)
	}
	if err := supervisor.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSupervisorRestartsFailedProcessWithBackoff(t *testing.T) {
	watcher := newFakeReloadWatcher()
	lifecycle := &fakeProcessLifecycle{buildResults: []error{nil}}
	options := testOptions(io.Discard)
	options.Restart = RestartOptions{
		Policy: RestartOnFailure, InitialDelay: 10 * time.Millisecond,
		MaxDelay: 20 * time.Millisecond, ResetAfter: time.Second,
	}
	supervisor, err := startSupervisor(context.Background(), options, lifecycle, watcher)
	if err != nil {
		t.Fatal(err)
	}
	process := lifecycle.process(0)
	process.exitErr = errors.New("process crashed")
	close(process.done)
	waitForLifecycleCount(t, lifecycle, func(state lifecycleState) bool { return state.starts == 2 })
	if err := supervisor.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSupervisorDoesNotLaunchAfterChangedFileBuildFails(t *testing.T) {
	watcher := newFakeReloadWatcher()
	lifecycle := &fakeProcessLifecycle{buildResults: []error{nil, errors.New("compile failed")}}
	log := &lockedLog{}
	options := testOptions(log)
	options.Restart = RestartOptions{
		Policy: RestartOnFailure, InitialDelay: 150 * time.Millisecond,
		MaxDelay: 300 * time.Millisecond, ResetAfter: time.Second,
	}
	supervisor, err := startSupervisor(context.Background(), options, lifecycle, watcher)
	if err != nil {
		t.Fatal(err)
	}
	process := lifecycle.process(0)
	process.exitErr = errors.New("process crashed")
	close(process.done)
	waitForCondition(t, func() bool { return log.contains("将在 150ms 后重启") }, "restart was not scheduled")
	watcher.changes <- "main.go"
	waitForLifecycleCount(t, lifecycle, func(state lifecycleState) bool { return state.builds == 2 })
	time.Sleep(200 * time.Millisecond)
	if state := lifecycle.state(); state.starts != 1 {
		t.Fatalf("starts after failed build = %d, want 1", state.starts)
	}
	if err := supervisor.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSupervisorReportsInitialCleanupFailure(t *testing.T) {
	readyErr := errors.New("health failed")
	stopErr := errors.New("stop failed")
	lifecycle := &fakeProcessLifecycle{
		buildResults: []error{nil},
		readyResults: []error{readyErr},
		stopResults:  []error{stopErr},
	}
	_, err := startSupervisor(context.Background(), testOptions(io.Discard), lifecycle, newFakeReloadWatcher())
	if err == nil || !strings.Contains(err.Error(), readyErr.Error()) || !strings.Contains(err.Error(), stopErr.Error()) {
		t.Fatalf("startSupervisor() error = %v", err)
	}
}

func TestSupervisorRetainsExitedProcessWhenCleanupFails(t *testing.T) {
	stopErr := errors.New("descendant still running")
	lifecycle := &fakeProcessLifecycle{stopResults: []error{stopErr, nil}}
	options := testOptions(io.Discard)
	options.Restart.Policy = RestartOnFailure
	supervisor, err := startSupervisor(context.Background(), options, lifecycle, newFakeReloadWatcher())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = supervisor.Close() })
	process := lifecycle.process(0)
	process.exitErr = errors.New("parent crashed")
	close(process.done)
	select {
	case <-supervisor.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not terminate after cleanup failed")
	}
	if !errors.Is(supervisor.Err(), stopErr) || supervisor.current != process {
		t.Fatalf("cleanup failure lost: current = %p, err = %v", supervisor.current, supervisor.Err())
	}
	if err := supervisor.Close(); err != nil {
		t.Fatal(err)
	}
	if state := lifecycle.state(); state.starts != 1 || state.stops != 2 {
		t.Fatalf("expected cleanup retry without replacement: %+v", state)
	}
}

func testOptions(writer io.Writer) Options {
	return Options{
		Name: "测试进程",
		Watch: WatchOptions{
			Paths: []string{"."}, Debounce: 10 * time.Millisecond,
		},
		Writer: writer,
	}
}

type fakeReloadWatcher struct {
	changes chan string
	errors  chan error
}

func newFakeReloadWatcher() *fakeReloadWatcher {
	return &fakeReloadWatcher{changes: make(chan string, 4), errors: make(chan error, 1)}
}

func (w *fakeReloadWatcher) Changes() <-chan string { return w.changes }
func (w *fakeReloadWatcher) Errors() <-chan error   { return w.errors }
func (w *fakeReloadWatcher) Close() error           { return nil }

type lifecycleState struct {
	builds int
	starts int
	stops  int
	ready  int
}

type fakeProcessLifecycle struct {
	mu           sync.Mutex
	buildResults []error
	readyResults []error
	stopResults  []error
	builds       int
	starts       int
	stops        int
	ready        int
	processes    []*Process
}

func (l *fakeProcessLifecycle) Build(context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	resultIndex := l.builds
	l.builds++
	if resultIndex < len(l.buildResults) {
		return l.buildResults[resultIndex]
	}
	return nil
}

func (l *fakeProcessLifecycle) Start() (*Process, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.starts++
	process := &Process{spec: Command{Name: "test"}, done: make(chan struct{})}
	l.processes = append(l.processes, process)
	return process, nil
}

func (l *fakeProcessLifecycle) Stop(*Process) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	resultIndex := l.stops
	l.stops++
	if resultIndex < len(l.stopResults) {
		return l.stopResults[resultIndex]
	}
	return nil
}

func (l *fakeProcessLifecycle) WaitReady(context.Context, *Process) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	resultIndex := l.ready
	l.ready++
	if resultIndex < len(l.readyResults) {
		return l.readyResults[resultIndex]
	}
	return nil
}

func (l *fakeProcessLifecycle) process(index int) *Process {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.processes[index]
}

func (l *fakeProcessLifecycle) state() lifecycleState {
	l.mu.Lock()
	defer l.mu.Unlock()
	return lifecycleState{builds: l.builds, starts: l.starts, stops: l.stops, ready: l.ready}
}

func waitForLifecycleCount(t *testing.T, lifecycle *fakeProcessLifecycle, ready func(lifecycleState) bool) {
	t.Helper()
	waitForCondition(t, func() bool { return ready(lifecycle.state()) }, "lifecycle did not reach expected state")
}

func waitForCondition(t *testing.T, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal(message)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

type lockedLog struct {
	mu      sync.Mutex
	content strings.Builder
}

func (l *lockedLog) Write(value []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.content.Write(value)
}

func (l *lockedLog) contains(value string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Contains(l.content.String(), value)
}
