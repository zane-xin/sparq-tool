package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/zane-xin/sparq-protocol/plugin"
)

const (
	DefaultGeneratedFileName = "manifest_gen.go"
	ManifestModuleDirName    = "manifest"
	appManifestModuleDirName = "app/manifest"
	ManifestComposeFuncName  = "Compose"
)

// GoGenerateOptions 描述由主清单及其引用文件生成 Go Manifest 入口所需的路径和包名。
type GoGenerateOptions struct {
	ManifestPath string
	PackageName  string
	Output       string
	// ArtifactPath 覆盖默认的前端 artifact.json 搜索路径。
	ArtifactPath string
	// RequireFrontendArtifacts 要求已声明的 frontend root 有本次构建生成的 artifact。
	RequireFrontendArtifacts bool
}

// GoGenerateResult 保存确定性的生成内容和输出位置。
type GoGenerateResult struct {
	Output  string
	Content []byte
}

// GenerateGo 将已校验的 Manifest 源文件集合转成插件根包唯一的 Manifest 实现。
func GenerateGo(options GoGenerateOptions) (GoGenerateResult, error) {
	manifestPath := options.ManifestPath
	if manifestPath == "" {
		manifestPath = DefaultFileName
	}
	manifestPath, err := filepath.Abs(manifestPath)
	if err != nil {
		return GoGenerateResult{}, fmt.Errorf("解析插件清单路径: %w", err)
	}
	item, err := Load(manifestPath)
	if err != nil {
		return GoGenerateResult{}, err
	}
	protocolManifest, err := item.ProtocolManifest()
	if err != nil {
		return GoGenerateResult{}, fmt.Errorf("转换插件清单 %s: %w", manifestPath, err)
	}
	artifacts, err := mergeFrontendArtifacts(&protocolManifest, manifestPath, options.ArtifactPath)
	if err != nil {
		return GoGenerateResult{}, err
	}
	var entryArtifact *plugin.FrontendEntryArtifact
	if RequiresFrontendEntryArtifact(protocolManifest) {
		entryArtifact, err = readFrontendEntryArtifact(filepath.Dir(manifestPath))
		if err != nil {
			return GoGenerateResult{}, err
		}
	}
	frontendRootDiscovered := isRegularFile(filepath.Join(filepath.Dir(manifestPath), "frontend", "src", "frontend.ts"))
	if options.RequireFrontendArtifacts && protocolManifest.Frontend != nil && artifacts == nil {
		return GoGenerateResult{}, fmt.Errorf("trusted frontend root 未找到 frontend/artifact.json，请先完成前端构建")
	}
	packageName := options.PackageName
	if packageName == "" {
		packageName, err = detectPackageName(filepath.Dir(manifestPath))
		if err != nil {
			return GoGenerateResult{}, err
		}
	}
	if !token.IsIdentifier(packageName) {
		return GoGenerateResult{}, fmt.Errorf("插件 Go package 名称无效: %q", packageName)
	}
	payload, err := json.Marshal(protocolManifest)
	if err != nil {
		return GoGenerateResult{}, fmt.Errorf("编码插件 %s protocol manifest: %w", item.Metadata.Code, err)
	}
	manifestImport := ""
	composeSource := "\treturn value\n"
	generatedModuleSource := ""
	module, enabled, err := resolveManifestModule(filepath.Dir(manifestPath))
	if err != nil {
		return GoGenerateResult{}, err
	}
	if enabled {
		manifestImport = fmt.Sprintf("\tpluginmanifest %q\n", module)
		composeSource = `	composed, err := pluginmanifest.Compose(value)
	if err != nil {
		panic("compose generated plugin manifest: " + err.Error())
	}
	return composed
`
	}
	if frontendRootDiscovered {
		generatedModuleSource = `
func applyGeneratedFrontendRoot(value *plugin.Manifest) {
	if value.Frontend != nil {
		return
	}
	componentID := plugin.DefaultComponentID
	for _, component := range value.Components {
		if component.Kind == plugin.ComponentKindFrontend {
			componentID = component.ID
			break
		}
	}
	value.Frontend = &plugin.FrontendManifest{Component: componentID, HostAPI: plugin.FrontendHostAPIV1, RuntimeProfile: plugin.FrontendRuntimeProfileAdminV1}
}
`
		if enabled {
			composeSource = strings.Replace(composeSource, "\treturn composed\n", "\tapplyGeneratedFrontendRoot(&composed)\n\treturn composed\n", 1)
		} else {
			composeSource = strings.Replace(composeSource, "\treturn value\n", "\tapplyGeneratedFrontendRoot(&value)\n\treturn value\n", 1)
		}
	}
	artifactSource := ""
	if artifacts != nil {
		artifactPayload, marshalErr := json.Marshal(artifacts)
		if marshalErr != nil {
			return GoGenerateResult{}, fmt.Errorf("编码插件 %s frontend artifact: %w", item.Metadata.Code, marshalErr)
		}
		artifactSource = fmt.Sprintf(`
const frontendArtifactJSON = %s

type generatedFrontendArtifact struct {
	Icons map[string]string `+"`json:\"icons,omitempty\"`"+`
	Version string                       `+"`json:\"version\"`"+`
	Entry   string                       `+"`json:\"entry\"`"+`
	Styles  []string                     `+"`json:\"styles,omitempty\"`"+`
	Files   map[string]string            `+"`json:\"files\"`"+`
}

func applyFrontendArtifacts(value *plugin.Manifest) {
	var artifact generatedFrontendArtifact
	if err := json.Unmarshal([]byte(frontendArtifactJSON), &artifact); err != nil {
		panic("decode generated frontend artifact: " + err.Error())
	}
	if value.Frontend == nil {
		return
	}
	value.Frontend.Artifact = &plugin.FrontendArtifact{Version: artifact.Version, Entry: artifact.Entry, Icons: artifact.Icons, Styles: artifact.Styles, Files: artifact.Files}
}
`, strconv.Quote(string(artifactPayload)))
		if enabled {
			composeSource = strings.Replace(composeSource, "\treturn composed\n", "\tapplyFrontendArtifacts(&composed)\n\treturn composed\n", 1)
		} else {
			composeSource = strings.Replace(composeSource, "\treturn value\n", "\tapplyFrontendArtifacts(&value)\n\treturn value\n", 1)
		}
	}
	if entryArtifact != nil {
		entryPayload, marshalErr := json.Marshal(entryArtifact)
		if marshalErr != nil {
			return GoGenerateResult{}, fmt.Errorf("编码插件 %s frontend entry artifact: %w", item.Metadata.Code, marshalErr)
		}
		artifactSource += fmt.Sprintf(`
const frontendEntryArtifactJSON = %s

func applyFrontendEntryArtifact(value *plugin.Manifest) {
	var artifact plugin.FrontendEntryArtifact
	if err := json.Unmarshal([]byte(frontendEntryArtifactJSON), &artifact); err != nil {
		panic("decode generated frontend entry artifact: " + err.Error())
	}
	candidates := make([]int, 0, 1)
	for index, entry := range value.Entries {
		if entry.LoadMode == plugin.EntryLoadModeRemote || entry.Component == "" {
			continue
		}
		component, exists := value.Component(entry.Component)
		if exists && component.Kind == plugin.ComponentKindFrontend {
			candidates = append(candidates, index)
		}
	}
	if len(candidates) == 0 {
		return
	}
	if len(candidates) > 1 {
		panic("frontend entry artifact requires exactly one shared local entry")
	}
	value.Entries[candidates[0]].Artifact = &artifact
}
`, strconv.Quote(string(entryPayload)))
		if enabled {
			composeSource = strings.Replace(composeSource, "\treturn composed\n", "\tapplyFrontendEntryArtifact(&composed)\n\treturn composed\n", 1)
		} else {
			composeSource = strings.Replace(composeSource, "\treturn value\n", "\tapplyFrontendEntryArtifact(&value)\n\treturn value\n", 1)
		}
	}
	source := fmt.Sprintf(`// Code generated by sparq-tool manifest generate. DO NOT EDIT.
package %s

import (
	"encoding/json"

	"github.com/zane-xin/sparq-protocol/plugin"
%s
)

const pluginManifestJSON = %s
%s

// Manifest 返回由插件 Manifest 源文件集合生成的完整协议清单。
func Manifest() plugin.Manifest {
	var value plugin.Manifest
	if err := json.Unmarshal([]byte(pluginManifestJSON), &value); err != nil {
		panic("decode generated plugin manifest: " + err.Error())
	}
%s}
`, packageName, manifestImport, strconv.Quote(string(payload)), generatedModuleSource+artifactSource, composeSource)
	content, err := format.Source([]byte(source))
	if err != nil {
		return GoGenerateResult{}, fmt.Errorf("格式化插件 %s manifest 生成文件: %w", item.Metadata.Code, err)
	}
	output := options.Output
	if output == "" {
		output = filepath.Join(filepath.Dir(manifestPath), DefaultGeneratedFileName)
	} else if !filepath.IsAbs(output) {
		output = filepath.Join(filepath.Dir(manifestPath), output)
	}
	return GoGenerateResult{Output: filepath.Clean(output), Content: content}, nil
}

