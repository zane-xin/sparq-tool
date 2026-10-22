package buildpipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	protocolplugin "github.com/zane-xin/sparq-protocol/plugin"
	"github.com/zane-xin/sparq-tool/internal/plugin/manifest"
	"gopkg.in/yaml.v3"
)

const ConfigFileName = "sparq.build.yaml"

const maxManifestOutputBytes = 8 << 20

const (
	optimizationSize  = "size"
	optimizationDebug = "debug"
)

type Target string

const (
	TargetAll      Target = "all"
	TargetGo       Target = "go"
	TargetFrontend Target = "frontend"
)

type Options struct {
	Dir    string
	Target Target
}

type Result struct {
	GoTested               bool
	BinaryPath             string
	FrontendBuilt          bool
	FrontendPath           string
	FrontendPackageManager string
	FrontendArtifactPath   string
	ManifestPath           string
}

type config struct {
	Version  int            `yaml:"version"`
	Go       goConfig       `yaml:"go"`
	Frontend frontendConfig `yaml:"frontend"`
}

type goConfig struct {
	Enabled      *bool    `yaml:"enabled"`
	TestPackages []string `yaml:"test_packages"`
	TestTags     string   `yaml:"test_tags"`
	Tags         string   `yaml:"tags"`
	RemoteEntry  string   `yaml:"remote_entry"`
	Output       string   `yaml:"output"`
	Optimization string   `yaml:"optimization"`
}

type frontendConfig struct {
	Enabled        *bool  `yaml:"enabled"`
	CWD            string `yaml:"cwd"`
	PackageManager string `yaml:"package_manager"`
	Script         string `yaml:"script"`
	Output         string `yaml:"output"`
}

type commandRunner func(context.Context, string, string, ...string) ([]byte, error)
type manifestExporter func(context.Context, string) ([]byte, error)

func Build(ctx context.Context, options Options) (Result, error) {
	return buildWithRunners(ctx, options, runCommand, runGeneratedManifestCommand)
}

func build(ctx context.Context, options Options, run commandRunner) (Result, error) {
	return buildWithRunners(ctx, options, run, nil)
}

