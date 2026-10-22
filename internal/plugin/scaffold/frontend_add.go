package scaffold

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/zane-xin/sparq-tool/internal/plugin/manifest"
)

// FrontendAddOptions 描述向已有 Go 插件增加标准前端时的输入。
type FrontendAddOptions struct {
	Dir string
}

// AddFrontend 向已有 Go 插件增加独立前端脚手架。
// 已存在的前端声明会被拒绝，避免覆盖插件开发者的自定义实现。
func AddFrontend(options FrontendAddOptions) (string, error) {
	root, err := filepath.Abs(strings.TrimSpace(options.Dir))
	if err != nil {
		return "", fmt.Errorf("解析插件目录失败: %w", err)
	}
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("读取插件目录 %s 失败: %w", root, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("插件路径不是目录: %s", root)
	}

	pluginManifest, err := manifest.Load(filepath.Join(root, manifest.DefaultFileName))
	if err != nil {
		return "", fmt.Errorf("读取插件清单失败: %w", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return "", fmt.Errorf("插件缺少 go.mod: %w", err)
	}
	manifestPath := filepath.Join(root, "app", "manifest", "manifest.go")
	manifestSource, packageName, err := prepareManifestSource(manifestPath)
	if err != nil {
		return "", err
	}
	frontendDir := filepath.Join(root, "frontend")
	frontendExisted := false
	if info, statErr := os.Stat(frontendDir); statErr == nil {
		frontendExisted = true
		if !info.IsDir() {
			return "", fmt.Errorf("前端路径不是目录: %s", frontendDir)
		}
		notEmpty, readErr := directoryNotEmpty(frontendDir)
		if readErr != nil {
			return "", readErr
		}
		if notEmpty {
			return "", fmt.Errorf("前端目录非空，不会覆盖: %s", frontendDir)
		}
	} else if !os.IsNotExist(statErr) {
		return "", fmt.Errorf("检查前端目录失败: %w", statErr)
	}

	helperPath := filepath.Join(root, "app", "manifest", "frontend.go")
	if _, err := os.Stat(helperPath); err == nil {
		return "", fmt.Errorf("前端 Manifest 辅助文件已存在，不会覆盖: %s", helperPath)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("检查前端 Manifest 辅助文件失败: %w", err)
	}
	buildConfigPath := filepath.Join(root, "sparq.build.yaml")
	_, buildConfigErr := os.Stat(buildConfigPath)
	buildConfigExisted := buildConfigErr == nil
	if buildConfigErr != nil && !os.IsNotExist(buildConfigErr) {
		return "", fmt.Errorf("检查插件构建配置失败: %w", buildConfigErr)
	}

	originalManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		return "", fmt.Errorf("读取插件 Manifest 源文件失败: %w", err)
	}
	originalGenerated, generatedErr := os.ReadFile(filepath.Join(root, manifest.DefaultGeneratedFileName))
	generatedExisted := generatedErr == nil
	if generatedErr != nil && !os.IsNotExist(generatedErr) {
		return "", fmt.Errorf("读取插件 Manifest 生成文件失败: %w", generatedErr)
	}

	rollback := func() {
		_ = os.WriteFile(manifestPath, originalManifest, 0o644)
		_ = os.Remove(helperPath)
		generatedPath := filepath.Join(root, manifest.DefaultGeneratedFileName)
		if generatedExisted {
			_ = os.WriteFile(generatedPath, originalGenerated, 0o644)
		} else {
			_ = os.Remove(generatedPath)
		}
		if frontendExisted {
			for relative := range map[string]struct{}{
				"package.json": {}, "tsconfig.json": {}, "vite.config.ts": {}, "README.md": {}, "index.html": {},
				filepath.Join("src", "App.vue"): {}, filepath.Join("src", "frontend.ts"): {},
				filepath.Join("src", "main.ts"): {}, filepath.Join("src", "bootstrap.ts"): {},
				filepath.Join("src", "style.scss"): {},
			} {
				_ = os.Remove(filepath.Join(frontendDir, relative))
			}
		} else {
			_ = os.RemoveAll(frontendDir)
		}
		if !buildConfigExisted {
			_ = os.Remove(buildConfigPath)
		}
	}

	if _, err := initFrontend(frontendOptions{name: pluginManifest.Metadata.Code, dir: frontendDir}); err != nil {
		rollback()
		return "", err
	}
	if !buildConfigExisted {
		if err := writeStaticFile(buildConfigPath, buildConfigTemplate); err != nil {
			rollback()
			return "", fmt.Errorf("写入插件构建配置失败: %w", err)
		}
	}
	if err := os.WriteFile(manifestPath, manifestSource, 0o644); err != nil {
		rollback()
		return "", fmt.Errorf("更新插件 Manifest 源文件失败: %w", err)
	}
	helperSource := frontendManifestHelper(packageName)
	if err := os.WriteFile(helperPath, []byte(helperSource), 0o644); err != nil {
		rollback()
		return "", fmt.Errorf("写入前端 Manifest 辅助文件失败: %w", err)
	}
	generated, err := manifest.GenerateGo(manifest.GoGenerateOptions{ManifestPath: filepath.Join(root, manifest.DefaultFileName)})
	if err != nil {
		rollback()
		return "", fmt.Errorf("重新生成插件 Manifest 失败: %w", err)
	}
	if err := manifest.WriteGo(generated); err != nil {
		rollback()
		return "", fmt.Errorf("写入插件 Manifest 生成文件失败: %w", err)
	}
	return frontendDir, nil
}

