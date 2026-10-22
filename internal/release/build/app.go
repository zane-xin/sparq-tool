package build

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zane-xin/sparq-tool/internal/toolchain"
)

// AppOptions 描述统一构建计划的命令行覆盖项。
type AppOptions struct {
	// Config 为统一构建计划。
	Config    *BuildConfig
	GitHash   string
	Version   string
	Targets   []Platform
	Debug     bool
	Tags      string
	Toolchain toolchain.Toolchain
}

// Platform 描述正式发行支持的目标 GOOS/GOARCH。
type Platform struct {
	OS   string
	Arch string
}

// PlatformBinary 描述一个目标平台的 Runtime 构建产物。
type PlatformBinary struct {
	Platform Platform
	Path     string
}

// Result 描述构建产物。
type Result struct {
	BinaryPath       string
	RuntimeBinaries  []PlatformBinary
	UpdaterBinaries  []PlatformBinary
	AdminPath        string
	AdminArtifactDir string
	FrontendPath     string
}

// Build 按 AppOptions 构建配置声明的全部组件。
func Build(options AppOptions) (Result, error) {
	if options.Config == nil {
		return Result{}, fmt.Errorf("构建配置不能为空")
	}
	return buildConfigured(*options.Config, options)
}

// DefaultReleasePlatforms 返回 Sparq 正式发行的完整平台矩阵。
func DefaultReleasePlatforms() []Platform {
	return []Platform{
		{OS: "linux", Arch: "amd64"}, {OS: "linux", Arch: "arm64"},
		{OS: "darwin", Arch: "amd64"}, {OS: "darwin", Arch: "arm64"},
		{OS: "windows", Arch: "amd64"}, {OS: "windows", Arch: "arm64"},
	}
}

// ParsePlatforms 解析 build app 的目标平台参数。
func ParsePlatforms(values []string) ([]Platform, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) == 1 && strings.TrimSpace(values[0]) == "all" {
		return DefaultReleasePlatforms(), nil
	}
	supported := make(map[string]Platform, len(DefaultReleasePlatforms()))
	for _, platform := range DefaultReleasePlatforms() {
		supported[platform.OS+"/"+platform.Arch] = platform
	}
	result := make([]Platform, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "all" {
			return nil, fmt.Errorf("--target all 不能与其他目标平台同时使用")
		}
		platform, ok := supported[value]
		if !ok {
			return nil, fmt.Errorf("不支持的目标平台 %q，支持 linux、darwin、windows 的 amd64/arm64", value)
		}
		if seen[value] {
			return nil, fmt.Errorf("目标平台重复: %s", value)
		}
		seen[value] = true
		result = append(result, platform)
	}
	return result, nil
}

