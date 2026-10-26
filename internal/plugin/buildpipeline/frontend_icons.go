package buildpipeline

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zane-xin/sparq-protocol/plugin"
)

// readFrontendIcons 在摘要计算后验证声明和 SVG，未声明时兼容既有插件。
func readFrontendIcons(outputDir string, files map[string]string) (map[string]string, error) {
	body, err := os.ReadFile(filepath.Join(outputDir, ".vite", "sparq-icons.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var descriptor struct {
		Schema string            `json:"schema"`
		Icons  map[string]string `json:"icons"`
	}
	if err := json.Unmarshal(body, &descriptor); err != nil {
		return nil, fmt.Errorf("解析 SVG 图标构建描述: %w", err)
	}
	if descriptor.Schema != "sparq.frontend-icons/v1" {
		return nil, fmt.Errorf("SVG 图标描述 schema 无效: %s", descriptor.Schema)
	}
	declared := map[string]string{}
	for _, file := range descriptor.Icons {
		declared[file] = "pending"
	}
	if err := plugin.ValidateFrontendIcons(descriptor.Icons, declared); err != nil {
		return nil, err
	}
	// iframe 的 Vite assets 清单不包含 public/ 下 SVG，按声明补入摘要清单。
	for _, relative := range descriptor.Icons {
		root, err := filepath.EvalSymlinks(outputDir)
		if err != nil {
			return nil, err
		}
		path := filepath.Join(root, filepath.FromSlash(relative))
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return nil, fmt.Errorf("读取 SVG 图标 %s: %w", relative, err)
		}
		absolute, err := filepath.Abs(path)
		if err != nil || resolved != absolute {
			return nil, fmt.Errorf("SVG 图标不能使用符号链接: %s", relative)
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > plugin.MaxFrontendIconBytes {
			return nil, fmt.Errorf("SVG 图标不是有效普通文件: %s", relative)
		}
		svg, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if err := plugin.ValidateFrontendSVG(svg); err != nil {
			return nil, fmt.Errorf("SVG 图标 %s: %w", relative, err)
		}
		digest := sha256.Sum256(svg)
		files[relative] = "sha256-" + base64.StdEncoding.EncodeToString(digest[:])
	}
	if err := plugin.ValidateFrontendIcons(descriptor.Icons, files); err != nil {
		return nil, err
	}
	return descriptor.Icons, nil
}
