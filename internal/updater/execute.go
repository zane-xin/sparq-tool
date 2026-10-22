package updater

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Result 记录已经切换的产物，供宿主审计和 updater 后续启动确认使用。
type Result struct {
	Applied []string
}

type appliedArtifact struct {
	plan      ArtifactPlan
	hadTarget bool
}

// Apply 校验并切换本地计划。任一步失败都会恢复此前已经切换的目标。
func Apply(plan Plan) (Result, error) {
	if err := plan.Validate(); err != nil {
		return Result{}, err
	}
	root, err := filepath.Abs(plan.AppRoot)
	if err != nil {
		return Result{}, err
	}
	applied := make([]appliedArtifact, 0, len(plan.Artifacts))
	result := Result{Applied: make([]string, 0, len(plan.Artifacts))}
	for _, artifact := range plan.Artifacts {
		current, err := applyArtifact(root, artifact)
		if err != nil {
			return Result{}, errors.Join(fmt.Errorf("apply %s artifact: %w", artifact.Code, err), rollbackApplied(applied))
		}
		applied = append(applied, current)
		result.Applied = append(result.Applied, artifact.Code)
	}
	return result, nil
}

// Rollback 恢复计划中的备份，并把失败的新版本移回原 source_path 供诊断。
func Rollback(plan Plan) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	applied := make([]appliedArtifact, 0, len(plan.Artifacts))
	for _, artifact := range plan.Artifacts {
		_, err := os.Lstat(artifact.BackupPath)
		applied = append(applied, appliedArtifact{plan: artifact, hadTarget: err == nil})
	}
	return rollbackApplied(applied)
}

func applyArtifact(root string, artifact ArtifactPlan) (appliedArtifact, error) {
	if err := ensureNoSymlinkParents(root, artifact.SourcePath); err != nil {
		return appliedArtifact{}, err
	}
	if err := artifactSourceValid(artifact.SourcePath, artifact.Kind); err != nil {
		return appliedArtifact{}, err
	}
	if artifact.SHA256 != "" && artifact.Kind == "file" {
		digest, err := fileDigest(artifact.SourcePath)
		if err != nil {
			return appliedArtifact{}, err
		}
		if !strings.EqualFold(digest, artifact.SHA256) {
			return appliedArtifact{}, fmt.Errorf("source sha256 does not match plan")
		}
	}
	if _, err := os.Lstat(artifact.BackupPath); err == nil {
		return appliedArtifact{}, fmt.Errorf("backup path already exists: %s", artifact.BackupPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return appliedArtifact{}, err
	}
	for _, parent := range []string{filepath.Dir(artifact.TargetPath), filepath.Dir(artifact.BackupPath)} {
		if err := ensureNoSymlinkParents(root, parent); err != nil {
			return appliedArtifact{}, err
		}
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return appliedArtifact{}, err
		}
	}
	current := appliedArtifact{plan: artifact}
	if _, err := os.Lstat(artifact.TargetPath); err == nil {
		if err := rejectSymlink(artifact.TargetPath); err != nil {
			return appliedArtifact{}, err
		}
		if err := os.Rename(artifact.TargetPath, artifact.BackupPath); err != nil {
			return appliedArtifact{}, err
		}
		current.hadTarget = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return appliedArtifact{}, err
	}
	if err := os.Rename(artifact.SourcePath, artifact.TargetPath); err != nil {
		if current.hadTarget {
			return appliedArtifact{}, errors.Join(err, os.Rename(artifact.BackupPath, artifact.TargetPath))
		}
		return appliedArtifact{}, err
	}
	return current, nil
}

func rollbackApplied(applied []appliedArtifact) error {
	var joined error
	for index := len(applied) - 1; index >= 0; index-- {
		current := applied[index]
		if _, err := os.Lstat(current.plan.TargetPath); err == nil {
			if _, sourceErr := os.Lstat(current.plan.SourcePath); errors.Is(sourceErr, os.ErrNotExist) {
				joined = errors.Join(joined, os.Rename(current.plan.TargetPath, current.plan.SourcePath))
			} else if sourceErr != nil {
				joined = errors.Join(joined, sourceErr)
			} else {
				joined = errors.Join(joined, fmt.Errorf("rollback source already exists: %s", current.plan.SourcePath))
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			joined = errors.Join(joined, err)
		}
		if current.hadTarget {
			if err := os.Rename(current.plan.BackupPath, current.plan.TargetPath); err != nil {
				joined = errors.Join(joined, err)
			}
		}
	}
	return joined
}