func buildWithRunners(ctx context.Context, options Options, run commandRunner, export manifestExporter) (Result, error) {
	root, err := filepath.Abs(defaultValue(strings.TrimSpace(options.Dir), "."))
	if err != nil {
		return Result{}, fmt.Errorf("解析插件目录失败: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return Result{}, fmt.Errorf("插件路径必须是目录: %s", root)
	}
	target := options.Target
	if target == "" {
		target = TargetAll
	}
	if target != TargetAll && target != TargetGo && target != TargetFrontend {
		return Result{}, fmt.Errorf("不支持的插件构建目标 %q，可选值为 all、go、frontend", target)
	}
	item, err := manifest.Load(filepath.Join(root, manifest.DefaultFileName))
	if err != nil {
		return Result{}, err
	}
	settings, err := loadConfig(root)
	if err != nil {
		return Result{}, err
	}

	goEnabled := enabled(settings.Go.Enabled, regularFile(filepath.Join(root, "go.mod")))
	frontendRoot, err := resolveRelative(root, defaultValue(settings.Frontend.CWD, "frontend"), "frontend.cwd")
	if err != nil {
		return Result{}, err
	}
	frontendEnabled := enabled(settings.Frontend.Enabled, regularFile(filepath.Join(frontendRoot, "package.json")))
	if target == TargetGo && !goEnabled {
		return Result{}, fmt.Errorf("插件 %s 未启用 Go 构建阶段", item.Metadata.Code)
	}
	if target == TargetFrontend && !frontendEnabled {
		return Result{}, fmt.Errorf("插件 %s 未启用前端构建阶段", item.Metadata.Code)
	}
	if err := invalidateBuiltManifest(root); err != nil {
		return Result{}, err
	}

	var result Result
	if frontendEnabled && (target == TargetAll || target == TargetFrontend) {
		result, err = buildFrontend(ctx, frontendRoot, settings.Frontend, run, result)
		if err != nil {
			return Result{}, err
		}
	}
	generated, err := generateManifest(root, target != TargetFrontend)
	if err != nil {
		return Result{}, err
	}
	if err := manifest.WriteGo(generated); err != nil {
		return Result{}, fmt.Errorf("写入插件 Manifest 生成文件失败: %w", err)
	}
	if err := manifest.CheckGo(generated); err != nil {
		return Result{}, err
	}
	if target == TargetFrontend {
		return result, nil
	}
	if goEnabled && (target == TargetAll || target == TargetGo) {
		result, err = buildGo(ctx, root, item.Metadata.Code, settings.Go, run, result)
		if err != nil {
			return Result{}, err
		}
	}
	manifestPath, err := exportManifest(ctx, root, item, export)
	if err != nil {
		return Result{}, err
	}
	result.ManifestPath = manifestPath
	return result, nil
}

func generateManifest(root string, requireFrontendArtifacts bool) (manifest.GoGenerateResult, error) {
	return manifest.GenerateGo(manifest.GoGenerateOptions{
		ManifestPath:             filepath.Join(root, manifest.DefaultFileName),
		RequireFrontendArtifacts: requireFrontendArtifacts,
	})
}

// ResolveManifest 导出并校验插件运行时使用的完整 Manifest，但不写入发布目录。
func ResolveManifest(ctx context.Context, manifestPath string) (protocolplugin.Manifest, error) {
	manifestPath, err := filepath.Abs(defaultValue(strings.TrimSpace(manifestPath), manifest.DefaultFileName))
	if err != nil {
		return protocolplugin.Manifest{}, fmt.Errorf("解析插件 Manifest 路径失败: %w", err)
	}
	item, err := manifest.Load(manifestPath)
	if err != nil {
		return protocolplugin.Manifest{}, err
	}
	root := filepath.Dir(manifestPath)
	generatedPath := filepath.Join(root, manifest.DefaultGeneratedFileName)
	if regularFile(generatedPath) {
		generated, err := manifest.GenerateGo(manifest.GoGenerateOptions{ManifestPath: manifestPath})
		if err != nil {
			return protocolplugin.Manifest{}, err
		}
		if err := manifest.CheckGo(generated); err != nil {
			return protocolplugin.Manifest{}, err
		}
	}
	return resolveManifest(ctx, root, manifestPath, item, runGeneratedManifestCommand)
}

func resolveManifest(ctx context.Context, root, manifestPath string, item manifest.Manifest, export manifestExporter) (protocolplugin.Manifest, error) {
	manifestCommand := filepath.Join(root, "cmd", "manifest")
	var payload []byte
	var err error
	if export != nil && (regularFile(filepath.Join(root, "go.mod")) || directory(manifestCommand)) {
		payload, err = export(ctx, root)
		if err != nil {
			return protocolplugin.Manifest{}, err
		}
	} else {
		protocolManifest, protocolErr := item.ProtocolManifest()
		if protocolErr != nil {
			return protocolplugin.Manifest{}, fmt.Errorf("转换插件基础 Manifest 失败: %w", protocolErr)
		}
		if err := manifest.MergeFrontendArtifacts(&protocolManifest, manifestPath, ""); err != nil {
			return protocolplugin.Manifest{}, fmt.Errorf("合并插件前端 artifact 失败: %w", err)
		}
		if err := manifest.MergeFrontendEntryArtifact(&protocolManifest, manifestPath); err != nil {
			return protocolplugin.Manifest{}, fmt.Errorf("合并插件 frontend entry artifact 失败: %w", err)
		}
		payload, err = json.MarshalIndent(protocolManifest, "", "  ")
		if err != nil {
			return protocolplugin.Manifest{}, fmt.Errorf("编码插件基础 Manifest 失败: %w", err)
		}
	}
	var complete protocolplugin.Manifest
	if err := json.Unmarshal(payload, &complete); err != nil {
		return protocolplugin.Manifest{}, fmt.Errorf("解析插件完整 Manifest 输出失败: %w", err)
	}
	if err := manifest.MergeFrontendEntryArtifact(&complete, manifestPath); err != nil {
		return protocolplugin.Manifest{}, fmt.Errorf("合并插件 frontend entry artifact 失败: %w", err)
	}
	if err := requireSharedFrontendEntryArtifacts(complete); err != nil {
		return protocolplugin.Manifest{}, err
	}
	if err := protocolplugin.ValidateManifest(complete); err != nil {
		return protocolplugin.Manifest{}, fmt.Errorf("校验插件完整 Manifest 失败: %w", err)
	}
	if string(complete.Code) != item.Metadata.Code || complete.Version != item.Metadata.Version {
		return protocolplugin.Manifest{}, fmt.Errorf("完整 Manifest 身份不一致: 得到 %s@%s，主清单要求 %s@%s", complete.Code, complete.Version, item.Metadata.Code, item.Metadata.Version)
	}
	return complete, nil
}

func exportManifest(ctx context.Context, root string, item manifest.Manifest, export manifestExporter) (string, error) {
	complete, err := resolveManifest(ctx, root, filepath.Join(root, manifest.DefaultFileName), item, export)
	if err != nil {
		return "", err
	}
	output := filepath.Join(root, "release", "manifest.json")
	encoded, err := json.MarshalIndent(complete, "", "  ")
	if err != nil {
		return "", fmt.Errorf("编码插件完整 Manifest 失败: %w", err)
	}
	if err := writeAtomic(output, append(encoded, '\n')); err != nil {
		return "", fmt.Errorf("写入插件完整 Manifest 失败: %w", err)
	}
	return output, nil
}

func requireSharedFrontendEntryArtifacts(value protocolplugin.Manifest) error {
	for _, entry := range value.Entries {
		// trusted full-app 由唯一 frontend root 激活，不对应 iframe 页面产物。
		if entry.LoadMode == protocolplugin.EntryLoadModeRemote || entry.Sandbox == protocolplugin.EntrySandboxTrusted || entry.Component == "" {
			continue
		}
		component, exists := value.Component(entry.Component)
		if !exists || component.Frontend == nil || entry.Artifact != nil {
			continue
		}
		return fmt.Errorf("Frontend entry %s 缺少 frontend/%s，请启用 Vite build.manifest 后重新构建", entry.Name, entryArtifactFileName)
	}
	return nil
}

func runManifestCommand(ctx context.Context, root string) ([]byte, error) {
	return runManifestCommandAt(ctx, root, "./cmd/manifest")
}

func runGeneratedManifestCommand(ctx context.Context, root string) ([]byte, error) {
	legacy := filepath.Join(root, "cmd", "manifest")
	if directory(legacy) {
		return runManifestCommand(ctx, root)
	}
	modulePath, err := readModulePath(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, err
	}
	temporary, err := os.MkdirTemp(root, ".sparq-manifest-export-*")
	if err != nil {
		return nil, fmt.Errorf("创建临时 Manifest 导出器失败: %w", err)
	}
	defer os.RemoveAll(temporary)
	source := fmt.Sprintf(`package main

import (
	"encoding/json"
	"os"

	pluginroot %q
)

func main() {
	if err := json.NewEncoder(os.Stdout).Encode(pluginroot.Manifest()); err != nil {
		panic(err)
	}
}
`, modulePath)
	entry := filepath.Join(temporary, "main.go")
	if err := os.WriteFile(entry, []byte(source), 0o644); err != nil {
		return nil, fmt.Errorf("写入临时 Manifest 导出器失败: %w", err)
	}
	relative, err := filepath.Rel(root, temporary)
	if err != nil {
		return nil, fmt.Errorf("解析临时 Manifest 导出器路径失败: %w", err)
	}
	return runManifestCommandAt(ctx, root, "./"+filepath.ToSlash(relative))
}

func runManifestCommandAt(ctx context.Context, root, packagePath string) ([]byte, error) {
	command := exec.CommandContext(ctx, "go", "run", "-buildvcs=false", packagePath)
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK=off")
	stdout := &limitedBuffer{limit: maxManifestOutputBytes}
	stderr := &limitedBuffer{limit: maxManifestOutputBytes}
	command.Stdout = stdout
	command.Stderr = stderr
	runErr := command.Run()
	if stdout.exceeded {
		return nil, fmt.Errorf("插件完整 Manifest 输出超过 %d MB 限制", maxManifestOutputBytes>>20)
	}
	if stderr.exceeded {
		return nil, fmt.Errorf("插件完整 Manifest 诊断输出超过 %d MB 限制", maxManifestOutputBytes>>20)
	}
	if runErr != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail == "" {
			return nil, fmt.Errorf("执行插件完整 Manifest 导出失败: %w", runErr)
		}
		return nil, fmt.Errorf("执行插件完整 Manifest 导出失败: %s: %w", detail, runErr)
	}
	return stdout.Bytes(), nil
}

