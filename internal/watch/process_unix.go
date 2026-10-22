//go:build unix

package watch

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func interruptProcessTree(process *os.Process) error {
	return syscall.Kill(-process.Pid, syscall.SIGINT)
}

func killProcessTree(process *os.Process) error {
	return syscall.Kill(-process.Pid, syscall.SIGKILL)
}

func processTreeStopped(process *os.Process) bool {
	err := syscall.Kill(-process.Pid, 0)
	return errors.Is(err, syscall.ESRCH)
}
