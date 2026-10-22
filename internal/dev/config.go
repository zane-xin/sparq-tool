package dev

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config 描述 sparq.dev.yaml 开发配置。
type Config struct {
	App      AppConfig      `yaml:"app"`
	Plugin   PluginConfig   `yaml:"plugin"`
	Frontend FrontendConfig `yaml:"frontend"`
	baseDir  string
}

// AppConfig 描述宿主 app 开发配置。
type AppConfig struct {
	Command   string `yaml:"command"`
	CWD       string `yaml:"cwd"`
	Endpoint  string `yaml:"endpoint"`
	DevSecret string `yaml:"dev_secret"`
	Health    string `yaml:"health"`
	Timeout   string `yaml:"timeout"`
}

// PluginConfig 描述插件后端开发配置。
type PluginConfig struct {
	ID        string            `yaml:"id"`
	Component string            `yaml:"component"`
	Manifest  string            `yaml:"manifest"`
	Endpoint  string            `yaml:"endpoint"`
	Command   string            `yaml:"command"`
	CWD       string            `yaml:"cwd"`
	Health    string            `yaml:"health"`
	Secret    string            `yaml:"secret"`
	Env       map[string]string `yaml:"env"`
	Timeout   string            `yaml:"timeout"`
	Reload    ReloadConfig      `yaml:"reload"`
}

// ReloadConfig 描述插件后端在开发会话中的文件监听和构建检查策略。
type ReloadConfig struct {
	Enabled    bool     `yaml:"enabled"`
	Paths      []string `yaml:"paths"`
	Extensions []string `yaml:"extensions"`
	Files      []string `yaml:"files"`
	Exclude    []string `yaml:"exclude"`
	Debounce   string   `yaml:"debounce"`
	Build      string   `yaml:"build"`
}

// FrontendConfig 描述插件前端开发配置。
type FrontendConfig struct {
	Enabled bool              `yaml:"enabled"`
	Command string            `yaml:"command"`
	CWD     string            `yaml:"cwd"`
	URL     string            `yaml:"url"`
	Health  string            `yaml:"health"`
	Env     map[string]string `yaml:"env"`
	Timeout string            `yaml:"timeout"`
}

// LoadConfig 读取 sparq.dev.yaml。
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	config, err := parseConfig(data)
	if err != nil {
		return Config{}, fmt.Errorf("解析开发配置 %s 失败: %w", path, err)
	}
	config.baseDir, err = filepath.Abs(filepath.Dir(path))
	if err != nil {
		return Config{}, fmt.Errorf("解析开发配置目录失败: %w", err)
	}
	return config, nil
}

// ParseConfig 解析 dev 配置；解析失败时返回零值，兼容仅需要读取配置的调用方。
func ParseConfig(data []byte) Config {
	config, _ := parseConfig(data)
	return config
}

func parseConfig(data []byte) (Config, error) {
	var config Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
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
