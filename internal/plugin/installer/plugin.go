package installer

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zane-xin/sparq-tool/internal/plugin/manifest"
)

// PluginInstallOptions 描述插件安装参数。
type PluginInstallOptions struct {
	Source     string
	PluginsDir string
}

// PluginInstallResult 描述插件安装结果。
type PluginInstallResult struct {
	Code      string
	TargetDir string
}

// InstallPlugin 从本地目录或 tar.gz 包安装插件。
func InstallPlugin(options PluginInstallOptions) (PluginInstallResult, error) {
	source := strings.TrimSpace(options.Source)
	if source == "" {
		return PluginInstallResult{}, fmt.Errorf("插件来源不能为空")
	}
	pluginsDir := defaultString(strings.TrimSpace(options.PluginsDir), "plugins")
	stageDir := filepath.Join(pluginsDir, ".installing-"+time.Now().Format("20060102150405"))
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		return PluginInstallResult{}, err
	}
	defer os.RemoveAll(stageDir)
	if err := CopyOrExtract(source, stageDir); err != nil {
		return PluginInstallResult{}, err
	}
	item, err := manifest.Load(filepath.Join(stageDir, manifest.DefaultFileName))
	if err != nil {
		return PluginInstallResult{}, err
	}
	targetDir := filepath.Join(pluginsDir, item.Metadata.Code)
	if exists(targetDir) {
		return PluginInstallResult{}, fmt.Errorf("插件已存在: %s", targetDir)
	}
	if err := os.Rename(stageDir, targetDir); err != nil {
		return PluginInstallResult{}, err
	}
	return PluginInstallResult{Code: item.Metadata.Code, TargetDir: targetDir}, nil
}

// CopyOrExtract 把插件目录或 tar.gz 包准备到目标目录。
func CopyOrExtract(source string, target string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(source, target)
	}
	if strings.HasSuffix(strings.ToLower(source), ".tar.gz") || strings.HasSuffix(strings.ToLower(source), ".tgz") {
		return extractTarGZ(source, target)
	}
	return fmt.Errorf("不支持的插件来源: %s", source)
}

func copyDir(source string, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return os.MkdirAll(target, 0o755)
		}
		next := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(next, 0o755)
		}
		return copyFile(path, next)
	})
}

func extractTarGZ(source string, target string) error {
	file, err := os.Open(source)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		path := filepath.Join(target, filepath.Clean(header.Name))
		if !isWithin(target, path) {
			return fmt.Errorf("插件包包含非法路径: %s", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, reader); err != nil {
				_ = out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		}
	}
}

func copyFile(source string, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer output.Close()
	_, err = io.Copy(output, input)
	return err
}

func isWithin(root string, target string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return false
	}
	return relative == "." || (!strings.HasPrefix(relative, ".."+string(filepath.Separator)) && relative != "..")
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
