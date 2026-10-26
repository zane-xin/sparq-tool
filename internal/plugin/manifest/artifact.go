package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zane-xin/sparq-protocol/plugin"
)

type frontendArtifact struct {
	Icons   map[string]string `json:"icons,omitempty"`
	Version string            `json:"version"`
	Entry   string            `json:"entry"`
	Styles  []string          `json:"styles,omitempty"`
	Files   map[string]string `json:"files"`
}

// MergeFrontendArtifacts 将构建生成的前端 artifact 合并到协议 Manifest。
// 未找到 artifact 时保持 Manifest 不变；找到但内容无效时返回错误。
func MergeFrontendArtifacts(value *plugin.Manifest, manifestPath, explicitPath string) error {
	if value == nil {
		return fmt.Errorf("插件协议 Manifest 不能为空")
	}
	_, err := mergeFrontendArtifacts(value, manifestPath, explicitPath)
	return err
}

func mergeFrontendArtifacts(manifest *plugin.Manifest, manifestPath, explicitPath string) (*frontendArtifact, error) {
	artifactPath, found, err := findArtifactPath(filepath.Dir(manifestPath), explicitPath)
	if err != nil || !found {
		return nil, err
	}
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		return nil, fmt.Errorf("读取前端 artifact %s: %w", artifactPath, err)
	}
	artifact, err := decodeFrontendArtifact(data, artifactPath)
	if err != nil {
		return nil, err
	}
	if manifest.Frontend == nil {
		return artifact, nil
	}
	manifest.Frontend.Artifact = &plugin.FrontendArtifact{
		Version: artifact.Version, Entry: artifact.Entry,
		Icons: cloneStringMap(artifact.Icons), Styles: append([]string(nil), artifact.Styles...), Files: cloneStringMap(artifact.Files),
	}
	return artifact, nil
}

func decodeFrontendArtifact(data []byte, artifactPath string) (*frontendArtifact, error) {
	var artifact frontendArtifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		return nil, fmt.Errorf("解析前端 artifact %s: %w", artifactPath, err)
	}
	if strings.TrimSpace(artifact.Version) == "" || strings.TrimSpace(artifact.Entry) == "" || len(artifact.Files) == 0 {
		return nil, fmt.Errorf("前端 artifact %s 缺少 version、entry 或 files", artifactPath)
	}
	if err := plugin.ValidateFrontendIcons(artifact.Icons, artifact.Files); err != nil {
		return nil, fmt.Errorf("前端 artifact %s: %w", artifactPath, err)
	}
	entryDigest, ok := artifact.Files[artifact.Entry]
	if !ok || strings.TrimSpace(entryDigest) == "" {
		return nil, fmt.Errorf("前端 artifact %s 未包含入口文件摘要 %s", artifactPath, artifact.Entry)
	}
	for _, style := range artifact.Styles {
		if filepath.Ext(style) != ".css" {
			return nil, fmt.Errorf("前端 artifact %s 包含非 CSS 样式文件 %s", artifactPath, style)
		}
		if digest, exists := artifact.Files[style]; !exists || strings.TrimSpace(digest) == "" {
			return nil, fmt.Errorf("前端 artifact %s 未包含样式文件摘要 %s", artifactPath, style)
		}
	}
	styles := make(map[string]bool, len(artifact.Styles))
	for _, style := range artifact.Styles {
		styles[style] = true
	}
	for file := range artifact.Files {
		if filepath.Ext(file) == ".css" && !styles[file] {
			return nil, fmt.Errorf("前端 artifact %s 未声明 root 样式文件 %s", artifactPath, file)
		}
	}
	return &artifact, nil
}

func findArtifactPath(pluginDir, explicitPath string) (string, bool, error) {
	if explicitPath != "" {
		path := explicitPath
		if !filepath.IsAbs(path) {
			path = filepath.Join(pluginDir, path)
		}
		if _, err := os.Stat(path); err != nil {
			return "", false, fmt.Errorf("检查前端 artifact %s: %w", path, err)
		}
		return filepath.Clean(path), true, nil
	}
	for _, candidate := range []string{
		filepath.Join(pluginDir, "artifact.json"),
		filepath.Join(pluginDir, "frontend", "artifact.json"),
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true, nil
		} else if !os.IsNotExist(err) {
			return "", false, fmt.Errorf("检查前端 artifact %s: %w", candidate, err)
		}
	}
	return "", false, nil
}

func cloneStringMap(values map[string]string) map[string]string {
	output := make(map[string]string, len(values))
	for key, value := range values {
		output[key] = value
	}
	return output
}