func resolveManifestModule(pluginDir string) (string, bool, error) {
	for _, relativeDir := range []string{appManifestModuleDirName, ManifestModuleDirName} {
		manifestDir := filepath.Join(pluginDir, filepath.FromSlash(relativeDir))
		info, err := os.Stat(manifestDir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", false, fmt.Errorf("检查插件 Manifest 模块 %s: %w", manifestDir, err)
		}
		if !info.IsDir() {
			return "", false, fmt.Errorf("插件 Manifest 模块必须是目录: %s", manifestDir)
		}
		if err := validateManifestComposeFunction(manifestDir); err != nil {
			return "", false, err
		}
		modulePath, err := readGoModulePath(filepath.Join(pluginDir, "go.mod"))
		if err != nil {
			return "", false, err
		}
		return path.Join(modulePath, relativeDir), true, nil
	}
	return "", false, nil
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func validateManifestComposeFunction(manifestDir string) error {
	packages, err := parser.ParseDir(token.NewFileSet(), manifestDir, func(info os.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("解析插件 Manifest 模块 %s: %w", manifestDir, err)
	}
	for _, parsedPackage := range packages {
		for _, file := range parsedPackage.Files {
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Recv != nil || function.Name.Name != ManifestComposeFuncName {
					continue
				}
				if fieldCount(function.Type.Params) != 1 || fieldCount(function.Type.Results) != 2 {
					return fmt.Errorf("插件 Manifest 函数签名无效: %s.%s 必须接收一个参数并返回 Manifest 和 error", manifestDir, ManifestComposeFuncName)
				}
				return nil
			}
		}
	}
	return fmt.Errorf("插件 Manifest 模块缺少 %s 函数: %s", ManifestComposeFuncName, manifestDir)
}

func fieldCount(fields *ast.FieldList) int {
	if fields == nil {
		return 0
	}
	count := 0
	for _, field := range fields.List {
		if len(field.Names) == 0 {
			count++
		} else {
			count += len(field.Names)
		}
	}
	return count
}

func readGoModulePath(goModPath string) (string, error) {
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return "", fmt.Errorf("读取插件 Go module %s: %w", goModPath, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("插件 go.mod 缺少 module 声明: %s", goModPath)
}

// WriteGo 写入 Manifest 生成结果。
func WriteGo(result GoGenerateResult) error {
	if result.Output == "" {
		return fmt.Errorf("Manifest 生成文件路径不能为空")
	}
	current, err := os.ReadFile(result.Output)
	if err == nil && bytes.Equal(current, result.Content) {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(result.Output), 0o755); err != nil {
		return err
	}
	return os.WriteFile(result.Output, result.Content, 0o644)
}

// CheckGo 确认现有生成文件与 Manifest 主清单及其引用文件完全一致。
func CheckGo(result GoGenerateResult) error {
	current, err := os.ReadFile(result.Output)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("插件 Manifest 生成文件不存在: %s", result.Output)
		}
		return err
	}
	if !bytes.Equal(current, result.Content) {
		return fmt.Errorf("插件 Manifest 生成文件已过期: %s，请运行 sparq-tool manifest generate", result.Output)
	}
	return nil
}

func detectPackageName(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == DefaultGeneratedFileName {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, parser.PackageClauseOnly)
		if err != nil {
			return "", fmt.Errorf("解析插件 Go package %s: %w", filepath.Join(dir, name), err)
		}
		return parsed.Name.Name, nil
	}
	return "", fmt.Errorf("插件目录 %s 缺少可用于确定 package 名称的 Go 文件", dir)
}
