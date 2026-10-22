package localplugins

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func readGoWorkUseDirs(root string) ([]string, error) {
	path := filepath.Join(root, "go.work")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("工作区根目录缺少 go.work: %s", root)
		}
		return nil, err
	}
	entries, err := parseGoWorkUsePaths(string(data))
	if err != nil {
		return nil, fmt.Errorf("解析 go.work use 项失败: %w", err)
	}
	dirs := make([]string, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		dir := entry
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(root, filepath.FromSlash(entry))
		}
		dir = filepath.Clean(dir)
		if _, exists := seen[dir]; exists {
			continue
		}
		seen[dir] = struct{}{}
		dirs = append(dirs, dir)
	}
	return dirs, nil
}

func parseGoWorkUsePaths(content string) ([]string, error) {
	paths := []string{}
	inUseBlock := false
	for _, raw := range strings.Split(content, "\n") {
		line := stripLineComment(raw)
		if line == "" {
			continue
		}
		if inUseBlock {
			if line == ")" {
				inUseBlock = false
				continue
			}
			path, err := cleanUsePath(line)
			if err != nil {
				return nil, err
			}
			paths = append(paths, path)
			continue
		}
		if line == "use (" {
			inUseBlock = true
			continue
		}
		if strings.HasPrefix(line, "use ") {
			path, err := cleanUsePath(strings.TrimSpace(strings.TrimPrefix(line, "use ")))
			if err != nil {
				return nil, err
			}
			paths = append(paths, path)
		}
	}
	if inUseBlock {
		return nil, fmt.Errorf("use 块缺少结束括号")
	}
	return paths, nil
}

func cleanUsePath(value string) (string, error) {
	if value == "" || value == ")" {
		return "", fmt.Errorf("use 路径不能为空")
	}
	if strings.HasPrefix(value, "\"") || strings.HasPrefix(value, "`") {
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return "", err
		}
		value = unquoted
	}
	if strings.Contains(value, "\\") {
		return "", fmt.Errorf("use 路径必须使用正斜杠: %s", value)
	}
	return value, nil
}

func stripLineComment(value string) string {
	index := strings.Index(value, "//")
	if index >= 0 {
		value = value[:index]
	}
	return strings.TrimSpace(value)
}
