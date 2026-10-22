package buildpipeline

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/zane-xin/sparq-tool/internal/plugin/manifest"
)

// Platform 描述 Remote Runner 的目标 GOOS/GOARCH。
type Platform struct {
	OS   string
	Arch string
}

// PlatformBinary 是发布矩阵中的单个平台二进制。
type PlatformBinary struct {
	Platform Platform
	Path     string
}

// PlatformOptions 描述发布阶段需要生成的 Go 平台矩阵。
type PlatformOptions struct {
	Dir       string
	Platforms []Platform
}

type platformCommandRunner func(context.Context, string, []string, string, ...string) ([]byte, error)

// DefaultPublishPlatforms 返回初期 Marketplace 支持的完整 Go 交付矩阵。
func DefaultPublishPlatforms() []Platform {
	return []Platform{
		{OS: "linux", Arch: "amd64"},
		{OS: "linux", Arch: "arm64"},
		{OS: "darwin", Arch: "amd64"},
		{OS: "darwin", Arch: "arm64"},
		{OS: "windows", Arch: "amd64"},
		{OS: "windows", Arch: "arm64"},
	}
}

// BuildPlatforms 为已有 Remote Runner 生成独立的跨平台二进制；Go 测试和 Manifest 导出由 Build 先行完成。
func BuildPlatforms(ctx context.Context, options PlatformOptions) ([]PlatformBinary, error) {
	return buildPlatforms(ctx, options, runPlatformCommand)
}

func buildPlatforms(ctx context.Context, options PlatformOptions, run platformCommandRunner) ([]PlatformBinary, error) {
	root, err := filepath.Abs(defaultValue(strings.TrimSpace(options.Dir), "."))
	if err != nil {
		return nil, fmt.Errorf("解析插件目录失败: %w", err)
	}
	item, err := manifestForPlatformBuild(root)
	if err != nil {
		return nil, err
	}
	settings, err := loadConfig(root)
	if err != nil {
		return nil, err
	}
	if !enabled(settings.Go.Enabled, regularFile(filepath.Join(root, "go.mod"))) {
		return []PlatformBinary{}, nil
	}
	entry := strings.TrimSpace(settings.Go.RemoteEntry)
	if entry == "" {
		entry = filepath.ToSlash(filepath.Join("cmd", item.Metadata.Code))
	}
	entryPath, err := resolveRelative(root, entry, "go.remote_entry")
	if err != nil {
		return nil, err
	}
	if !directory(entryPath) {
		return []PlatformBinary{}, nil
	}
	platforms := options.Platforms
	if len(platforms) == 0 {
		platforms = DefaultPublishPlatforms()
	}
	if err := validatePlatforms(platforms); err != nil {
		return nil, err
	}
	outputRoot, err := resolveRelative(root, defaultValue(settings.Go.Output, "release/bin"), "go.output")
	if err != nil {
		return nil, err
	}
	entryArgument := "./" + filepath.ToSlash(mustRelative(root, entryPath))
	buildFlags, err := goBuildFlags(settings.Go.Optimization)
	if err != nil {
		return nil, err
	}
	results := make([]PlatformBinary, 0, len(platforms))
	for _, platform := range platforms {
		outputDir := filepath.Join(outputRoot, platform.OS, platform.Arch)
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return nil, fmt.Errorf("创建 %s/%s 插件输出目录失败: %w", platform.OS, platform.Arch, err)
		}
		binaryPath := filepath.Join(outputDir, platformBinaryName(item.Metadata.Code, platform.OS))
		environment := withPlatformEnvironment(os.Environ(), platform)
		arguments := append([]string{"build", "-buildvcs=false"}, buildFlags...)
		if tags := strings.TrimSpace(settings.Go.Tags); tags != "" {
			arguments = append(arguments, "-tags", tags)
		}
		arguments = append(arguments, "-o", binaryPath, entryArgument)
		output, err := run(ctx, root, environment, "go", arguments...)
		if err != nil {
			return nil, commandError(fmt.Sprintf("插件 Remote Runner %s/%s 构建失败", platform.OS, platform.Arch), output, err)
		}
		results = append(results, PlatformBinary{Platform: platform, Path: binaryPath})
	}
	return results, nil
}

func manifestForPlatformBuild(root string) (manifest.Manifest, error) {
	return manifest.Load(filepath.Join(root, manifest.DefaultFileName))
}

func validatePlatforms(platforms []Platform) error {
	seen := make(map[string]struct{}, len(platforms))
	for _, platform := range platforms {
		key := platform.OS + "/" + platform.Arch
		if !supportedPublishPlatform(platform) {
			return fmt.Errorf("不支持的插件发布平台 %q", key)
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("插件发布平台重复: %s", key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func supportedPublishPlatform(platform Platform) bool {
	if platform.Arch != "amd64" && platform.Arch != "arm64" {
		return false
	}
	return platform.OS == "linux" || platform.OS == "darwin" || platform.OS == "windows"
}

func platformBinaryName(code, targetOS string) string {
	if targetOS == "windows" {
		return code + ".exe"
	}
	return code
}

func withPlatformEnvironment(current []string, platform Platform) []string {
	result := make([]string, 0, len(current)+4)
	for _, value := range current {
		if strings.HasPrefix(value, "GOOS=") || strings.HasPrefix(value, "GOARCH=") || strings.HasPrefix(value, "CGO_ENABLED=") || strings.HasPrefix(value, "GOWORK=") {
			continue
		}
		result = append(result, value)
	}
	return append(result, "GOOS="+platform.OS, "GOARCH="+platform.Arch, "CGO_ENABLED=0", "GOWORK=off")
}

func runPlatformCommand(ctx context.Context, dir string, environment []string, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = dir
	command.Env = environment
	return command.CombinedOutput()
}
