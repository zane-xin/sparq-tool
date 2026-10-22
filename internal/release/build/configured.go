package build

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/zane-xin/sparq-tool/internal/toolchain"
)

// buildConfigured 执行统一配置驱动的 app、updater、Admin 和应用前端构建。
func buildConfigured(config BuildConfig, options AppOptions) (Result, error) {
	if err := config.Validate(); err != nil {
		return Result{}, err
	}
	tool, _, err := toolchain.LoadUser()
	if err != nil {
		return Result{}, err
	}
	tool = toolchain.Merge(tool, config.Toolchain)
	tool = toolchain.Merge(tool, options.Toolchain)
	root := config.BaseDir()
	if root == "" {
		root = "."
	}
	outputDir := config.resolvePath(config.Output.Dir)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("创建构建输出目录失败: %w", err)
	}
	version, gitHash, gitDirty := resolveBuildMetadata(config, options)
	options.Version = version
	options.GitHash = gitHash

	appDir := config.resolvePath(config.App.Path)
	if !regularDirectory(appDir) {
		return Result{}, fmt.Errorf("app.path 不是目录: %s", appDir)
	}
	platforms := options.Targets
	if len(platforms) == 0 {
		platforms = []Platform{{OS: envOrDefault("GOOS", runtime.GOOS), Arch: envOrDefault("GOARCH", runtime.GOARCH)}}
	}
	result := Result{}
	for _, platform := range platforms {
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return Result{}, err
		}
		qualified := len(platforms) > 1
		binaryPath := filepath.Join(outputDir, platformBinaryName(config.App.Binary, platform, qualified))
		if err := buildConfiguredGo(appDir, config.App, binaryPath, platform, options, tool.Go, true); err != nil {
			return Result{}, err
		}
		result.RuntimeBinaries = append(result.RuntimeBinaries, PlatformBinary{Platform: platform, Path: binaryPath})
		if result.BinaryPath == "" && len(platforms) == 1 {
			result.BinaryPath = binaryPath
		}
		if strings.TrimSpace(config.Updater.Path) != "" {
			updaterDir := config.resolvePath(config.Updater.Path)
			updaterPath := filepath.Join(outputDir, platformBinaryName(config.Updater.Binary, platform, qualified))
			if err := buildConfiguredGo(updaterDir, config.Updater, updaterPath, platform, options, tool.Go, false); err != nil {
				return Result{}, err
			}
			result.UpdaterBinaries = append(result.UpdaterBinaries, PlatformBinary{Platform: platform, Path: updaterPath})
		}
	}
	if err := publishConfiguredPaths(appDir, outputDir, config.Copy); err != nil {
		return Result{}, err
	}

	if strings.TrimSpace(config.Admin.Path) != "" {
		adminDir := config.resolvePath(config.Admin.Path)
		dist, err := buildConfiguredFrontend(adminDir, config.Admin.Commands, tool.Frontend, options.Debug, options.Version, options.GitHash, "Admin Shell")
		if err != nil {
			return Result{}, err
		}
		adminOutput := filepath.Join(outputDir, "public", "admin")
		if err := publishAdmin(dist, adminOutput); err != nil {
			return Result{}, err
		}
		result.AdminPath = adminOutput
		result.AdminArtifactDir = adminOutput
	}
	if strings.TrimSpace(config.Frontend.Path) != "" {
		frontendDir := config.resolvePath(config.Frontend.Path)
		dist, err := buildConfiguredFrontend(frontendDir, config.Frontend.Commands, tool.Frontend, options.Debug, options.Version, options.GitHash, "应用前端")
		if err != nil {
			return Result{}, err
		}
		frontendOutput := filepath.Join(outputDir, "public", "admin", "__app__")
		if err := publishApplicationFrontend(dist, frontendOutput); err != nil {
			return Result{}, fmt.Errorf("发布应用前端失败: %w", err)
		}
		if err := removeLegacyApplicationFrontend(filepath.Join(outputDir, "public", "app")); err != nil {
			return Result{}, err
		}
		result.FrontendPath = frontendOutput
	}
	if err := writeBuildManifest(outputDir, config, options, platforms, result, gitDirty); err != nil {
		return Result{}, err
	}
	return result, nil
}