func readModulePath(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取插件 Go module %s 失败: %w", path, err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("插件 go.mod 缺少 module 声明: %s", path)
}

type limitedBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (b *limitedBuffer) Write(value []byte) (int, error) {
	if b.Len()+len(value) > b.limit {
		remaining := b.limit - b.Len()
		if remaining > 0 {
			_, _ = b.Buffer.Write(value[:remaining])
		}
		b.exceeded = true
		return len(value), nil
	}
	return b.Buffer.Write(value)
}

func writeAtomic(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".manifest-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(body); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func invalidateBuiltManifest(root string) error {
	path := filepath.Join(root, "release", "manifest.json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("清理旧的完整 Manifest 失败: %w", err)
	}
	return nil
}

func loadConfig(root string) (config, error) {
	value := config{Go: goConfig{Tags: "production"}}
	path := filepath.Join(root, ConfigFileName)
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return value, nil
	}
	if err != nil {
		return config{}, fmt.Errorf("打开插件构建配置失败: %w", err)
	}
	defer file.Close()
	decoder := yaml.NewDecoder(io.LimitReader(file, (1<<20)+1))
	decoder.KnownFields(true)
	if err := decoder.Decode(&value); err != nil {
		return config{}, fmt.Errorf("解析插件构建配置 %s 失败: %w", path, err)
	}
	if value.Version != 1 {
		return config{}, fmt.Errorf("插件构建配置 version 必须为 1: %s", path)
	}
	if _, err := goBuildFlags(value.Go.Optimization); err != nil {
		return config{}, fmt.Errorf("插件构建配置 %s 无效: %w", path, err)
	}
	return value, nil
}

