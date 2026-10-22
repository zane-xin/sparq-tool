package build

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/zane-xin/sparq-tool/internal/toolchain"
	"gopkg.in/yaml.v3"
)

var buildEnvironmentPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// BuildConfig 描述一个应用构建计划；应用类型由路径和 package 决定。
type BuildConfig struct {
	Version   string                 `yaml:"version"`
	GitHash   string                 `yaml:"git_hash"`
	App       BuildGoComponent       `yaml:"app"`
	Updater   BuildGoComponent       `yaml:"updater"`
	Admin     BuildFrontendComponent `yaml:"admin"`
	Frontend  BuildFrontendComponent `yaml:"frontend"`
	Copy      []BuildCopyEntry       `yaml:"copy"`
	Output    BuildOutput            `yaml:"output"`
	Toolchain toolchain.Toolchain    `yaml:"toolchain"`
	baseDir   string
}

// BuildGoComponent 描述需要由 Go 编译的组件。Path 为空表示不参与构建。
type BuildGoComponent struct {
	Path     string   `yaml:"path"`
	Package  string   `yaml:"package"`
	Binary   string   `yaml:"binary"`
	Commands []string `yaml:"commands"`
}

// BuildFrontendComponent 描述需要由前端项目生成 dist 的组件。
type BuildFrontendComponent struct {
	Path     string   `yaml:"path"`
	Commands []string `yaml:"commands"`
}

// BuildCopyEntry 描述需要从 app.path 显式发布到输出目录的运行文件或目录。
type BuildCopyEntry struct {
	Source string `yaml:"source"`
	Target string `yaml:"target"`
}

// BuildOutput 描述所有构建产物的根目录。
type BuildOutput struct {
	Dir string `yaml:"dir"`
}

// LoadBuildConfig 按指定文件读取构建配置。
func LoadBuildConfig(path string) (BuildConfig, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return BuildConfig{}, fmt.Errorf("构建配置路径不能为空")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return BuildConfig{}, fmt.Errorf("解析构建配置路径失败: %w", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return BuildConfig{}, fmt.Errorf("读取构建配置 %s 失败: %w", path, err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	var config BuildConfig
	if err := decoder.Decode(&config); err != nil {
		return BuildConfig{}, fmt.Errorf("解析构建配置 %s 失败: %w", path, err)
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); err == nil {
		return BuildConfig{}, fmt.Errorf("构建配置 %s 只能包含一个 YAML 文档", path)
	} else if err != io.EOF {
		return BuildConfig{}, fmt.Errorf("解析构建配置 %s 失败: %w", path, err)
	}
	config.baseDir = filepath.Dir(path)
	if err := config.Validate(); err != nil {
		return BuildConfig{}, fmt.Errorf("校验构建配置 %s 失败: %w", path, err)
	}
	return config, nil
}

// ResolveBuildConfigPath 根据环境名选择 build.<env>.yaml；未指定环境时使用原路径。
func ResolveBuildConfigPath(path, environment string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "build.yaml"
	}
	environment = strings.TrimSpace(environment)
	if environment == "" {
		return path, nil
	}
	if !buildEnvironmentPattern.MatchString(environment) {
		return "", fmt.Errorf("构建环境名称无效 %q，必须匹配 ^[a-z][a-z0-9-]*$", environment)
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	if ext == "" {
		ext = ".yaml"
	}
	resolved := base + "." + environment + ext
	if _, err := os.Stat(resolved); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("构建环境配置不存在: %s", resolved)
		}
		return "", fmt.Errorf("检查构建环境配置 %s 失败: %w", resolved, err)
	}
	return resolved, nil
}

func (config BuildConfig) BaseDir() string { return config.baseDir }

// Validate 校验路径和组件的基本结构，不访问外部工具。
func (config BuildConfig) Validate() error {
	if strings.TrimSpace(config.App.Path) == "" {
		return fmt.Errorf("app.path 不能为空")
	}
	if strings.TrimSpace(config.App.Package) == "" {
		return fmt.Errorf("app.package 不能为空")
	}
	if strings.TrimSpace(config.App.Binary) == "" {
		return fmt.Errorf("app.binary 不能为空")
	}
	if strings.TrimSpace(config.Output.Dir) == "" {
		return fmt.Errorf("output.dir 不能为空")
	}
	for name, component := range map[string]BuildGoComponent{"app": config.App, "updater": config.Updater} {
		if strings.TrimSpace(component.Path) == "" {
			continue
		}
		if strings.TrimSpace(component.Package) == "" {
			return fmt.Errorf("%s.package 不能为空", name)
		}
		if strings.TrimSpace(component.Binary) == "" {
			return fmt.Errorf("%s.binary 不能为空", name)
		}
		for _, command := range component.Commands {
			if strings.TrimSpace(command) == "" {
				return fmt.Errorf("%s.commands 不能包含空值", name)
			}
		}
	}
	for index, directory := range config.Copy {
		if strings.TrimSpace(directory.Source) == "" {
			return fmt.Errorf("copy[%d].source 不能为空", index)
		}
		if filepath.IsAbs(directory.Source) || escapesRelativePath(directory.Source) {
			return fmt.Errorf("copy[%d].source 必须是 app.path 内的相对路径", index)
		}
		if strings.TrimSpace(directory.Target) == "" {
			return fmt.Errorf("copy[%d].target 不能为空", index)
		}
		if filepath.IsAbs(directory.Target) || escapesRelativePath(directory.Target) {
			return fmt.Errorf("copy[%d].target 必须是输出目录内的相对路径", index)
		}
	}
	return nil
}

func escapesRelativePath(value string) bool {
	cleaned := filepath.Clean(strings.TrimSpace(value))
	return cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator))
}

func (config BuildConfig) resolvePath(value string) string {
	value = strings.TrimSpace(value)
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(config.baseDir, value)
}