func publishApplicationFrontend(source string, destination string) error {
	if !regularFile(filepath.Join(source, "frontend.mjs")) {
		return fmt.Errorf("应用前端构建产物缺少 frontend.mjs: %s", source)
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, ".application-frontend-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := copyDirectory(source, staging); err != nil {
		return err
	}
	backup, err := os.MkdirTemp(parent, ".application-frontend-backup-*")
	if err != nil {
		return err
	}
	if err := os.Remove(backup); err != nil {
		return err
	}
	hasPrevious := false
	if info, statErr := os.Lstat(destination); statErr == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("应用前端发布目录不是普通目录: %s", destination)
		}
		if err := os.Rename(destination, backup); err != nil {
			return err
		}
		hasPrevious = true
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if err := os.Rename(staging, destination); err != nil {
		if hasPrevious {
			return errors.Join(err, os.Rename(backup, destination))
		}
		return err
	}
	if hasPrevious {
		if err := os.RemoveAll(backup); err != nil {
			return fmt.Errorf("清理应用前端旧构建失败: %w", err)
		}
	}
	return nil
}

func removeLegacyApplicationFrontend(legacyRoot string) error {
	info, err := os.Lstat(legacyRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !regularFile(filepath.Join(legacyRoot, "frontend.mjs")) {
		return fmt.Errorf("旧应用前端目录无法确认为构建产物，请手动处理: %s", legacyRoot)
	}
	if err := filepath.WalkDir(legacyRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == legacyRoot || entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("旧应用前端目录包含非普通文件: %s", path)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := os.RemoveAll(legacyRoot); err != nil {
		return fmt.Errorf("清理旧应用前端目录失败: %w", err)
	}
	return nil
}

// publishConfiguredPaths 只发布构建配置显式声明的运行文件和目录。
func publishConfiguredPaths(appDir, outputDir string, entries []BuildCopyEntry) error {
	for _, entry := range entries {
		source := filepath.Join(appDir, filepath.Clean(strings.TrimSpace(entry.Source)))
		info, err := os.Lstat(source)
		if err != nil {
			return fmt.Errorf("读取 copy.source %s 失败: %w", entry.Source, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("copy.source 不能是符号链接: %s", entry.Source)
		}
		target := filepath.Join(outputDir, filepath.Clean(strings.TrimSpace(entry.Target)))
		switch {
		case info.IsDir():
			if err := copyDirectory(source, target); err != nil {
				return fmt.Errorf("复制目录 %s 到 %s 失败: %w", entry.Source, entry.Target, err)
			}
		case info.Mode().IsRegular():
			if err := copyConfiguredFile(source, target); err != nil {
				return fmt.Errorf("复制文件 %s 到 %s 失败: %w", entry.Source, entry.Target, err)
			}
		default:
			return fmt.Errorf("copy.source 必须是普通文件或目录: %s", entry.Source)
		}
	}
	return nil
}

func copyConfiguredFile(source, target string) error {
	body, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, body, 0o644)
}

func buildConfiguredGo(dir string, component BuildGoComponent, output string, platform Platform, options AppOptions, spec toolchain.Spec, injectRuntimeMetadata bool) error {
	if !regularDirectory(dir) {
		return fmt.Errorf("Go 组件路径不是目录: %s", dir)
	}
	args := []string{"build"}
	args = append(args, component.Commands...)
	if tags := strings.TrimSpace(options.Tags); tags != "" {
		args = append(args, "-tags", tags)
	}
	if !options.Debug {
		ldflags := []string{"-s", "-w", "-buildid="}
		if injectRuntimeMetadata {
			ldflags = append(ldflags, configuredAppLdflags(options, component)...)
		}
		args = append(args, "-trimpath", "-ldflags", strings.Join(ldflags, " "))
	} else if injectRuntimeMetadata {
		if ldflags := configuredAppLdflags(options, component); len(ldflags) > 0 {
			args = append(args, "-ldflags", strings.Join(ldflags, " "))
		}
	}
	args = append(args, "-o", output, component.Package)
	command := exec.Command(spec.Command, append(append([]string{}, spec.Args...), args...)...)
	command.Dir = dir
	command.Env = appPlatformEnvironment(os.Environ(), platform)
	return runBuildCommand(fmt.Sprintf("Go 组件 %s/%s 构建失败", platform.OS, platform.Arch), command)
}

func buildConfiguredFrontend(dir string, scripts []string, spec toolchain.Spec, debug bool, version, gitHash, stage string) (string, error) {
	if !regularDirectory(dir) {
		return "", fmt.Errorf("%s路径不是目录: %s", stage, dir)
	}
	if !regularFile(filepath.Join(dir, "package.json")) {
		return "", fmt.Errorf("%s目录缺少 package.json: %s", stage, dir)
	}
	manager, err := detectPackageManager(dir)
	if err != nil {
		return "", err
	}
	if len(scripts) == 0 {
		scripts = []string{"build"}
	}
	for _, script := range scripts {
		script = strings.TrimSpace(script)
		if script == "" || strings.ContainsAny(script, " \t\r\n") {
			return "", fmt.Errorf("%s commands 必须是非空 package script 名称", stage)
		}
		args := frontendCommandArgs(spec, manager, "run", script)
		command := exec.Command(spec.Command, args...)
		command.Dir = dir
		command.Env = buildFrontendEnvironment(os.Environ(), debug, version, gitHash)
		if err := runBuildCommand(stage+"构建失败", command); err != nil {
			return "", err
		}
	}
	dist := filepath.Join(dir, "dist")
	if err := requireFrontendBuildOutput(dir); err != nil {
		return "", fmt.Errorf("%s: %w", stage, err)
	}
	return dist, nil
}

func requireFrontendBuildOutput(frontendDir string) error {
	outputDir := filepath.Join(frontendDir, "dist")
	info, err := os.Stat(outputDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("前端构建未生成输出目录: %s", outputDir)
		}
		return fmt.Errorf("检查前端输出目录失败: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("前端输出路径不是目录: %s", outputDir)
	}
	found := errors.New("found regular file")
	err = filepath.WalkDir(outputDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		fileInfo, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if fileInfo.Mode().IsRegular() {
			return found
		}
		return nil
	})
	if errors.Is(err, found) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("检查前端输出目录失败: %w", err)
	}
	return fmt.Errorf("前端构建输出目录为空: %s", outputDir)
}

