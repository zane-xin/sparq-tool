package updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	protocolrelease "github.com/zane-xin/sparq-protocol/release"
)

const processPollInterval = 100 * time.Millisecond

// ExecutionResult 描述 updater 完成切换并获得新 Runtime 确认后的结果。
type ExecutionResult struct {
	Result
	RuntimePID int
}

// Execute 等待旧宿主退出、切换产物、启动新 Runtime，并在确认失败时回滚。
func Execute(plan Plan) (ExecutionResult, error) {
	if err := plan.Validate(); err != nil {
		return ExecutionResult{}, err
	}
	if err := waitForProcessExit(plan.Process.HostPID, time.Duration(plan.Process.HostExitTimeoutSeconds)*time.Second); err != nil {
		_ = persistTransaction(plan, protocolrelease.UpdateStatusFailed, err)
		return ExecutionResult{}, fmt.Errorf("wait for old runtime: %w", err)
	}
	if err := persistTransaction(plan, protocolrelease.UpdateStatusRestarting, nil); err != nil {
		return ExecutionResult{}, err
	}
	if err := prepareConfirmationPath(plan); err != nil {
		_ = persistTransaction(plan, protocolrelease.UpdateStatusFailed, err)
		return ExecutionResult{}, err
	}
	applied, err := Apply(plan)
	if err != nil {
		return ExecutionResult{}, rollbackAfterFailure(plan, err, nil)
	}
	command := exec.Command(plan.Process.ExecutablePath, plan.Process.Arguments...)
	command.Dir = plan.WorkingDir
	command.Env = os.Environ()
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		return ExecutionResult{}, rollbackAfterFailure(plan, fmt.Errorf("start new runtime: %w", err), nil)
	}
	if err := persistTransaction(plan, protocolrelease.UpdateStatusConfirming, nil); err != nil {
		_ = command.Process.Kill()
		return ExecutionResult{}, rollbackAfterFailure(plan, err, nil)
	}
	waitResult := make(chan error, 1)
	go func() { waitResult <- command.Wait() }()
	if err := waitForConfirmation(plan, waitResult, time.Duration(plan.Process.ConfirmationTimeoutSeconds)*time.Second); err != nil {
		stopErr := command.Process.Kill()
		select {
		case <-waitResult:
		case <-time.After(5 * time.Second):
			stopErr = errors.Join(stopErr, fmt.Errorf("new runtime did not exit after termination"))
		}
		return ExecutionResult{}, rollbackAfterFailure(plan, err, stopErr)
	}
	_ = writeUpdaterInstallationMetadata(plan)
	return ExecutionResult{Result: applied, RuntimePID: command.Process.Pid}, nil
}

func writeUpdaterInstallationMetadata(plan Plan) error {
	var updaterArtifact *ArtifactPlan
	for index := range plan.Artifacts {
		if plan.Artifacts[index].Code == protocolrelease.ArtifactUpdater {
			updaterArtifact = &plan.Artifacts[index]
			break
		}
	}
	if updaterArtifact == nil {
		return nil
	}
	digest, err := fileDigest(updaterArtifact.TargetPath)
	if err != nil {
		return err
	}
	installation := protocolrelease.UpdaterInstallation{Format: protocolrelease.UpdaterInstallationFormat, Version: plan.Transaction.Updater.Version, TargetOS: runtime.GOOS, TargetArch: runtime.GOARCH, SHA256: digest, ReleaseSHA256: updaterArtifact.ReleaseSHA256, InstalledAt: time.Now().UTC()}
	body, err := json.MarshalIndent(installation, "", "  ")
	if err != nil {
		return err
	}
	metadataPath := updaterArtifact.TargetPath + ".json"
	if err := ensureNoSymlinkParents(plan.AppRoot, metadataPath); err != nil {
		return err
	}
	return writeAtomic(metadataPath, append(body, '\n'), 0o600)
}

