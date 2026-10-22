package scaffold

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/zane-xin/sparq-tool/internal/plugin/manifest"
)

var pluginNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

const DevExampleFileName = "sparq.dev.example.yaml"

// PluginOptions 描述插件脚手架的输入。
type PluginOptions struct {
	Name       string
	Title      string
	Dir        string
	Module     string
	Runtimes   []string
	Components []string
}

type pluginOptions struct {
	name       string
	title      string
	dir        string
	module     string
	runtimes   []string
	components []string
}

type pluginTemplateData struct {
	Name        string
	Title       string
	Module      string
	PackageName string
	EnvPrefix   string
	Runtimes    []string
	HasLocal    bool
	HasHTTP     bool
	HasFrontend bool
}

// Plugin 在指定目录生成可编译的最小插件骨架；Name 必须提供，Dir 为空时使用当前目录。
func Plugin(options PluginOptions) (string, error) {
	return initPlugin(pluginOptions{
		name:       strings.TrimSpace(options.Name),
		title:      strings.TrimSpace(options.Title),
		dir:        strings.TrimSpace(options.Dir),
		module:     strings.TrimSpace(options.Module),
		runtimes:   append([]string(nil), options.Runtimes...),
		components: append([]string(nil), options.Components...),
	})
}

func initPlugin(options pluginOptions) (string, error) {
	name := options.name
	if name == "" {
		return "", fmt.Errorf("插件 code 不能为空")
	}
	if !pluginNamePattern.MatchString(name) {
		return "", fmt.Errorf("插件 code %q 无效，必须匹配 ^[a-z][a-z0-9-]*$", name)
	}
	components := options.components
	if len(components) == 0 {
		components = []string{"go"}
	}
	if err := validateComponents(components); err != nil {
		return "", err
	}
	if !supportsComponent(components, "go") {
		return "", fmt.Errorf("当前初始化器要求包含 go 组件，纯前端和 Lua 插件需等待对应 Loader 接入")
	}

	target, err := pluginTarget(options.dir)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(target); err == nil {
		return "", fmt.Errorf("插件目标目录已存在，不会覆盖: %s", target)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("检查插件目标目录失败: %w", err)
	}
	runtimes := options.runtimes
	if len(runtimes) == 0 {
		runtimes = []string{"local", "http"}
	}
	if err := validateRuntimes(runtimes); err != nil {
		return "", err
	}
	title := options.title
	if title == "" {
		title = name
	}
	module := options.module
	if module == "" {
		module = "sparq.xin/sparq-plugins-" + name
	}
	data := pluginTemplateData{
		Name:        name,
		Title:       title,
		Module:      module,
		PackageName: packageName(name),
		EnvPrefix:   envPrefix(name),
		Runtimes:    runtimes,
		HasLocal:    supportsRuntime(runtimes, "local"),
		HasHTTP:     supportsRuntime(runtimes, "http"),
		HasFrontend: supportsComponent(components, "frontend"),
	}
	files := pluginFiles(data)
	if err := validatePluginTargets(target, files, data.HasFrontend); err != nil {
		return "", err
	}
	for relative, body := range files {
		path := filepath.Join(target, relative)
		if relative == DevExampleFileName {
			err = writeStaticFile(path, body)
		} else {
			err = writeTemplate(path, body, data)
		}
		if err != nil {
			return "", err
		}
	}
	generated, err := manifest.GenerateGo(manifest.GoGenerateOptions{
		ManifestPath: filepath.Join(target, manifest.DefaultFileName),
		PackageName:  data.PackageName,
	})
	if err != nil {
		return "", err
	}
	if err := manifest.WriteGo(generated); err != nil {
		return "", err
	}
	if data.HasFrontend {
		if _, err := initFrontend(frontendOptions{name: name, dir: filepath.Join(target, "frontend")}); err != nil {
			return "", err
		}
	}
	return target, nil
}

func pluginTarget(configured string) (string, error) {
	if configured == "" {
		var err error
		configured, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("读取当前目录失败: %w", err)
		}
	}
	target, err := filepath.Abs(configured)
	if err != nil {
		return "", fmt.Errorf("解析插件目录失败: %w", err)
	}
	return filepath.Clean(target), nil
}