func goBuildFlags(optimization string) ([]string, error) {
	if optimization == "" {
		optimization = optimizationSize
	}
	switch optimization {
	case optimizationSize:
		return []string{"-trimpath", "-ldflags", "-s -w"}, nil
	case optimizationDebug:
		return nil, nil
	default:
		return nil, fmt.Errorf("go.optimization 必须是 %s 或 %s，当前为 %q", optimizationSize, optimizationDebug, optimization)
	}
}

func buildGo(ctx context.Context, root, code string, settings goConfig, run commandRunner, result Result) (Result, error) {
	if !regularFile(filepath.Join(root, "go.mod")) {
		return Result{}, fmt.Errorf("Go 构建阶段缺少 go.mod: %s", root)
	}
	packages := settings.TestPackages
	if len(packages) == 0 {
		packages = []string{"./..."}
	}
	for _, packagePath := range packages {
		if strings.TrimSpace(packagePath) == "" || strings.HasPrefix(packagePath, "-") {
			return Result{}, fmt.Errorf("go.test_packages 包含无效路径 %q", packagePath)
		}
	}
	testArguments := []string{"test"}
	if tags := strings.TrimSpace(settings.TestTags); tags != "" {
		testArguments = append(testArguments, "-tags", tags)
	}
	testArguments = append(testArguments, packages...)
	if output, err := run(ctx, root, "go", testArguments...); err != nil {
		return Result{}, commandError("插件 Go 测试失败", output, err)
	}
	result.GoTested = true

	entry := strings.TrimSpace(settings.RemoteEntry)
	explicitEntry := entry != ""
	if entry == "" {
		entry = filepath.ToSlash(filepath.Join("cmd", code))
	}
	entryPath, err := resolveRelative(root, entry, "go.remote_entry")
	if err != nil {
		return Result{}, err
	}
	if !directory(entryPath) {
		if explicitEntry {
			return Result{}, fmt.Errorf("插件 Remote Runner 入口目录不存在: %s", entryPath)
		}
		return result, nil
	}
	outputDir, err := resolveRelative(root, defaultValue(settings.Output, "release/bin"), "go.output")
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("创建插件 Go 输出目录失败: %w", err)
	}
	binaryPath := filepath.Join(outputDir, binaryName(code))
	entryArgument := "./" + filepath.ToSlash(mustRelative(root, entryPath))
	buildFlags, err := goBuildFlags(settings.Optimization)
	if err != nil {
		return Result{}, err
	}
	arguments := append([]string{"build", "-buildvcs=false"}, buildFlags...)
	if tags := strings.TrimSpace(settings.Tags); tags != "" {
		arguments = append(arguments, "-tags", tags)
	}
	arguments = append(arguments, "-o", binaryPath, entryArgument)
	if output, err := run(ctx, root, "go", arguments...); err != nil {
		return Result{}, commandError("插件 Remote Runner 构建失败", output, err)
	}
	result.BinaryPath = binaryPath
	return result, nil
}

