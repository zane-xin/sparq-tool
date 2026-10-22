package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zane-xin/sparq-protocol/plugin"
)

const frontendEntryArtifactFileName = "entry-artifact.json"

// RequiresFrontendEntryArtifact 判断插件是否声明了需要独立本地页面产物的前端 Entry。
// trusted root 在宿主进程内加载统一的 frontend/artifact.json，不创建 iframe 文档，
// 因此不能要求旧的 entry-artifact.json。
func RequiresFrontendEntryArtifact(value plugin.Manifest) bool {
	for _, entry := range value.Entries {
		if entry.LoadMode == plugin.EntryLoadModeRemote || entry.Sandbox == plugin.EntrySandboxTrusted || entry.Component == "" {
			continue
		}
		component, exists := value.Component(entry.Component)
		if exists && component.Kind == plugin.ComponentKindFrontend {
			return true
		}
	}
	return false
}

func readFrontendEntryArtifact(pluginDir string) (*plugin.FrontendEntryArtifact, error) {
	path := filepath.Join(pluginDir, "frontend", frontendEntryArtifactFileName)
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取 frontend entry artifact %s: %w", path, err)
	}
	var artifact plugin.FrontendEntryArtifact
	if err := json.Unmarshal(body, &artifact); err != nil {
		return nil, fmt.Errorf("解析 frontend entry artifact %s: %w", path, err)
	}
	return &artifact, nil
}

// MergeFrontendEntryArtifact 将 Vite 构建产物绑定到唯一的本地 iframe entry。
func MergeFrontendEntryArtifact(value *plugin.Manifest, manifestPath string) error {
	if value == nil {
		return fmt.Errorf("插件协议 Manifest 不能为空")
	}
	if !RequiresFrontendEntryArtifact(*value) {
		return nil
	}
	artifact, err := readFrontendEntryArtifact(filepath.Dir(manifestPath))
	if err != nil || artifact == nil {
		return err
	}
	return applyFrontendEntryArtifact(value, artifact)
}

func applyFrontendEntryArtifact(value *plugin.Manifest, artifact *plugin.FrontendEntryArtifact) error {
	if value == nil {
		return fmt.Errorf("插件协议 Manifest 不能为空")
	}
	candidates := make([]int, 0, 1)
	for index, entry := range value.Entries {
		if entry.LoadMode == plugin.EntryLoadModeRemote || entry.Sandbox == plugin.EntrySandboxTrusted || entry.Component == "" {
			continue
		}
		component, exists := value.Component(entry.Component)
		if exists && component.Kind == plugin.ComponentKindFrontend {
			candidates = append(candidates, index)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	if len(candidates) > 1 {
		return fmt.Errorf("frontend entry artifact 要求 Manifest 恰好包含一个声明共享依赖的本地 entry，当前为 %d", len(candidates))
	}
	copyValue := *artifact
	copyValue.Styles = append([]string(nil), artifact.Styles...)
	copyValue.Files = cloneStringMap(artifact.Files)
	value.Entries[candidates[0]].Artifact = &copyValue
	return nil
}
