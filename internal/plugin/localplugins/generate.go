package localplugins

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/zane-xin/sparq-protocol/plugin"
	"github.com/zane-xin/sparq-tool/internal/plugin/manifest"
)

// GenerateOptions 描述本地源码插件 Package 生成参数。
type GenerateOptions struct {
	Root   string
	AppDir string
	Output string
}

// Entry 描述一个 local 插件注册项。
type Entry struct {
	Code          string
	ComponentID   string
	Module        string
	ImportAlias   string
	Name          string
	Version       string
	Description   string
	DocsURL       string
	Path          string
	Authors       []manifest.Author
	Runtimes      []string
	Provides      []plugin.ProvideSpec
	Requires      []plugin.RequireSpec
	Entries       []plugin.EntryManifest
	Capabilities  []plugin.FrontendCapability
	Contributions plugin.ContributionManifest
}

type templateData struct {
	Entries []Entry
}

// GenerateResult 描述本地源码插件 Package 生成结果。
type GenerateResult struct {
	Output  string
	Content []byte
	Entries []Entry
}

// Generate 扫描本地插件并生成可信 Manifest 对应的 PluginPackage。
func Generate(options GenerateOptions) (GenerateResult, error) {
	root, err := filepath.Abs(defaultString(strings.TrimSpace(options.Root), "."))
	if err != nil {
		return GenerateResult{}, err
	}
	appDir := defaultString(strings.TrimSpace(options.AppDir), "sparq-app")
	output := strings.TrimSpace(options.Output)
	if output == "" {
		output = filepath.Join(root, appDir, "cmd", "sparq", "plugins_gen.go")
	}
	entries, err := discover(root, true)
	if err != nil {
		return GenerateResult{}, err
	}
	content, err := render(entries)
	if err != nil {
		return GenerateResult{}, err
	}
	return GenerateResult{
		Output:  output,
		Content: content,
		Entries: entries,
	}, nil
}

// DiscoverOptions 描述本地源码插件发现参数。
type DiscoverOptions struct {
	Root string
}

// Discover 从 go.work use 项中发现支持 local runtime 的插件源码目录。
func Discover(options DiscoverOptions) ([]Entry, error) {
	root, err := filepath.Abs(defaultString(strings.TrimSpace(options.Root), "."))
	if err != nil {
		return nil, err
	}
	return discover(root, false)
}

func discover(root string, checkGeneratedManifest bool) ([]Entry, error) {
	dirs, err := readGoWorkUseDirs(root)
	if err != nil {
		return nil, err
	}
	return scanPlugins(root, dirs, checkGeneratedManifest)
}

// Write 将生成结果写入目标文件。
func Write(result GenerateResult) error {
	if strings.TrimSpace(result.Output) == "" {
		return fmt.Errorf("输出文件不能为空")
	}
	if err := os.MkdirAll(filepath.Dir(result.Output), 0o755); err != nil {
		return err
	}
	return os.WriteFile(result.Output, result.Content, 0o644)
}

func scanPlugins(root string, dirs []string, checkGeneratedManifest bool) ([]Entry, error) {
	var entries []Entry
	for _, dir := range dirs {
		manifestPath := filepath.Join(dir, manifest.DefaultFileName)
		if !exists(manifestPath) {
			continue
		}
		pluginManifest, err := manifest.Load(manifestPath)
		if err != nil {
			return nil, err
		}
		protocolManifest, err := pluginManifest.ProtocolManifest()
		if err != nil {
			return nil, fmt.Errorf("转换插件清单 %s: %w", manifestPath, err)
		}
		componentID, local, err := localComponent(protocolManifest)
		if err != nil {
			return nil, fmt.Errorf("检查插件 %s local component: %w", manifestPath, err)
		}
		if !local {
			continue
		}
		if checkGeneratedManifest {
			generatedManifest, err := manifest.GenerateGo(manifest.GoGenerateOptions{ManifestPath: manifestPath})
			if err != nil {
				return nil, err
			}
			if err := manifest.CheckGo(generatedManifest); err != nil {
				return nil, err
			}
		}
		module, err := readModule(filepath.Join(dir, "go.mod"))
		if err != nil {
			return nil, err
		}
		relativePath, err := filepath.Rel(root, dir)
		if err != nil {
			return nil, err
		}
		entries = append(entries, Entry{
			Code: pluginManifest.Metadata.Code, ComponentID: componentID, Module: module,
			ImportAlias: strings.ReplaceAll(pluginManifest.Metadata.Code, "-", "_") + "plugin",
			Name:        pluginManifest.Metadata.Name, Version: pluginManifest.Metadata.Version,
			Description: pluginManifest.Metadata.Description, DocsURL: pluginManifest.Metadata.Docs, Path: filepath.ToSlash(relativePath),
			Authors:  pluginManifest.Metadata.Authors,
			Runtimes: pluginManifest.Spec.Runtime.Supported, Entries: pluginManifest.Spec.Entries,
			Provides: protocolManifest.Provides, Requires: protocolManifest.Requires,
			Capabilities: pluginManifest.Spec.Capabilities, Contributions: protocolManifest.Contributions,
		})
	}
	sort.Slice(entries, func(i int, j int) bool {
		return entries[i].Code < entries[j].Code
	})
	return entries, nil
}

func render(entries []Entry) ([]byte, error) {
	tpl, err := template.New("localplugins").Funcs(template.FuncMap{
		"quote":      func(value any) string { return fmt.Sprintf("%q", value) },
		"quoteBytes": func(value []byte) string { return fmt.Sprintf("%q", string(value)) },
	}).Parse(localpluginsTemplate)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	if err := tpl.Execute(&buffer, templateData{Entries: entries}); err != nil {
		return nil, err
	}
	content, err := format.Source(buffer.Bytes())
	if err != nil {
		return nil, fmt.Errorf("格式化本地源码插件入口失败: %w", err)
	}
	return content, nil
}

func supportsLocal(runtimes []string) bool {
	for _, runtime := range runtimes {
		if strings.TrimSpace(runtime) == "local" {
			return true
		}
	}
	return false
}

func localComponent(manifest plugin.Manifest) (string, bool, error) {
	if len(manifest.Components) == 0 {
		return "", supportsRuntime(manifest.Runtime, plugin.RuntimeLocal), nil
	}
	componentID := ""
	for _, component := range manifest.Components {
		language := strings.ToLower(strings.TrimSpace(component.Language))
		if (language != "" && language != "go") || !supportsRuntime(component.Runtime, plugin.RuntimeLocal) {
			continue
		}
		if componentID != "" {
			return "", false, fmt.Errorf("多个 Go local component 需要显式 package assembly")
		}
		componentID = string(component.ID)
	}
	return componentID, componentID != "", nil
}

func supportsRuntime(runtimes []plugin.RuntimeKind, target plugin.RuntimeKind) bool {
	for _, runtime := range runtimes {
		if runtime == target {
			return true
		}
	}
	return false
}

func readModule(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			module := strings.TrimSpace(strings.TrimPrefix(line, "module "))
			if module == "" {
				break
			}
			return module, nil
		}
	}
	return "", fmt.Errorf("go.mod 缺少 module 声明: %s", path)
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
