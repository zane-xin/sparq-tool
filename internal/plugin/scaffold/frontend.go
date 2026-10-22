package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zane-xin/sparq-tool/internal/plugin/manifest"
)

// FrontendOptions 描述插件前端脚手架输入。
type FrontendOptions struct {
	Name string
	Dir  string
}

type frontendOptions struct {
	name string
	dir  string
}

type frontendTemplateData struct {
	Name string
}

// Frontend 生成插件前端脚手架。
func Frontend(options FrontendOptions) (string, error) {
	return initFrontend(frontendOptions{
		name: strings.TrimSpace(options.Name),
		dir:  strings.TrimSpace(options.Dir),
	})
}

func initFrontend(options frontendOptions) (string, error) {
	if !pluginNamePattern.MatchString(options.name) {
		return "", fmt.Errorf("插件名称必须匹配 ^[a-z][a-z0-9-]*$")
	}
	target := options.dir
	if target == "" {
		target = filepath.Join(".", "frontend")
	}
	target = filepath.Clean(target)
	pluginManifestPath := filepath.Join(filepath.Dir(target), manifest.DefaultFileName)
	pluginManifest, err := manifest.Load(pluginManifestPath)
	if err != nil {
		return "", fmt.Errorf("读取插件清单 %s 失败: %w", pluginManifestPath, err)
	}
	if pluginManifest.Metadata.Code != options.name {
		return "", fmt.Errorf(
			"插件清单 %s 的 metadata.code 为 %q，与前端名称 %q 不一致",
			pluginManifestPath,
			pluginManifest.Metadata.Code,
			options.name,
		)
	}
	notEmpty, err := directoryNotEmpty(target)
	if err != nil {
		return "", err
	}
	if notEmpty {
		return "", fmt.Errorf("目标目录非空: %s", target)
	}
	data := frontendTemplateData{
		Name: options.name,
	}
	files := map[string]string{
		"package.json":                       frontendPackageTemplate,
		"tsconfig.json":                      frontendTSConfigTemplate,
		"vite.config.ts":                     frontendViteConfigTemplate,
		"README.md":                          frontendReadmeTemplate,
		"index.html":                         frontendIndexTemplate,
		filepath.Join("src", "App.vue"):      frontendAppTemplate,
		filepath.Join("src", "frontend.ts"):  frontendAdminExtensionTemplate,
		filepath.Join("src", "main.ts"):      frontendEntryTemplate,
		filepath.Join("src", "bootstrap.ts"): frontendBootstrapTemplate,
		filepath.Join("src", "style.scss"):   frontendStyleTemplate,
	}
	for relative, body := range files {
		path := filepath.Join(target, relative)
		if err := writeFrontendTemplate(path, body, data); err != nil {
			return "", err
		}
	}
	return target, nil
}

func directoryNotEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("读取目标目录 %s 失败: %w", path, err)
	}
	return len(entries) > 0, nil
}

func writeFrontendTemplate(path string, body string, data frontendTemplateData) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建前端目录 %s 失败: %w", filepath.Dir(path), err)
	}
	rendered := strings.ReplaceAll(body, "{{name}}", data.Name)
	if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("写入前端文件 %s 失败: %w", path, err)
	}
	return nil
}
