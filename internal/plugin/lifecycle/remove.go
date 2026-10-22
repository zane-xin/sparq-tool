package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RemoveOptions 描述插件移除操作。
type RemoveOptions struct {
	Name       string
	PluginsDir string
}

// RemoveResult 描述插件移除后的备份位置。
type RemoveResult struct {
	Name      string
	BackupDir string
}

// Remove 将插件目录移动到可恢复的备份目录。
func Remove(options RemoveOptions) (RemoveResult, error) {
	name := strings.TrimSpace(options.Name)
	if name == "" {
		return RemoveResult{}, fmt.Errorf("插件名称不能为空")
	}
	pluginsDir := defaultString(strings.TrimSpace(options.PluginsDir), "plugins")
	source := filepath.Join(pluginsDir, name)
	if !exists(source) {
		return RemoveResult{}, fmt.Errorf("插件不存在: %s", source)
	}
	backupRoot := filepath.Join(pluginsDir, ".removed")
	if err := os.MkdirAll(backupRoot, 0o755); err != nil {
		return RemoveResult{}, err
	}
	backup := filepath.Join(backupRoot, name+"-"+time.Now().Format("20060102150405"))
	if err := os.Rename(source, backup); err != nil {
		return RemoveResult{}, err
	}
	return RemoveResult{Name: name, BackupDir: backup}, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