func buildFrontendEnvironment(current []string, debug bool, version, gitHash string) []string {
	result := appBuildEnvironment(current, debug)
	for _, prefix := range []string{"SPARQ_BUILD_VERSION=", "SPARQ_BUILD_GIT_HASH="} {
		filtered := result[:0]
		for _, value := range result {
			if !strings.HasPrefix(value, prefix) {
				filtered = append(filtered, value)
			}
		}
		result = filtered
	}
	result = append(result, "SPARQ_BUILD_VERSION="+version)
	if gitHash != "" {
		result = append(result, "SPARQ_BUILD_GIT_HASH="+gitHash)
	}
	return result
}

func frontendCommandArgs(spec toolchain.Spec, manager string, command ...string) []string {
	args := append([]string{}, spec.Args...)
	if len(args) == 0 || filepath.Base(args[len(args)-1]) != manager {
		args = append(args, manager)
	}
	return append(args, command...)
}

func detectPackageManager(dir string) (string, error) {
	manager := ""
	for current := dir; ; current = filepath.Dir(current) {
		if data, err := os.ReadFile(filepath.Join(current, "package.json")); err == nil {
			var metadata struct {
				PackageManager string `json:"packageManager"`
			}
			if err := json.Unmarshal(data, &metadata); err != nil {
				return "", fmt.Errorf("解析前端 package.json 失败: %w", err)
			}
			manager = strings.SplitN(metadata.PackageManager, "@", 2)[0]
		}
		if manager == "" {
			switch {
			case regularFile(filepath.Join(current, "pnpm-lock.yaml")):
				manager = "pnpm"
			case regularFile(filepath.Join(current, "package-lock.json")):
				manager = "npm"
			}
		}
		if manager != "" || filepath.Dir(current) == current {
			break
		}
	}
	if manager == "" {
		return "", fmt.Errorf("无法判断前端包管理器，请在 package.json 声明 packageManager 或提供锁文件: %s", dir)
	}
	if manager != "pnpm" && manager != "npm" {
		return "", fmt.Errorf("不支持的前端包管理器 %q", manager)
	}
	return manager, nil
}

func regularDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func copyDirectory(source, destination string) error {
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil || relative == "." {
			return err
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("前端构建产物必须是普通文件: %s", path)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, body, 0o644)
	})
}

type buildManifest struct {
	Format     string     `json:"format"`
	Version    string     `json:"version"`
	GitHash    string     `json:"git_hash"`
	GitDirty   bool       `json:"git_dirty"`
	BuiltAt    string     `json:"built_at"`
	Targets    []Platform `json:"targets"`
	Components []string   `json:"components"`
}

func writeBuildManifest(output string, config BuildConfig, options AppOptions, targets []Platform, result Result, dirty bool) error {
	version := strings.TrimSpace(options.Version)
	if version == "" {
		version = strings.TrimSpace(config.Version)
	}
	if version == "" {
		version = "dev"
	}
	gitHash := strings.TrimSpace(options.GitHash)
	if gitHash == "" {
		gitHash = strings.TrimSpace(config.GitHash)
	}
	if gitHash == "" || gitHash == "auto" {
		gitHash, dirty = detectGitHash(config.BaseDir())
	}
	components := []string{"app"}
	if len(result.UpdaterBinaries) > 0 {
		components = append(components, "updater")
	}
	if result.AdminPath != "" {
		components = append(components, "admin")
	}
	if result.FrontendPath != "" {
		components = append(components, "frontend")
	}
	sort.Strings(components)
	payload, err := json.MarshalIndent(buildManifest{Format: "sparq.app.build.v1", Version: version, GitHash: gitHash, GitDirty: dirty, BuiltAt: time.Now().UTC().Format(time.RFC3339), Targets: targets, Components: components}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(output, "build-manifest.json"), append(payload, '\n'), 0o644)
}

func resolveBuildMetadata(config BuildConfig, options AppOptions) (string, string, bool) {
	version := strings.TrimSpace(options.Version)
	if version == "" {
		version = strings.TrimSpace(config.Version)
	}
	if version == "" {
		version = "dev"
	}
	gitHash := strings.TrimSpace(options.GitHash)
	if gitHash == "" {
		gitHash = strings.TrimSpace(config.GitHash)
	}
	resolvedHash, dirty := detectGitHash(config.BaseDir())
	if gitHash == "" || gitHash == "auto" {
		gitHash = resolvedHash
	}
	return version, gitHash, dirty
}

func configuredAppLdflags(options AppOptions, component BuildGoComponent) []string {
	if filepath.Base(filepath.Clean(component.Package)) != "sparq" && !strings.HasPrefix(component.Binary, "sparq") {
		return nil
	}
	version := strings.TrimSpace(options.Version)
	if version == "" || version == "dev" {
		return nil
	}
	flags := []string{"-X", "github.com/zane-xin/sparq.version=" + version}
	if hash := strings.TrimSpace(options.GitHash); hash != "" {
		flags = append(flags, "-X", "github.com/zane-xin/sparq/internal/buildmanifest.BuildGitHash="+hash)
	}
	for _, value := range []struct {
		path  string
		value string
	}{
		{"github.com/zane-xin/sparq/internal/buildmanifest.Distribution", "official"},
		{"github.com/zane-xin/sparq/internal/buildmanifest.HostOwnership", "managed"},
		{"github.com/zane-xin/sparq/internal/buildmanifest.HostUpdatePolicy", "automatic"},
		{"github.com/zane-xin/sparq/internal/buildmanifest.AdminOwnership", "managed"},
		{"github.com/zane-xin/sparq/internal/buildmanifest.AdminUpdatePolicy", "automatic"},
	} {
		flags = append(flags, "-X", value.path+"="+value.value)
	}
	return flags
}

func detectGitHash(root string) (string, bool) {
	command := exec.Command("git", "-C", root, "rev-parse", "HEAD")
	output, err := command.Output()
	if err != nil {
		return "", false
	}
	hash := strings.TrimSpace(string(bytes.TrimSpace(output)))
	dirtyCommand := exec.Command("git", "-C", root, "status", "--porcelain")
	dirtyOutput, dirtyErr := dirtyCommand.Output()
	return hash, dirtyErr == nil && len(bytes.TrimSpace(dirtyOutput)) > 0
}