func prepareManifestSource(path string) ([]byte, string, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", fmt.Errorf("插件缺少 app/manifest/manifest.go: %s", path)
		}
		return nil, "", fmt.Errorf("读取插件 Manifest 源文件失败: %w", err)
	}
	if strings.Contains(string(source), "ComponentKindFrontend") || strings.Contains(string(source), "FrontendManifest") {
		return nil, "", fmt.Errorf("插件 Manifest 已包含前端声明，请手工维护，未生成前端文件")
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, source, parser.ParseComments)
	if err != nil {
		return nil, "", fmt.Errorf("解析插件 Manifest 源文件失败: %w", err)
	}
	var compose *ast.FuncDecl
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Recv == nil && function.Name.Name == "Compose" {
			compose = function
			break
		}
	}
	if compose == nil || compose.Body == nil || compose.Type.Params == nil || compose.Type.Params.NumFields() != 1 {
		return nil, "", fmt.Errorf("插件 Manifest 缺少可修改的 Compose(base plugin.Manifest) 函数")
	}
	field := compose.Type.Params.List[0]
	if len(field.Names) != 1 {
		return nil, "", fmt.Errorf("插件 Manifest Compose 参数必须有明确名称")
	}
	param := field.Names[0].Name
	validationIndex := -1
	for index, statement := range compose.Body.List {
		found := false
		ast.Inspect(statement, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if ok && selector.Sel.Name == "ValidateManifest" {
				found = true
			}
			return !found
		})
		if found {
			validationIndex = index
			break
		}
	}
	if validationIndex < 0 {
		return nil, "", fmt.Errorf("插件 Manifest Compose 缺少 ValidateManifest 调用，无法安全增加前端声明")
	}
	call := &ast.ExprStmt{X: &ast.CallExpr{
		Fun:  &ast.Ident{Name: "sparqToolGeneratedFrontend"},
		Args: []ast.Expr{&ast.UnaryExpr{Op: token.AND, X: &ast.Ident{Name: param}}},
	}}
	compose.Body.List = append(compose.Body.List, nil)
	copy(compose.Body.List[validationIndex+1:], compose.Body.List[validationIndex:])
	compose.Body.List[validationIndex] = call
	var output bytes.Buffer
	err = format.Node(&output, token.NewFileSet(), file)
	if err != nil {
		return nil, "", fmt.Errorf("格式化插件 Manifest 源文件失败: %w", err)
	}
	return output.Bytes(), file.Name.Name, nil
}

func frontendManifestHelper(packageName string) string {
	return fmt.Sprintf(`package %s

import (
	"github.com/zane-xin/sparq-protocol/plugin"
	"github.com/zane-xin/sparq-protocol/protocol"
)

func sparqToolGeneratedFrontend(base *plugin.Manifest) {
	const componentID protocol.ComponentID = "admin"
	base.Components = append(base.Components, plugin.ComponentManifest{
		ID: componentID, Kind: plugin.ComponentKindFrontend, Language: "typescript",
		Frontend: &plugin.FrontendComponent{},
	})
	base.Frontend = &plugin.FrontendManifest{
		Component: componentID, HostAPI: plugin.FrontendHostAPIV1,
		RuntimeProfile: plugin.FrontendRuntimeProfileAdminV1,
	}
}
`, packageName)
}