func appPlatformEnvironment(current []string, platform Platform) []string {
	result := make([]string, 0, len(current)+3)
	for _, value := range current {
		if strings.HasPrefix(value, "GOOS=") || strings.HasPrefix(value, "GOARCH=") || strings.HasPrefix(value, "CGO_ENABLED=") {
			continue
		}
		result = append(result, value)
	}
	return append(result, "GOOS="+platform.OS, "GOARCH="+platform.Arch, "CGO_ENABLED=0")
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func platformBinaryName(code string, platform Platform, qualified bool) string {
	name := code
	if qualified {
		name += "-" + platform.OS + "-" + platform.Arch
	}
	if platform.OS == "windows" {
		return name + ".exe"
	}
	return name
}

func appBuildEnvironment(current []string, debug bool) []string {
	result := make([]string, 0, len(current)+1)
	for _, value := range current {
		if strings.HasPrefix(value, "SPARQ_BUILD_DEBUG=") {
			continue
		}
		result = append(result, value)
	}
	debugValue := "0"
	if debug {
		debugValue = "1"
	}
	return append(result, "SPARQ_BUILD_DEBUG="+debugValue)
}

func runBuildCommand(stage string, cmd *exec.Cmd) error {
	output, err := cmd.CombinedOutput()
	if err != nil {
		failure := fmt.Errorf("%s（命令 %q，目录 %q）: %w", stage, cmd.Path, cmd.Dir, err)
		if detail := strings.TrimSpace(string(output)); detail != "" {
			return fmt.Errorf("%w\n%s", failure, detail)
		}
		return failure
	}
	return nil
}

func publishAdmin(source string, destination string) error {
	if !exists(filepath.Join(source, "index.html")) {
		return fmt.Errorf("Admin Shell 构建产物缺少 index.html: %s", source)
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, ".admin-shell-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	files, err := stageAdmin(source, staging)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	if err := rejectAdminSymlinkPath(destination, destination); err != nil {
		return err
	}
	if err := removeAdminExternalDirectories(destination); err != nil {
		return err
	}
	for _, relative := range files {
		if relative == "index.html" {
			continue
		}
		if err := publishAdminFile(destination, filepath.Join(staging, filepath.FromSlash(relative)), filepath.Join(destination, filepath.FromSlash(relative))); err != nil {
			return fmt.Errorf("发布 Admin Shell 文件 %s 失败: %w", relative, err)
		}
	}
	if err := publishAdminFile(destination, filepath.Join(staging, "index.html"), filepath.Join(destination, "index.html")); err != nil {
		return fmt.Errorf("发布 Admin Shell 入口失败: %w", err)
	}
	return nil
}

func removeAdminExternalDirectories(destination string) error {
	for _, directory := range []string{"platforms", "plugins"} {
		path := filepath.Join(destination, directory)
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("清理 Admin Shell 历史外部目录 %s 失败: %w", path, err)
		}
	}
	return nil
}

func stageAdmin(source string, staging string) ([]string, error) {
	files := make([]string, 0)
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		relative = filepath.ToSlash(relative)
		top, _, _ := strings.Cut(relative, "/")
		if top == "plugin-assets" {
			return fmt.Errorf("Admin Shell 构建产物不能包含运行期目录: %s", relative)
		}
		if top == "__app__" {
			return fmt.Errorf("Admin Shell 构建产物不能包含应用前端目录: %s", relative)
		}
		if top == "platforms" || top == "plugins" {
			return fmt.Errorf("Admin Shell 构建产物不能包含二次开发目录: %s", relative)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("Admin Shell 构建产物不能包含符号链接: %s", relative)
		}
		if entry.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("Admin Shell 构建产物必须是普通文件: %s", relative)
		}
		target := filepath.Join(staging, filepath.FromSlash(relative))
		if err := copyAdminFile(path, target); err != nil {
			return err
		}
		files = append(files, relative)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func publishAdminFile(root string, source string, target string) error {
	if err := rejectAdminSymlinkPath(root, target); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".publish-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	input, err := os.Open(source)
	if err != nil {
		_ = temporary.Close()
		return err
	}
	_, copyErr := io.Copy(temporary, input)
	closeErr := temporary.Close()
	inputCloseErr := input.Close()
	if err := errors.Join(copyErr, closeErr, inputCloseErr); err != nil {
		return err
	}
	if err := os.Chmod(temporaryPath, 0o644); err != nil {
		return err
	}
	return replaceFile(temporaryPath, target)
}

func rejectAdminSymlinkPath(root string, target string) error {
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("Admin Shell 发布路径越出目标目录: %s", target)
	}
	current := root
	if info, err := os.Lstat(current); err != nil {
		return err
	} else if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("Admin Shell 发布目录必须是普通目录: %s", current)
	}
	parts := strings.Split(filepath.Clean(relative), string(filepath.Separator))
	for index, part := range parts {
		if index == len(parts)-1 {
			break
		}
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("Admin Shell 发布路径包含非普通目录: %s", current)
		}
	}
	return nil
}

func copyAdminFile(source string, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	return errors.Join(copyErr, closeErr)
}

func appRelativePath(appDir string, relative string, field string) (string, error) {
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("%s必须相对 app 目录: %s", field, relative)
	}
	cleaned := filepath.Clean(relative)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s无效: %s", field, relative)
	}
	return filepath.Join(appDir, cleaned), nil
}
