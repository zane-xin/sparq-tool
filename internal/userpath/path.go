package userpath

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	Environment           = "SPARQ_HOME"
	DefaultRootDisplay    = "~/.sparq"
	DefaultToolDirDisplay = DefaultRootDisplay + "/sparq-tool"
)

// Root 返回当前用户的 Sparq 产品级数据根目录。
func Root() (string, error) {
	root := strings.TrimSpace(os.Getenv(Environment))
	if root == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("读取用户主目录失败: %w", err)
		}
		root = filepath.Join(userHome, ".sparq")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("解析 %s 目录失败: %w", Environment, err)
	}
	return root, nil
}

// ToolDir 返回 sparq-tool 的用户级数据目录。
func ToolDir() (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "sparq-tool"), nil
}
