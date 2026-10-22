package process

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Options 描述重启命令参数。
type Options struct {
	Driver  string
	Command string
	Timeout time.Duration
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
}

// Run 执行重启。
func Run(ctx context.Context, options Options) error {
	driver := strings.TrimSpace(options.Driver)
	if driver == "" {
		driver = "command"
	}
	switch driver {
	case "command":
		return runCommand(ctx, options)
	default:
		return fmt.Errorf("不支持的重启驱动: %s", driver)
	}
}

// IsReservedDriver 报告驱动是否已占位但尚未实现。
func IsReservedDriver(driver string) bool {
	switch strings.TrimSpace(driver) {
	case "systemd", "docker-compose":
		return true
	default:
		return false
	}
}

func runCommand(ctx context.Context, options Options) error {
	value := strings.TrimSpace(options.Command)
	if value == "" {
		return fmt.Errorf("command 驱动需要提供 --command")
	}
	command := shellCommand(ctx, value)
	command.Stdout = options.Stdout
	command.Stderr = options.Stderr
	command.Stdin = options.Stdin
	if err := command.Run(); err != nil {
		return fmt.Errorf("重启命令执行失败: %w", err)
	}
	return nil
}

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", command)
	}
	return exec.CommandContext(ctx, "sh", "-c", command)
}