func pluginFiles(data pluginTemplateData) map[string]string {
	files := map[string]string{
		"plugin.yaml":  pluginYAMLTemplate,
		".sparqignore": sourceIgnoreTemplate,
		"go.mod":       goModTemplate,
		"README.md":    readmeTemplate,
		"bootstrap.go": bootstrapTemplate,
		filepath.Join("app", "lifecycle", "register.go"): registerTemplate,
		filepath.Join("app", "lifecycle", "instance.go"): instanceTemplate,
		filepath.Join("app", "manifest", "manifest.go"):  manifestModuleTemplate,
	}
	if data.HasHTTP {
		files[filepath.Join("cmd", data.Name, "main.go")] = remoteMainTemplate
		files[DevExampleFileName] = sparqDevExampleTemplate
	}
	if data.HasFrontend {
		files["sparq.build.yaml"] = buildConfigTemplate
	}
	return files
}

func validatePluginTargets(target string, files map[string]string, hasFrontend bool) error {
	for relative := range files {
		path := filepath.Join(target, relative)
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("初始化文件已存在，不会覆盖: %s", path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("检查初始化文件 %s 失败: %w", path, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(target, manifest.DefaultGeneratedFileName)); err == nil {
		return fmt.Errorf("初始化文件已存在，不会覆盖: %s", filepath.Join(target, manifest.DefaultGeneratedFileName))
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Lstat(filepath.Join(target, "frontend")); hasFrontend && err == nil {
		return fmt.Errorf("前端目录已存在，不会覆盖: %s", filepath.Join(target, "frontend"))
	} else if hasFrontend && !os.IsNotExist(err) {
		return fmt.Errorf("检查前端目录失败: %w", err)
	}
	return nil
}

func supportsRuntime(runtimes []string, target string) bool {
	for _, runtime := range runtimes {
		if runtime == target {
			return true
		}
	}
	return false
}

func supportsComponent(components []string, target string) bool {
	for _, component := range components {
		if component == target {
			return true
		}
	}
	return false
}

func validateRuntimes(runtimes []string) error {
	seen := make(map[string]bool, len(runtimes))
	for _, runtime := range runtimes {
		if runtime != "local" && runtime != "http" {
			return fmt.Errorf("不支持的运行时 %q", runtime)
		}
		if seen[runtime] {
			return fmt.Errorf("运行时 %q 重复", runtime)
		}
		seen[runtime] = true
	}
	return nil
}

func validateComponents(components []string) error {
	seen := make(map[string]bool, len(components))
	for _, component := range components {
		if component != "go" && component != "frontend" {
			return fmt.Errorf("不支持的初始化组件 %q，当前只支持 go 和 frontend", component)
		}
		if seen[component] {
			return fmt.Errorf("初始化组件 %q 重复", component)
		}
		seen[component] = true
	}
	return nil
}

// ParseRuntimes 解析 CLI 形式的插件运行时列表。
func ParseRuntimes(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "both"
	}
	if value == "both" {
		return []string{"local", "http"}, nil
	}
	seen := map[string]bool{}
	output := []string{}
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		switch item {
		case "local", "http":
			if !seen[item] {
				seen[item] = true
				output = append(output, item)
			}
		default:
			return nil, fmt.Errorf("不支持的运行时 %q", item)
		}
	}
	if len(output) == 0 {
		return nil, fmt.Errorf("至少需要声明一个运行时")
	}
	return output, nil
}

// ParseComponents 解析初始化时需要生成的组件集合。
func ParseComponents(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "go"
	}
	seen := map[string]bool{}
	components := make([]string, 0, 2)
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		switch item {
		case "go", "frontend":
			if !seen[item] {
				seen[item] = true
				components = append(components, item)
			}
		default:
			return nil, fmt.Errorf("不支持的初始化组件 %q，当前只支持 go 和 frontend", item)
		}
	}
	if !seen["go"] {
		return nil, fmt.Errorf("当前初始化器要求 --with 包含 go")
	}
	return components, nil
}

func packageName(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	})
	return strings.Join(parts, "")
}

func envPrefix(name string) string {
	normalized := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r - 'a' + 'A'
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, name)
	return "SPARQ_" + strings.Trim(normalized, "_")
}

func writeTemplate(path string, body string, data pluginTemplateData) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tpl, err := template.New(filepath.Base(path)).Parse(body)
	if err != nil {
		return err
	}
	var buffer bytes.Buffer
	if err := tpl.Execute(&buffer, data); err != nil {
		return err
	}
	content := buffer.Bytes()
	if filepath.Ext(path) == ".go" {
		content, err = format.Source(content)
		if err != nil {
			return fmt.Errorf("格式化初始化文件 %s 失败: %w", path, err)
		}
	}
	return os.WriteFile(path, content, 0o644)
}

func writeStaticFile(path string, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