func buildFrontend(ctx context.Context, frontendRoot string, settings frontendConfig, run commandRunner, result Result) (Result, error) {
	packageJSONPath := filepath.Join(frontendRoot, "package.json")
	if !regularFile(packageJSONPath) {
		return Result{}, fmt.Errorf("前端构建阶段缺少 package.json: %s", frontendRoot)
	}
	manager, err := resolvePackageManager(frontendRoot, packageJSONPath, settings.PackageManager)
	if err != nil {
		return Result{}, err
	}
	script := defaultValue(strings.TrimSpace(settings.Script), "build")
	if strings.HasPrefix(script, "-") || strings.ContainsAny(script, " \t\r\n") {
		return Result{}, fmt.Errorf("frontend.script 必须是单个 package script 名称: %q", script)
	}
	entryArtifactPath := filepath.Join(frontendRoot, entryArtifactFileName)
	if err := os.Remove(entryArtifactPath); err != nil && !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("清理旧的 frontend entry artifact 失败: %w", err)
	}
	frontendArtifactPath := filepath.Join(frontendRoot, frontendArtifactName)
	if err := os.Remove(frontendArtifactPath); err != nil && !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("清理旧的 frontend artifact 失败: %w", err)
	}
	if output, err := run(ctx, frontendRoot, manager, "run", script); err != nil {
		return Result{}, commandError("插件前端构建失败", output, err)
	}
	outputDir, err := resolveRelative(frontendRoot, defaultValue(settings.Output, "dist"), "frontend.output")
	if err != nil {
		return Result{}, err
	}
	if err := requireNonEmptyDirectory(outputDir); err != nil {
		return Result{}, err
	}
	if _, err := generateFrontendArtifact(frontendRoot, outputDir); err != nil {
		return Result{}, err
	}
	artifactPath, err := generateFrontendEntryArtifact(frontendRoot, outputDir)
	if err != nil {
		return Result{}, err
	}
	result.FrontendBuilt = true
	result.FrontendPath = outputDir
	result.FrontendPackageManager = manager
	result.FrontendArtifactPath = artifactPath
	return result, nil
}

func resolvePackageManager(frontendRoot, packageJSONPath, configured string) (string, error) {
	manager := strings.TrimSpace(configured)
	if manager == "" {
		body, err := os.ReadFile(packageJSONPath)
		if err != nil {
			return "", fmt.Errorf("读取前端 package.json 失败: %w", err)
		}
		var metadata struct {
			PackageManager string `json:"packageManager"`
		}
		if err := json.Unmarshal(body, &metadata); err != nil {
			return "", fmt.Errorf("解析前端 package.json 失败: %w", err)
		}
		manager = strings.SplitN(metadata.PackageManager, "@", 2)[0]
	}
	if manager == "" {
		switch {
		case regularFile(filepath.Join(frontendRoot, "pnpm-lock.yaml")):
			manager = "pnpm"
		case regularFile(filepath.Join(frontendRoot, "package-lock.json")):
			manager = "npm"
		default:
			return "", fmt.Errorf("无法判断前端包管理器，请在 %s 中设置 frontend.package_manager", ConfigFileName)
		}
	}
	if manager != "pnpm" && manager != "npm" {
		return "", fmt.Errorf("不支持的前端包管理器 %q，当前支持 pnpm 和 npm", manager)
	}
	return manager, nil
}

func requireNonEmptyDirectory(path string) error {
	if !directory(path) {
		return fmt.Errorf("插件前端构建未生成输出目录: %s", path)
	}
	found := errors.New("found regular file")
	err := filepath.WalkDir(path, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				return found
			}
		}
		return nil
	})
	if errors.Is(err, found) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("检查插件前端输出目录失败: %w", err)
	}
	return fmt.Errorf("插件前端构建输出目录为空: %s", path)
}

func resolveRelative(base, value, field string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || filepath.IsAbs(value) {
		return "", fmt.Errorf("%s 必须是非空相对路径", field)
	}
	resolved := filepath.Clean(filepath.Join(base, filepath.FromSlash(value)))
	relative, err := filepath.Rel(base, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s 不能离开所属目录: %q", field, value)
	}
	return resolved, nil
}

func runCommand(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GOWORK=off")
	return command.CombinedOutput()
}

func commandError(stage string, output []byte, err error) error {
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return fmt.Errorf("%s: %w", stage, err)
	}
	return fmt.Errorf("%s: %s: %w", stage, detail, err)
}

func binaryName(code string) string {
	if runtime.GOOS == "windows" {
		return code + ".exe"
	}
	return code
}

func enabled(value *bool, inferred bool) bool {
	if value == nil {
		return inferred
	}
	return *value
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func directory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func mustRelative(base, target string) string {
	value, _ := filepath.Rel(base, target)
	return value
}

func defaultValue(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
