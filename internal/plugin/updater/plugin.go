package updater

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zane-xin/sparq-tool/internal/plugin/installer"
	"github.com/zane-xin/sparq-tool/internal/plugin/manifest"
)

// PluginUpdateOptions 描述插件更新参数。
type PluginUpdateOptions struct {
	Source     string
	PluginsDir string
	TempDir    string
}

// PluginUpdateResult 描述插件更新结果。
type PluginUpdateResult struct {
	Code      string
	TargetDir string
	BackupDir string
}

// UpdatePlugin 从本地目录或 tar.gz 包更新已安装插件。
func UpdatePlugin(options PluginUpdateOptions) (PluginUpdateResult, error) {
	source := strings.TrimSpace(options.Source)
	if source == "" {
		return PluginUpdateResult{}, fmt.Errorf("插件来源不能为空")
	}
	pluginsDir := defaultString(strings.TrimSpace(options.PluginsDir), "plugins")
	tempDir := strings.TrimSpace(options.TempDir)
	if tempDir == "" {
		tempDir = filepath.Join(filepath.Dir(pluginsDir), "storage", "tmp", "plugins")
	}
	operationDir := filepath.Join(tempDir, itemCode(source)+"-"+time.Now().Format("20060102150405.000000000"))
	stageDir := filepath.Join(operationDir, "new")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		return PluginUpdateResult{}, err
	}
	if err := os.MkdirAll(operationDir, 0o755); err != nil {
		return PluginUpdateResult{}, err
	}
	defer os.RemoveAll(operationDir)
	if err := prepareSource(source, stageDir); err != nil {
		return PluginUpdateResult{}, err
	}
	item, err := manifest.Load(filepath.Join(stageDir, manifest.DefaultFileName))
	if err != nil {
		return PluginUpdateResult{}, err
	}
	targetDir := filepath.Join(pluginsDir, item.Metadata.Code)
	if !exists(targetDir) {
		return PluginUpdateResult{}, fmt.Errorf("插件未安装，请先执行 install: %s", item.Metadata.Code)
	}
	backupDir := filepath.Join(operationDir, "old")
	if err := os.Rename(targetDir, backupDir); err != nil {
		return PluginUpdateResult{}, err
	}
	if err := os.Rename(stageDir, targetDir); err != nil {
		_ = os.Rename(backupDir, targetDir)
		return PluginUpdateResult{}, err
	}
	return PluginUpdateResult{Code: item.Metadata.Code, TargetDir: targetDir}, nil
}

func itemCode(source string) string {
	base := filepath.Base(source)
	if base == "." || base == string(filepath.Separator) || base == "" {
		return "plugin"
	}
	return strings.NewReplacer("/", "_", "\\", "_", " ", "_").Replace(base)
}

func prepareSource(source string, target string) error {
	return installer.CopyOrExtract(source, target)
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
