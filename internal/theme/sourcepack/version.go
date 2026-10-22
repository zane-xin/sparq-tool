package sourcepack

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	pluginmanifest "github.com/zane-xin/sparq-tool/internal/plugin/manifest"
	"gopkg.in/yaml.v3"
)

// ReadVersion 读取并校验主题双清单中的当前版本。
func ReadVersion(sourceDir string) (string, error) {
	root, err := filepath.Abs(defaultValue(sourceDir, "."))
	if err != nil {
		return "", fmt.Errorf("解析主题目录失败: %w", err)
	}
	source, runtime, err := loadManifests(root)
	if err != nil {
		return "", err
	}
	if source.Metadata.Version != runtime.Version {
		return "", fmt.Errorf("theme.yaml 与 template.yaml 的版本不一致")
	}
	return source.Metadata.Version, nil
}

// UpdateVersion 同时更新 template.yaml 和 theme.yaml，任一写入失败时恢复原文件。
func UpdateVersion(sourceDir, target string) error {
	root, err := filepath.Abs(defaultValue(sourceDir, "."))
	if err != nil {
		return fmt.Errorf("解析主题目录失败: %w", err)
	}
	if _, err := pluginmanifest.ParseVersion(target); err != nil {
		return err
	}
	if _, _, err := loadManifests(root); err != nil {
		return err
	}
	templatePath := filepath.Join(root, "template.yaml")
	themePath := filepath.Join(root, "theme.yaml")
	templateBody, templateMode, err := updatedManifestVersion(templatePath, target, "metadata", "version")
	if err != nil {
		return err
	}
	themeBody, themeMode, err := updatedManifestVersion(themePath, target, "version")
	if err != nil {
		return err
	}
	originalTemplate, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("读取 template.yaml 失败: %w", err)
	}
	originalTheme, err := os.ReadFile(themePath)
	if err != nil {
		return fmt.Errorf("读取 theme.yaml 失败: %w", err)
	}
	if err := writeManifestAtomic(templatePath, templateBody, templateMode); err != nil {
		return fmt.Errorf("更新 template.yaml 版本失败: %w", err)
	}
	if err := writeManifestAtomic(themePath, themeBody, themeMode); err != nil {
		restoreErr := writeManifestAtomic(templatePath, originalTemplate, templateMode)
		if restoreErr != nil {
			return fmt.Errorf("更新 theme.yaml 版本失败: %w；恢复 template.yaml 失败: %v", err, restoreErr)
		}
		return fmt.Errorf("更新 theme.yaml 版本失败: %w", err)
	}
	if _, _, err := loadManifests(root); err != nil {
		_ = writeManifestAtomic(templatePath, originalTemplate, templateMode)
		_ = writeManifestAtomic(themePath, originalTheme, themeMode)
		return fmt.Errorf("更新主题版本后双清单校验失败: %w", err)
	}
	return nil
}

func updatedManifestVersion(path, target string, keys ...string) ([]byte, os.FileMode, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, fmt.Errorf("读取 %s 失败: %w", filepath.Base(path), err)
	}
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&document); err != nil || len(document.Content) != 1 {
		return nil, 0, fmt.Errorf("解析 %s 失败", filepath.Base(path))
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, 0, fmt.Errorf("%s 只能包含一个 YAML 文档", filepath.Base(path))
	}
	versionNode, err := manifestValue(document.Content[0], keys...)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	versionNode.Value = target
	versionNode.Tag = "!!str"
	var encoded bytes.Buffer
	encoder := yaml.NewEncoder(&encoded)
	encoder.SetIndent(2)
	if err := encoder.Encode(&document); err != nil {
		return nil, 0, fmt.Errorf("编码 %s 失败: %w", filepath.Base(path), err)
	}
	if err := encoder.Close(); err != nil {
		return nil, 0, fmt.Errorf("编码 %s 失败: %w", filepath.Base(path), err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, err
	}
	return encoded.Bytes(), info.Mode().Perm(), nil
}

func manifestValue(node *yaml.Node, keys ...string) (*yaml.Node, error) {
	current := node
	for index, key := range keys {
		if current.Kind != yaml.MappingNode {
			path := "YAML 根节点"
			if index > 0 {
				path = keys[index-1]
			}
			return nil, fmt.Errorf("%s 必须是对象", path)
		}
		var next *yaml.Node
		for position := 0; position+1 < len(current.Content); position += 2 {
			if current.Content[position].Value == key {
				next = current.Content[position+1]
				break
			}
		}
		if next == nil {
			return nil, fmt.Errorf("缺少字段 %s", key)
		}
		current = next
	}
	if current.Kind != yaml.ScalarNode {
		return nil, fmt.Errorf("版本必须是字符串")
	}
	return current, nil
}

func writeManifestAtomic(path string, body []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".theme-version-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(body); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
