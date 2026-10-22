package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	protocolrelease "github.com/zane-xin/sparq-protocol/release"
)

const PlanFormat = protocolrelease.UpdatePlanFormat

// Plan 是宿主写给一次性 updater 的本地执行计划，不包含 URL、凭据或任意命令。
type Plan protocolrelease.UpdatePlan

// ProcessPlan 描述旧宿主退出和新 Runtime 启动确认所需的最小本地信息。
type ProcessPlan = protocolrelease.UpdateProcessPlan

// ArtifactPlan 描述一个已下载并准备切换的本地产物。
type ArtifactPlan = protocolrelease.UpdateArtifactPlan

// ReadPlan 读取并严格解析 updater 计划，拒绝未知字段以避免宿主和 updater 误解协议。
func ReadPlan(path string) (Plan, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Plan{}, err
	}
	var plan Plan
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return Plan{}, fmt.Errorf("decode update plan: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err == nil {
		return Plan{}, fmt.Errorf("update plan contains trailing JSON")
	} else if err != io.EOF {
		return Plan{}, fmt.Errorf("decode update plan trailing data: %w", err)
	}
	return plan, nil
}

// Validate 校验计划格式、路径边界和可执行的产物集合。
func (plan Plan) Validate() error {
	if plan.Format != PlanFormat {
		return fmt.Errorf("update plan format %q is invalid", plan.Format)
	}
	if plan.Transaction.ID == "" || plan.Transaction.TargetVersion == "" {
		return fmt.Errorf("update transaction id and target version are required")
	}
	if plan.Transaction.Status != protocolrelease.UpdateStatusPrepared && plan.Transaction.Status != protocolrelease.UpdateStatusRestarting {
		return fmt.Errorf("update transaction status %q is not executable", plan.Transaction.Status)
	}
	if strings.TrimSpace(plan.AppRoot) == "" || !filepath.IsAbs(plan.AppRoot) {
		return fmt.Errorf("app root must be an absolute non-empty path")
	}
	root, err := filepath.Abs(plan.AppRoot)
	if err != nil {
		return fmt.Errorf("app root: %w", err)
	}
	if plan.WorkingDir != "" {
		if _, err := pathWithin(root, plan.WorkingDir); err != nil {
			return fmt.Errorf("working directory: %w", err)
		}
	}
	if len(plan.Artifacts) == 0 {
		return fmt.Errorf("update artifacts are required")
	}
	seenTargets := make(map[string]struct{}, len(plan.Artifacts))
	runtimeTarget := ""
	for _, artifact := range plan.Artifacts {
		if artifact.Code != protocolrelease.ArtifactRuntime && artifact.Code != protocolrelease.ArtifactAdmin && artifact.Code != protocolrelease.ArtifactUpdater {
			return fmt.Errorf("artifact code %q is not allowed", artifact.Code)
		}
		if artifact.Kind == "" {
			artifact.Kind = "file"
		}
		if artifact.Kind != "file" && artifact.Kind != "directory" {
			return fmt.Errorf("artifact %s kind %q is not allowed", artifact.Code, artifact.Kind)
		}
		if artifact.Code == protocolrelease.ArtifactAdmin && artifact.Kind != "directory" {
			return fmt.Errorf("admin artifact must be a directory")
		}
		if artifact.Code != protocolrelease.ArtifactAdmin && artifact.Kind != "file" {
			return fmt.Errorf("artifact %s must be a file", artifact.Code)
		}
		for name, path := range map[string]string{"source": artifact.SourcePath, "target": artifact.TargetPath, "backup": artifact.BackupPath} {
			if _, err := pathWithin(root, path); err != nil {
				return fmt.Errorf("artifact %s path: %w", name, err)
			}
		}
		key, _ := filepath.Abs(artifact.TargetPath)
		if _, exists := seenTargets[key]; exists {
			return fmt.Errorf("artifact target %q is duplicated", artifact.TargetPath)
		}
		seenTargets[key] = struct{}{}
		if artifact.Code == protocolrelease.ArtifactRuntime {
			if runtimeTarget != "" {
				return fmt.Errorf("update plan must contain exactly one runtime artifact")
			}
			runtimeTarget = key
		}
		sourcePath, _ := filepath.Abs(artifact.SourcePath)
		backupPath, _ := filepath.Abs(artifact.BackupPath)
		if sourcePath == key || backupPath == key || sourcePath == backupPath {
			return fmt.Errorf("artifact %s source, target and backup paths must differ", artifact.Code)
		}
		if excludedPath(root, artifact.TargetPath) {
			return fmt.Errorf("artifact target %q is inside a protected user directory", artifact.TargetPath)
		}
		if artifact.SHA256 != "" && !validSHA256(artifact.SHA256) {
			return fmt.Errorf("artifact %s sha256 is invalid", artifact.Code)
		}
	}
	if runtimeTarget == "" {
		return fmt.Errorf("update plan must contain exactly one runtime artifact")
	}
	if plan.Process.HostPID <= 0 {
		return fmt.Errorf("host pid must be greater than zero")
	}
	executablePath, err := pathWithin(root, plan.Process.ExecutablePath)
	if err != nil {
		return fmt.Errorf("runtime executable path: %w", err)
	}
	if executablePath != runtimeTarget {
		return fmt.Errorf("runtime executable path must match the runtime artifact target")
	}
	confirmationPath, err := pathWithin(root, plan.Process.ConfirmationPath)
	if err != nil {
		return fmt.Errorf("confirmation path: %w", err)
	}
	confirmationRelative, err := filepath.Rel(root, confirmationPath)
	if err != nil || !strings.HasPrefix(filepath.ToSlash(confirmationRelative), "storage/update/"+plan.Transaction.ID+"/") {
		return fmt.Errorf("confirmation path must be inside the transaction update directory")
	}
	if plan.Process.HostExitTimeoutSeconds < 1 || plan.Process.HostExitTimeoutSeconds > 600 {
		return fmt.Errorf("host exit timeout must be between 1 and 600 seconds")
	}
	if plan.Process.ConfirmationTimeoutSeconds < 1 || plan.Process.ConfirmationTimeoutSeconds > 600 {
		return fmt.Errorf("confirmation timeout must be between 1 and 600 seconds")
	}
	for _, argument := range plan.Process.Arguments {
		if strings.ContainsRune(argument, '\x00') {
			return fmt.Errorf("runtime argument contains NUL")
		}
	}
	return nil
}

