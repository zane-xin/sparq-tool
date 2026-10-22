package watch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

const (
	RestartNever     = "never"
	RestartOnFailure = "on-failure"
)

// ReadyChecker 在新进程启动后确认它已经可以接收请求。
type ReadyChecker interface {
	Wait(context.Context, *Process) error
}

// RestartOptions 描述运行进程异常退出后的退避重启策略。
type RestartOptions struct {
	Policy       string
	InitialDelay time.Duration
	MaxDelay     time.Duration
	ResetAfter   time.Duration
}

// Validate 校验失败重启策略及其退避时间。
func (o RestartOptions) Validate() error {
	if o.Policy != RestartNever && o.Policy != RestartOnFailure {
		return fmt.Errorf("restart.policy 只支持 %q 或 %q: %s", RestartNever, RestartOnFailure, o.Policy)
	}
	if o.InitialDelay <= 0 {
		return fmt.Errorf("restart.initial_delay 必须大于 0")
	}
	if o.MaxDelay < o.InitialDelay {
		return fmt.Errorf("restart.max_delay 不能小于 initial_delay")
	}
	if o.ResetAfter <= 0 {
		return fmt.Errorf("restart.reset_after 必须大于 0")
	}
	return nil
}

// Options 描述一次监听、构建和进程替换会话。
type Options struct {
	Name             string
	Watch            WatchOptions
	Build            Command
	Run              Command
	Restart          RestartOptions
	Checker          ReadyChecker
	SkipInitialBuild bool
	// ExitOnProcessExit 让上层编排器把被管理进程的退出作为会话终止信号。
	// 默认关闭以保持 watch 命令在进程退出后等待文件变化的既有行为。
	ExitOnProcessExit bool
	Writer            io.Writer
}

type processLifecycle interface {
	Build(context.Context) error
	Start() (*Process, error)
	Stop(*Process) error
	WaitReady(context.Context, *Process) error
}

type commandLifecycle struct {
	options Options
}

func (l commandLifecycle) Build(ctx context.Context) error {
	if l.options.Build.Command == "" {
		return nil
	}
	process, err := StartProcess(l.options.Build)
	if err != nil {
		return err
	}
	select {
	case <-process.Done():
		if process.Err() != nil {
			return fmt.Errorf("构建命令 %q 执行失败: %w", l.options.Build.Command, process.Err())
		}
		return nil
	case <-ctx.Done():
		return errors.Join(ctx.Err(), process.Stop())
	}
}

func (l commandLifecycle) Start() (*Process, error) {
	return StartProcess(l.options.Run)
}

func (l commandLifecycle) Stop(process *Process) error {
	return process.Stop()
}

func (l commandLifecycle) WaitReady(ctx context.Context, process *Process) error {
	if l.options.Checker == nil {
		select {
		case <-process.Done():
			return ExitError(process)
		default:
			return nil
		}
	}
	return l.options.Checker.Wait(ctx, process)
}

// Supervisor 管理构建和运行命令，并在文件变化或异常退出后更新运行进程。
type Supervisor struct {
	ctx              context.Context
	cancel           context.CancelFunc
	options          Options
	lifecycle        processLifecycle
	watcher          reloadWatcher
	current          *Process
	currentStartedAt time.Time
	restartDelay     time.Duration
	restartTimer     *time.Timer
	restartC         <-chan time.Time
	done             chan struct{}
	closeOnce        sync.Once
	closeErr         error
	runErr           error
}

// Start 创建文件监听器，执行首次构建并启动运行进程。
func Start(parent context.Context, options Options) (*Supervisor, error) {
	options = normalizeOptions(options)
	if options.Run.Command == "" {
		return nil, fmt.Errorf("run.command 不能为空")
	}
	if err := options.Restart.Validate(); err != nil {
		return nil, err
	}
	watcher, err := newReloadWatcher(options.Watch)
	if err != nil {
		return nil, err
	}
	return startSupervisor(parent, options, commandLifecycle{options: options}, watcher)
}

