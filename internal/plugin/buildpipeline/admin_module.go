package buildpipeline

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	frontendRootSourcePath        = "src/frontend.ts"
	frontendArtifactName          = "artifact.json"
	frontendBuildDescriptorName   = ".vite/sparq-root.json"
	frontendBuildDescriptorSchema = "sparq.frontend-build/v1"
)

type frontendBuildDescriptor struct {
	Schema string   `json:"schema"`
	Entry  string   `json:"entry"`
	Styles []string `json:"styles"`
}

type generatedFrontendArtifact struct {
	Icons   map[string]string `json:"icons,omitempty"`
	Version string            `json:"version"`
	Entry   string            `json:"entry"`
	Styles  []string          `json:"styles,omitempty"`
	Files   map[string]string `json:"files"`
}

func generateFrontendArtifact(frontendRoot, outputDir string) (string, error) {
	descriptor, err := readFrontendBuildDescriptor(outputDir)
	if err != nil {
		return "", err
	}
	packageVersion, err := readFrontendPackageVersion(filepath.Join(frontendRoot, "package.json"))
	if err != nil {
		return "", err
	}
	files, err := digestFrontendRootFiles(outputDir)
	if err != nil {
		return "", err
	}
	if err := validateFrontendBuildDescriptor(descriptor, files); err != nil {
		return "", err
	}
	icons, err := readFrontendIcons(outputDir, files)
	if err != nil {
		return "", err
	}
	artifact := generatedFrontendArtifact{
		Icons:   icons,
		Version: packageVersion + "-" + moduleVersionDigest(files),
		Entry:   descriptor.Entry,
		Styles:  descriptor.Styles,
		Files:   files,
	}
	body, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return "", fmt.Errorf("编码 frontend artifact 失败: %w", err)
	}
	path := filepath.Join(frontendRoot, frontendArtifactName)
	if err := writeAtomic(path, append(body, '\n')); err != nil {
		return "", fmt.Errorf("写入 frontend artifact 失败: %w", err)
	}
	return path, nil
}

func readFrontendBuildDescriptor(outputDir string) (frontendBuildDescriptor, error) {
	descriptorPath := filepath.Join(outputDir, filepath.FromSlash(frontendBuildDescriptorName))
	body, err := os.ReadFile(descriptorPath)
	if err != nil {
		if os.IsNotExist(err) {
			return frontendBuildDescriptor{}, fmt.Errorf(
				"插件前端构建缺少 %s，请在 Vite 配置中启用 Sparq Trusted Root 插件并使用 %s",
				frontendBuildDescriptorName,
				frontendRootSourcePath,
			)
		}
		return frontendBuildDescriptor{}, fmt.Errorf("读取 Trusted Root 构建描述失败: %w", err)
	}
	var descriptor frontendBuildDescriptor
	if err := json.Unmarshal(body, &descriptor); err != nil {
		return frontendBuildDescriptor{}, fmt.Errorf("解析 Trusted Root 构建描述 %s 失败: %w", frontendBuildDescriptorName, err)
	}
	return descriptor, nil
}

func validateFrontendBuildDescriptor(descriptor frontendBuildDescriptor, files map[string]string) error {
	if descriptor.Schema != frontendBuildDescriptorSchema {
		return fmt.Errorf(
			"Trusted Root 构建描述 schema 必须为 %q，实际为 %q",
			frontendBuildDescriptorSchema,
			descriptor.Schema,
		)
	}
	if err := validateFrontendBuildPath("entry", descriptor.Entry, ".mjs"); err != nil {
		return err
	}
	if _, exists := files[descriptor.Entry]; !exists {
		return fmt.Errorf("Trusted Root 构建描述 entry 对应文件不存在: %s", descriptor.Entry)
	}

	declaredStyles := make(map[string]struct{}, len(descriptor.Styles))
	for _, style := range descriptor.Styles {
		if err := validateFrontendBuildPath("styles", style, ".css"); err != nil {
			return err
		}
		if _, exists := declaredStyles[style]; exists {
			return fmt.Errorf("Trusted Root 构建描述 styles 路径重复: %s", style)
		}
		if _, exists := files[style]; !exists {
			return fmt.Errorf("Trusted Root 构建描述 style 对应文件不存在: %s", style)
		}
		declaredStyles[style] = struct{}{}
	}

	undeclaredStyles := make([]string, 0)
	for file := range files {
		if filepath.Ext(file) != ".css" {
			continue
		}
		if _, declared := declaredStyles[file]; !declared {
			undeclaredStyles = append(undeclaredStyles, file)
		}
	}
	if len(undeclaredStyles) > 0 {
		sort.Strings(undeclaredStyles)
		return fmt.Errorf("Trusted Root 构建描述遗漏 CSS 文件: %s", strings.Join(undeclaredStyles, ", "))
	}
	return nil
}

func validateFrontendBuildPath(field, value, extension string) error {
	if value == "" || strings.Contains(value, `\`) || !fs.ValidPath(value) {
		return fmt.Errorf("Trusted Root 构建描述 %s 必须是安全的相对路径: %q", field, value)
	}
	if filepath.Ext(value) != extension {
		return fmt.Errorf("Trusted Root 构建描述 %s 必须指向 %s 文件: %q", field, extension, value)
	}
	return nil
}

func digestFrontendRootFiles(outputDir string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(outputDir, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".vite" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("frontend root 只能包含普通文件: %s", current)
		}
		body, err := os.ReadFile(current)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(outputDir, current)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(body)
		files[filepath.ToSlash(relative)] = "sha256-" + base64.StdEncoding.EncodeToString(digest[:])
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("计算 frontend 文件摘要失败: %w", err)
	}
	return files, nil
}

func moduleVersionDigest(files map[string]string) string {
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var source strings.Builder
	for _, key := range keys {
		source.WriteString(key)
		source.WriteByte('\n')
		source.WriteString(files[key])
		source.WriteByte('\n')
	}
	digest := sha256.Sum256([]byte(source.String()))
	return hex.EncodeToString(digest[:])[:12]
}

func readFrontendPackageVersion(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取前端 package.json 失败: %w", err)
	}
	var metadata struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &metadata); err != nil {
		return "", fmt.Errorf("解析前端 package.json 失败: %w", err)
	}
	if strings.TrimSpace(metadata.Version) == "" {
		return "", fmt.Errorf("前端 package.json 缺少 version")
	}
	return metadata.Version, nil
}
