package watch

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// Command 描述由 watchrun 管理的 shell 命令及其运行环境。
type Command struct {
	Name         string
	Command      string
	Args         []string
	CWD          string
	Env          []string
	GraceTimeout time.Duration
	KillTimeout  time.Duration
	// KeepStdinOpen 为不需要交互输入的长期进程提供持续打开的 stdin，避免工具把 EOF 当作退出信号。
	KeepStdinOpen bool
	Stdin         io.Reader
	Stdout        io.Writer
	Stderr        io.Writer
}

// Process 保存一个命令进程及其完整进程树的生命周期状态。
type Process struct {
	spec           Command
	cmd            *exec.Cmd
	stdinKeepAlive *os.File
	done           chan struct{}
	exitErr        error
}

// StartProcess 启动命令并将其放入独立进程组，供退出时清理完整子进程树。
func StartProcess(spec Command) (*Process, error) {
	command := commandProcess(spec.Command, spec.Args)
	configureProcess(command)
	if spec.CWD != "" {
		command.Dir = spec.CWD
	}
	if len(spec.Env) > 0 {
		command.Env = append(os.Environ(), spec.Env...)
	}
	var stdinReader *os.File
	var stdinKeepAlive *os.File
	if spec.KeepStdinOpen {
		var err error
		stdinReader, stdinKeepAlive, err = os.Pipe()
		if err != nil {
			return nil, fmt.Errorf("创建进程 %s 标准输入管道失败: %w", spec.Name, err)
		}
		command.Stdin = stdinReader
	} else {
		command.Stdin = defaultReader(spec.Stdin, os.Stdin)
	}
	command.Stdout = defaultWriter(spec.Stdout, os.Stdout)
	command.Stderr = defaultWriter(spec.Stderr, os.Stderr)
	if err := command.Start(); err != nil {
		if stdinReader != nil {
			_ = stdinReader.Close()
		}
		if stdinKeepAlive != nil {
			_ = stdinKeepAlive.Close()
		}
		return nil, fmt.Errorf("启动进程 %s 失败: %w", spec.Name, err)
	}
	if stdinReader != nil {
		_ = stdinReader.Close()
	}
	process := &Process{spec: spec, cmd: command, stdinKeepAlive: stdinKeepAlive, done: make(chan struct{})}
	go func() {
		process.exitErr = command.Wait()
		if process.stdinKeepAlive != nil {
			_ = process.stdinKeepAlive.Close()
		}
		close(process.done)
	}()
	return process, nil
}

// Done 在进程退出且退出状态可读取后关闭。
func (p *Process) Done() <-chan struct{} {
	return p.done
}

// Err 返回进程退出错误；调用方应在 Done 关闭后读取。
func (p *Process) Err() error {
	return p.exitErr
}

// Name 返回配置中的进程名称。
func (p *Process) Name() string {
	return p.spec.Name
}

// Stop 先请求进程组优雅退出，超时后清理完整进程树。
func (p *Process) Stop() error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	graceTimeout := p.spec.GraceTimeout
	if graceTimeout <= 0 {
		graceTimeout = 3 * time.Second
	}
	killTimeout := p.spec.KillTimeout
	if killTimeout <= 0 {
		killTimeout = 2 * time.Second
	}
	interruptErr := interruptProcessTree(p.cmd.Process)
	if p.waitProcessTreeStopped(graceTimeout) {
		return nil
	}
	killErr := killProcessTree(p.cmd.Process)
	if p.waitProcessTreeStopped(killTimeout) {
		return nil
	}
	return errors.Join(
		interruptErr,
		killErr,
		fmt.Errorf("进程组 %d 未在强制终止后于 %s 内退出", p.cmd.Process.Pid, killTimeout),
	)
}

// ExitError 将进程退出状态转换为包含进程名称的错误。
func ExitError(process *Process) error {
	if process.Err() != nil {
		return fmt.Errorf("进程 %s 退出异常: %w", process.Name(), process.Err())
	}
	return fmt.Errorf("进程 %s 已退出", process.Name())
}

func (p *Process) stopped() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

func (p *Process) waitProcessTreeStopped(timeout time.Duration) bool {
	if processTreeStopped(p.cmd.Process) {
		return true
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if processTreeStopped(p.cmd.Process) {
				return true
			}
		case <-deadline.C:
			return processTreeStopped(p.cmd.Process)
		}
	}
}

func shellCommand(command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/C", command)
	}
	return exec.Command("sh", "-c", command)
}

func commandProcess(command string, args []string) *exec.Cmd {
	if len(args) > 0 {
		return exec.Command(command, args...)
	}
	return shellCommand(command)
}

func defaultReader(value, fallback io.Reader) io.Reader {
	if value == nil {
		return fallback
	}
	return value
}

func defaultWriter(value, fallback io.Writer) io.Writer {
	if value == nil {
		return fallback
	}
	return value
}