func startSupervisor(
	parent context.Context,
	options Options,
	lifecycle processLifecycle,
	watcher reloadWatcher,
) (*Supervisor, error) {
	options = normalizeOptions(options)
	ctx, cancel := context.WithCancel(parent)
	supervisor := &Supervisor{
		ctx:          ctx,
		cancel:       cancel,
		options:      options,
		lifecycle:    lifecycle,
		watcher:      watcher,
		restartDelay: options.Restart.InitialDelay,
		done:         make(chan struct{}),
	}
	if !options.SkipInitialBuild {
		buildStarted := time.Now()
		supervisor.log("build", "开始首次构建")
		if err := lifecycle.Build(ctx); err != nil {
			cancel()
			return nil, errors.Join(
				fmt.Errorf("%s首次构建失败: %w", options.Name, err),
				watcher.Close(),
			)
		}
		supervisor.log("build", "首次构建完成，耗时 %s", elapsed(buildStarted))
	}
	process, err := lifecycle.Start()
	if err != nil {
		cancel()
		return nil, errors.Join(err, watcher.Close())
	}
	supervisor.current = process
	supervisor.currentStartedAt = time.Now()
	supervisor.log("run", "已启动%s", options.Name)
	healthStarted := time.Now()
	supervisor.log("health", "等待%s就绪", options.Name)
	if err := lifecycle.WaitReady(ctx, process); err != nil {
		cancel()
		return nil, errors.Join(
			fmt.Errorf("等待%s就绪失败: %w", options.Name, err),
			watcher.Close(),
			lifecycle.Stop(process),
		)
	}
	supervisor.log("health", "%s已就绪，耗时 %s", options.Name, elapsed(healthStarted))
	supervisor.log("watch", "文件监听已启用，模式 %s，监听 %d 个根目录", defaultWatchMode(options.Watch.Mode), len(options.Watch.Paths))
	go supervisor.run()
	return supervisor, nil
}

// Close 停止监听并清理当前运行进程。重复调用返回第一次清理结果。
func (s *Supervisor) Close() error {
	s.closeOnce.Do(func() {
		s.cancel()
		<-s.done
		s.closeErr = errors.Join(s.watcher.Close(), s.lifecycle.Stop(s.current))
	})
	return s.closeErr
}

// Done 在 supervisor 停止运行后关闭。
func (s *Supervisor) Done() <-chan struct{} {
	return s.done
}

// Err 返回 supervisor 因被管理进程退出而结束时的错误；调用方应在 Done 关闭后读取。
func (s *Supervisor) Err() error {
	return s.runErr
}

func (s *Supervisor) run() {
	defer close(s.done)
	defer s.cancelRestart()
	for {
		var processDone <-chan struct{}
		if s.current != nil {
			processDone = s.current.Done()
		}
		select {
		case <-s.ctx.Done():
			return
		case err := <-s.watcher.Errors():
			s.log("watch", "监听警告: %v", err)
		case path := <-s.watcher.Changes():
			s.cancelRestart()
			changedPath, ok := s.waitForQuietPeriod(path)
			if !ok {
				return
			}
			s.rebuild(changedPath)
		case <-processDone:
			process := s.current
			runningFor := time.Since(s.currentStartedAt)
			// 父进程退出不代表子进程已停止；清理成功前保留引用，避免丢失进程组。
			if err := s.lifecycle.Stop(process); err != nil {
				s.runErr = errors.Join(ExitError(process), fmt.Errorf("清理%s退出后的进程组失败: %w", s.options.Name, err))
				s.log("run", "%v", s.runErr)
				return
			}
			s.current = nil
			if process.Err() == nil {
				s.log("run", "%s已正常退出，等待文件变化后重新启动", s.options.Name)
				if s.options.ExitOnProcessExit {
					s.runErr = ExitError(process)
					return
				}
				continue
			}
			err := ExitError(process)
			s.log("run", "%s异常退出: %v", s.options.Name, err)
			if s.options.ExitOnProcessExit {
				s.runErr = err
				return
			}
			if runningFor >= s.options.Restart.ResetAfter {
				s.restartDelay = s.options.Restart.InitialDelay
			}
			s.scheduleRestart(err)
		case <-s.restartC:
			s.restartTimer = nil
			s.restartC = nil
			s.restart()
		}
	}
}

func (s *Supervisor) waitForQuietPeriod(path string) (string, bool) {
	timer := time.NewTimer(s.options.Watch.Debounce)
	defer timer.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return "", false
		case changedPath := <-s.watcher.Changes():
			path = changedPath
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(s.options.Watch.Debounce)
		case err := <-s.watcher.Errors():
			s.log("watch", "监听警告: %v", err)
		case <-timer.C:
			return path, true
		}
	}
}