func pathWithin(root, value string) (string, error) {
	if strings.TrimSpace(value) == "" || !filepath.IsAbs(value) {
		return "", fmt.Errorf("path must be absolute")
	}
	path, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes app root", value)
	}
	return path, nil
}

func excludedPath(root, value string) bool {
	path, err := pathWithin(root, value)
	if err != nil {
		return true
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return true
	}
	first, _, _ := strings.Cut(filepath.ToSlash(relative), "/")
	switch first {
	case "plugins", "storage/plugins", "storage/plugin-data", "storage/uploads", "config", "logs", "backups":
		return true
	}
	if first == "storage" {
		// 更新计划只能写入 storage/update，避免覆盖插件数据、数据库和上传文件。
		return !strings.HasPrefix(filepath.ToSlash(relative), "storage/update/")
	}
	return strings.HasPrefix(filepath.ToSlash(relative), "database")
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func rejectSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlink is not allowed: %s", path)
	}
	return nil
}

func fileDigest(path string) (string, error) {
	if err := rejectSymlink(path); err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("artifact source must be a regular file: %s", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), nil
}

func artifactSourceValid(path, kind string) error {
	if err := rejectSymlink(path); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if kind == "directory" {
		if !info.IsDir() {
			return fmt.Errorf("artifact source must be a directory: %s", path)
		}
		return rejectDirectorySymlinks(path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("artifact source must be a regular file: %s", path)
	}
	return nil
}

func rejectDirectorySymlinks(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not allowed: %s", path)
		}
		return nil
	})
}

func ensureNoSymlinkParents(root, path string) error {
	current := filepath.Dir(path)
	for {
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink parent is not allowed: %s", current)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if current == root {
			return nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}
