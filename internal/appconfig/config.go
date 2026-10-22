package appconfig

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/zane-xin/sparq-tool/internal/toolchain"
	"gopkg.in/yaml.v3"
)

const (
	FrontendModeSource      = "source"
	FrontendModeSourceDev   = "source-dev"
	FrontendModeSourceBuild = "source-build"
	FrontendModeArtifact    = "artifact"
	FrontendModeDisabled    = "disabled"
)

// Config 描述二次开发应用的构建与本地联调配置。
type Config struct {
	Target    TargetConfig        `yaml:"target"`
	Backend   BackendConfig       `yaml:"backend"`
	App       AppConfig           `yaml:"app"`
	Frontend  FrontendConfig      `yaml:"frontend"`
	Toolchain toolchain.Toolchain `yaml:"toolchain"`
	baseDir   string
}

type TargetConfig struct {
	Kind string `yaml:"kind"`
}

type AppConfig struct {
	Dir     string `yaml:"dir"`
	Package string `yaml:"package"`
	Binary  string `yaml:"binary"`
	Output  string `yaml:"output"`
}

type BackendConfig struct {
	WatchFile string `yaml:"watch_file"`
}

type FrontendConfig struct {
	Mode    string            `yaml:"mode"`
	Command string            `yaml:"command"`
	Script  string            `yaml:"script"`
	CWD     string            `yaml:"cwd"`
	URL     string            `yaml:"url"`
	Health  string            `yaml:"health"`
	Env     map[string]string `yaml:"env"`
	Timeout string            `yaml:"timeout"`
	Stop    StopConfig        `yaml:"stop"`
}

type StopConfig struct {
	GraceTimeout string `yaml:"grace_timeout"`
	KillTimeout  string `yaml:"kill_timeout"`
}

// Load 读取并严格解析应用配置。
func Load(path string) (Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	config, err := Parse(content)
	if err != nil {
		return Config{}, fmt.Errorf("解析应用配置 %s 失败: %w", path, err)
	}
	config.baseDir, err = filepath.Abs(filepath.Dir(path))
	if err != nil {
		return Config{}, fmt.Errorf("解析应用配置目录失败: %w", err)
	}
	return config, nil
}

// Parse 严格解析单个 YAML 文档。
func Parse(content []byte) (Config, error) {
	var config Config
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return Config{}, err
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); err == nil {
		return Config{}, fmt.Errorf("只能包含一个 YAML 文档")
	} else if err != io.EOF {
		return Config{}, err
	}
	return config, nil
}

// BaseDir 返回配置文件所在的绝对目录。
func (c Config) BaseDir() string {
	if c.baseDir == "" {
		return "."
	}
	return c.baseDir
}

// FrontendMode 返回包含默认值的前端来源模式。
func (c Config) FrontendMode() string {
	if c.Frontend.Mode == "" {
		return FrontendModeSource
	}
	return c.Frontend.Mode
}

// ValidateCommon 校验构建与开发共享的应用配置约束。
func (c Config) ValidateCommon() error {
	if c.Target.Kind != "" && c.Target.Kind != "application" {
		return fmt.Errorf("target.kind 不支持: %s", c.Target.Kind)
	}
	switch c.FrontendMode() {
	case FrontendModeSource, FrontendModeSourceDev, FrontendModeSourceBuild, FrontendModeArtifact, FrontendModeDisabled:
		return nil
	default:
		return fmt.Errorf("frontend.mode 不支持: %s", c.FrontendMode())
	}
}