func (s *Supervisor) rebuild(path string) {
	s.log("watch", "检测到文件变更: %s", path)
	buildStarted := time.Now()
	s.log("build", "开始构建")
	if err := s.lifecycle.Build(s.ctx); err != nil {
		if s.ctx.Err() == nil {
			s.log("build", "构建失败，保留当前运行状态: %v", err)
		}
		return
	}
	s.log("build", "构建完成，耗时 %s", elapsed(buildStarted))
	if s.ctx.Err() != nil {
		return
	}
	if s.current != nil {
		s.log("run", "停止旧%s", s.options.Name)
		if err := s.lifecycle.Stop(s.current); err != nil {
			s.log("run", "停止旧%s失败，本次重新运行已取消: %v", s.options.Name, err)
			return
		}
		s.current = nil
	}
	retryable, err := s.launch("文件变更")
	if err != nil {
		s.log("run", "重新运行失败: %v", err)
		if retryable {
			s.scheduleRestart(err)
		}
		return
	}
	s.restartDelay = s.options.Restart.InitialDelay
}

func (s *Supervisor) restart() {
	retryable, err := s.launch("失败重启")
	if err == nil {
		return
	}
	s.log("run", "失败重启未成功: %v", err)
	if retryable {
		s.scheduleRestart(err)
	}
}

func (s *Supervisor) launch(reason string) (bool, error) {
	started := time.Now()
	s.log("run", "%s触发启动%s", reason, s.options.Name)
	process, err := s.lifecycle.Start()
	if err != nil {
		return true, err
	}
	s.current = process
	s.currentStartedAt = time.Now()
	healthStarted := time.Now()
	s.log("health", "等待新%s就绪", s.options.Name)
	if err := s.lifecycle.WaitReady(s.ctx, process); err != nil {
		stopErr := s.lifecycle.Stop(process)
		if stopErr != nil {
			return false, errors.Join(err, fmt.Errorf("停止未就绪%s失败: %w", s.options.Name, stopErr))
		}
		s.current = nil
		return true, err
	}
	s.log("health", "新%s已就绪，耗时 %s", s.options.Name, elapsed(healthStarted))
	s.log("run", "%s完成，耗时 %s", reason, elapsed(started))
	return false, nil
}

func (s *Supervisor) scheduleRestart(reason error) {
	if s.options.Restart.Policy != RestartOnFailure || s.ctx.Err() != nil || s.current != nil {
		s.log("run", "等待文件变化后重新启动: %v", reason)
		return
	}
	s.cancelRestart()
	delay := s.restartDelay
	s.restartDelay = nextRestartDelay(delay, s.options.Restart.MaxDelay)
	s.restartTimer = time.NewTimer(delay)
	s.restartC = s.restartTimer.C
	s.log("run", "将在 %s 后重启，下一次退避 %s", delay, s.restartDelay)
}

func (s *Supervisor) cancelRestart() {
	if s.restartTimer != nil {
		s.restartTimer.Stop()
	}
	s.restartTimer = nil
	s.restartC = nil
}

func (s *Supervisor) log(stage, format string, values ...any) {
	fmt.Fprintf(s.options.Writer, "[%s] %s\n", stage, fmt.Sprintf(format, values...))
}

func normalizeOptions(options Options) Options {
	if options.Name == "" {
		options.Name = options.Run.Name
	}
	if options.Name == "" {
		options.Name = "开发进程"
	}
	if options.Run.Name == "" {
		options.Run.Name = options.Name
	}
	if options.Build.Name == "" {
		options.Build.Name = options.Name + "构建"
	}
	if options.Writer == nil {
		options.Writer = io.Discard
	}
	if options.Build.Stdout == nil {
		options.Build.Stdout = options.Writer
	}
	if options.Build.Stderr == nil {
		options.Build.Stderr = options.Writer
	}
	if options.Run.Stdout == nil {
		options.Run.Stdout = options.Writer
	}
	if options.Run.Stderr == nil {
		options.Run.Stderr = options.Writer
	}
	if options.Restart.Policy == "" {
		options.Restart.Policy = RestartNever
	}
	if options.Restart.InitialDelay <= 0 {
		options.Restart.InitialDelay = 500 * time.Millisecond
	}
	if options.Restart.MaxDelay <= 0 {
		options.Restart.MaxDelay = 10 * time.Second
	}
	if options.Restart.ResetAfter <= 0 {
		options.Restart.ResetAfter = 30 * time.Second
	}
	return options
}

func nextRestartDelay(current, maximum time.Duration) time.Duration {
	if current >= maximum || current > maximum/2 {
		return maximum
	}
	return current * 2
}

func elapsed(start time.Time) time.Duration {
	return time.Since(start).Round(time.Millisecond)
}
