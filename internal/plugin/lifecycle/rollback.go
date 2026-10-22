package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RollbackOptions 描述插件恢复操作。
type RollbackOptions struct {
	Name       string
	PluginsDir string
}

// RollbackResult 描述插件恢复使用的源目录和目标目录。
type RollbackResult struct {
	BackupDir string
	TargetDir string
}

// Rollback 恢复最近一次移除的插件备份。
func Rollback(options RollbackOptions) (RollbackResult, error) {
	name := strings.TrimSpace(options.Name)
	if name == "" {
		return RollbackResult{}, fmt.Errorf("插件名称不能为空")
	}
	pluginsDir := defaultString(strings.TrimSpace(options.PluginsDir), "plugins")
	target := filepath.Join(pluginsDir, name)
	if exists(target) {
		return RollbackResult{}, fmt.Errorf("插件已存在: %s", target)
	}
	backup, err := latestBackup(filepath.Join(pluginsDir, ".removed"), name)
	if err != nil {
		return RollbackResult{}, err
	}
	if err := os.Rename(backup, target); err != nil {
		return RollbackResult{}, err
	}
	return RollbackResult{BackupDir: backup, TargetDir: target}, nil
}
func latestBackup(removedDir string, name string) (string, error) {
	entries, err := os.ReadDir(removedDir)
	if err != nil {
		return "", err
	}
	prefix := name + "-"
	candidates := []string{}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			candidates = append(candidates, filepath.Join(removedDir, entry.Name()))
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("未找到可回滚的插件备份: %s", name)
	}
	sort.Strings(candidates)
	return candidates[len(candidates)-1], nil
}