func rollbackAfterFailure(plan Plan, cause error, additional error) error {
	statusErr := persistTransaction(plan, protocolrelease.UpdateStatusRollingBack, cause)
	rollbackErr := Rollback(plan)
	if rollbackErr != nil {
		statusErr = errors.Join(statusErr, persistTransaction(plan, protocolrelease.UpdateStatusFailed, errors.Join(cause, rollbackErr)))
	} else {
		restartErr := restartRestoredRuntime(plan)
		if restartErr != nil {
			statusErr = errors.Join(statusErr, persistTransaction(plan, protocolrelease.UpdateStatusFailed, errors.Join(cause, restartErr)))
		} else {
			statusErr = errors.Join(statusErr, persistTransaction(plan, protocolrelease.UpdateStatusRolledBack, cause))
		}
		additional = errors.Join(additional, restartErr)
	}
	return errors.Join(cause, additional, rollbackErr, statusErr)
}

// restartRestoredRuntime restores service availability after the old files have
// been put back. It deliberately does not wait for update confirmation again.
func restartRestoredRuntime(plan Plan) error {
	command := exec.Command(plan.Process.ExecutablePath, plan.Process.Arguments...)
	command.Dir = plan.WorkingDir
	command.Env = os.Environ()
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		return fmt.Errorf("restart restored runtime: %w", err)
	}
	go func() { _ = command.Wait() }()
	return nil
}

func persistTransaction(plan Plan, status protocolrelease.UpdateStatus, cause error) error {
	transaction := plan.Transaction
	transaction.Status = status
	now := time.Now().UTC()
	if status == protocolrelease.UpdateStatusRestarting && transaction.RestartStartedAt == nil {
		transaction.RestartStartedAt = &now
	}
	if cause != nil {
		if status == protocolrelease.UpdateStatusRollingBack || status == protocolrelease.UpdateStatusRolledBack {
			transaction.RollbackReason = cause.Error()
		} else {
			transaction.FailureReason = cause.Error()
		}
	}
	body, err := json.MarshalIndent(transaction, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(filepath.Dir(plan.Process.ConfirmationPath), "transaction.json")
	return writeAtomic(path, append(body, '\n'), 0o600)
}

func writeAtomic(path string, body []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".update-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(body); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}

func prepareConfirmationPath(plan Plan) error {
	root, _ := filepath.Abs(plan.AppRoot)
	if err := ensureNoSymlinkParents(root, plan.Process.ConfirmationPath); err != nil {
		return fmt.Errorf("prepare confirmation path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(plan.Process.ConfirmationPath), 0o700); err != nil {
		return fmt.Errorf("prepare confirmation directory: %w", err)
	}
	if err := os.Remove(plan.Process.ConfirmationPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale confirmation: %w", err)
	}
	return nil
}

func waitForProcessExit(pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		alive, err := processAlive(pid)
		if err != nil {
			return err
		}
		if !alive {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("process %d did not exit within %s", pid, timeout)
		}
		time.Sleep(processPollInterval)
	}
}

func waitForConfirmation(plan Plan, processResult <-chan error, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(processPollInterval)
	defer ticker.Stop()
	for {
		select {
		case err := <-processResult:
			if err == nil {
				return fmt.Errorf("new runtime exited before startup confirmation")
			}
			return fmt.Errorf("new runtime exited before startup confirmation: %w", err)
		case <-deadline.C:
			return fmt.Errorf("new runtime did not confirm startup within %s", timeout)
		case <-ticker.C:
			confirmation, err := readConfirmation(plan.Process.ConfirmationPath)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
			}
			if err := confirmation.Validate(plan.Transaction.ID); err != nil {
				return err
			}
			return nil
		}
	}
}

func readConfirmation(path string) (protocolrelease.UpdateConfirmation, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return protocolrelease.UpdateConfirmation{}, err
	}
	var confirmation protocolrelease.UpdateConfirmation
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&confirmation); err != nil {
		return confirmation, fmt.Errorf("decode update confirmation: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return confirmation, fmt.Errorf("update confirmation must contain one JSON object")
	}
	return confirmation, nil
}
