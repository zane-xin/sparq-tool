package toolchain

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/zane-xin/sparq-tool/internal/userpath"
	"gopkg.in/yaml.v3"
)

// Spec 描述一个可覆盖的外部工具命令。
type Spec struct {
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
}

// Config 描述用户级和项目级共用的工具链默认值。
type Config struct {
	Toolchain Toolchain `yaml:"toolchain"`
}

// Toolchain 描述构建和开发使用的外部工具。
type Toolchain struct {
	Go       Spec `yaml:"go"`
	Frontend Spec `yaml:"frontend"`
}

// Defaults 返回不依赖宿主机目录的内置默认值。
func Defaults() Toolchain {
	return Toolchain{
		Go:       Spec{Command: "go"},
		Frontend: Spec{Command: "pnpm"},
	}
}

// LoadUser 读取用户级工具链配置。文件不存在时返回内置默认值。
func LoadUser() (Toolchain, string, error) {
	root, err := userpath.ToolDir()
	if err != nil {
		return Toolchain{}, "", err
	}
	path := filepath.Join(root, "config.yaml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Defaults(), path, nil
	}
	if err != nil {
		return Toolchain{}, path, fmt.Errorf("读取用户工具链配置 %s 失败: %w", path, err)
	}
	var config Config
	if err := decode(data, &config); err != nil {
		return Toolchain{}, path, fmt.Errorf("解析用户工具链配置 %s 失败: %w", path, err)
	}
	return Merge(Defaults(), config.Toolchain), path, nil
}

// Merge 将 override 中已配置的工具覆盖到 base；参数数组整体替换。
func Merge(base, override Toolchain) Toolchain {
	if override.Go.Command != "" || override.Go.Args != nil {
		base.Go = mergeSpec(base.Go, override.Go)
	}
	if override.Frontend.Command != "" || override.Frontend.Args != nil {
		base.Frontend = mergeSpec(base.Frontend, override.Frontend)
	}
	return base
}

func mergeSpec(base, override Spec) Spec {
	if override.Command != "" {
		base.Command = override.Command
	}
	if override.Args != nil {
		base.Args = append([]string(nil), override.Args...)
	}
	return base
}

func decode(data []byte, target any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); err == nil {
		return fmt.Errorf("只能包含一个 YAML 文档")
	} else if err != io.EOF {
		return err
	}
	return nil
}
