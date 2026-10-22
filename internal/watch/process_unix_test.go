//go:build unix

package watch

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestConfigureProcessCreatesOwnProcessGroup(t *testing.T) {
	command := exec.Command("sh", "-c", "true")
	configureProcess(command)
	if command.SysProcAttr == nil || !command.SysProcAttr.Setpgid {
		t.Fatal("命令没有配置独立进程组")
	}
}

func TestKeepStdinOpenPreventsLongRunningProcessFromReceivingEOF(t *testing.T) {
	process, err := StartProcess(Command{
		Name:          "stdin-listener",
		Command:       "read value",
		KeepStdinOpen: true,
		Stdin:         strings.NewReader(""),
		GraceTimeout:  200 * time.Millisecond,
		KillTimeout:   500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("启动测试进程失败: %v", err)
	}

	select {
	case <-process.Done():
		t.Fatalf("进程提前收到 stdin EOF: %v", process.Err())
	case <-time.After(100 * time.Millisecond):
	}
	if err := process.Stop(); err != nil {
		t.Fatalf("停止测试进程失败: %v", err)
	}
}

func TestStopCleansProcessGroupAfterParentExits(t *testing.T) {
	childPIDPath := filepath.Join(t.TempDir(), "child.pid")
	process, err := StartProcess(Command{
		Name:         "background-child",
		Command:      fmt.Sprintf("sleep 30 & echo $! > %q", childPIDPath),
		GraceTimeout: 200 * time.Millisecond,
		KillTimeout:  500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("启动测试进程失败: %v", err)
	}

	select {
	case <-process.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("父进程未按预期退出")
	}

	childPID := readPID(t, childPIDPath)
	t.Cleanup(func() {
		_ = syscall.Kill(childPID, syscall.SIGKILL)
	})
	if err := syscall.Kill(childPID, 0); err != nil {
		t.Fatalf("后台子进程未保持运行: %v", err)
	}
	processGroupID, err := syscall.Getpgid(childPID)
	if err != nil {
		t.Fatalf("读取后台子进程组失败: %v", err)
	}
	if processGroupID != process.cmd.Process.Pid {
		t.Fatalf("后台子进程组 = %d，期望 %d", processGroupID, process.cmd.Process.Pid)
	}

	if err := process.Stop(); err != nil {
		t.Fatalf("停止进程组失败: %v", err)
	}
	if err := syscall.Kill(childPID, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("后台子进程仍在运行: %v", err)
	}
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		contents, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(contents)))
			if parseErr != nil {
				t.Fatalf("解析后台子进程 PID 失败: %v", parseErr)
			}
			return pid
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("读取后台子进程 PID 失败: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("等待后台子进程 PID 超时")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
