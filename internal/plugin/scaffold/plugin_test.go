package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zane-xin/sparq-tool/internal/dev"
)

func TestInitPluginCreatesMinimalScaffoldInTargetDirectory(t *testing.T) {
	target := filepath.Join(t.TempDir(), "sparq-plugins-notification-email")
	got, err := initPlugin(pluginOptions{name: "notification-email", dir: target, runtimes: []string{"local", "http"}})
	if err != nil {
		t.Fatalf("initPlugin failed: %v", err)
	}
	if got != target {
		t.Fatalf("target = %q, want %q", got, target)
	}
	for _, relative := range []string{
		"plugin.yaml",
		".sparqignore",
		"go.mod",
		"README.md",
		"bootstrap.go",
		"manifest_gen.go",
		filepath.Join("app", "lifecycle", "register.go"),
		filepath.Join("app", "lifecycle", "instance.go"),
		filepath.Join("app", "manifest", "manifest.go"),
		filepath.Join("cmd", "notification-email", "main.go"),
		DevExampleFileName,
	} {
		assertFileExists(t, target, relative)
	}
	for _, relative := range []string{
		"internal",
		filepath.Join("app", "router"),
		filepath.Join("app", "web"),
		"plugins.dev.yaml",
		"sparq.dev.yaml",
		"frontend",
	} {
		assertPathNotExists(t, target, relative)
	}

	assertFileContains(t, target, "plugin.yaml", "code: notification-email")
	assertFileContains(t, target, ".sparqignore", "coverage/")
	assertFileContains(t, target, "plugin.yaml", "# authors 表示插件发布者或维护者")
	assertFileExcludes(t, target, "plugin.yaml", "name: Sparq Team")
	assertFileExcludes(t, target, "plugin.yaml", "spec:", "entries:", "capabilities:")
	assertFileContains(t, target, "README.md", "`metadata.authors` 表示插件发布者或维护者")
	assertFileExcludes(t, target, "README.md", ".hello", "GET /hello", "internal/register.go")
	assertFileContains(t, target, "go.mod", "module sparq.xin/sparq-plugins-notification-email")
	assertFileContains(t, target, "bootstrap.go", `"sparq.xin/sparq-plugins-notification-email/app/lifecycle"`)
	assertFileContains(t, target, "bootstrap.go", "return lifecycle.Register(ctx, runtime)")
	assertFileContains(t, target, filepath.Join("app", "lifecycle", "register.go"), "return Instance{}, nil")
	assertFileContains(t, target, filepath.Join("app", "lifecycle", "instance.go"), "func (Instance) Start(context.Context) error")
	assertFileContains(t, target, filepath.Join("app", "lifecycle", "instance.go"), "func (Instance) Stop(context.Context) error")
	assertFileContains(t, target, filepath.Join("app", "manifest", "manifest.go"), "func Compose(base plugin.Manifest) (plugin.Manifest, error)")
	assertFileExcludes(t, target, filepath.Join("app", "manifest", "manifest.go"), "EntryManifest", "ProvideSpec", ".hello")
	assertFileContains(t, target, "manifest_gen.go", `pluginmanifest "sparq.xin/sparq-plugins-notification-email/app/manifest"`)
	assertFileContains(t, target, "manifest_gen.go", "pluginmanifest.Compose(value)")
	assertFileContains(t, target, filepath.Join("cmd", "notification-email", "main.go"), "remoteplugin.NewRunner")
	assertFileContains(t, target, filepath.Join("cmd", "notification-email", "main.go"), "ComponentID: pluginmanifest.BackendComponentID")
	assertFileContains(t, target, DevExampleFileName, "component: backend")
}

func TestInitPluginRequiresCode(t *testing.T) {
	_, err := initPlugin(pluginOptions{dir: filepath.Join(t.TempDir(), "cms")})
	if err == nil || !strings.Contains(err.Error(), "插件 code 不能为空") {
		t.Fatalf("expected missing code error, got %v", err)
	}
}

func TestInitPluginRefusesExistingTargetDirectory(t *testing.T) {
	target := filepath.Join(t.TempDir(), "cms")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	_, err := initPlugin(pluginOptions{name: "cms", dir: target})
	if err == nil || !strings.Contains(err.Error(), "插件目标目录已存在") {
		t.Fatalf("expected existing target error, got %v", err)
	}
}

func TestInitPluginExplicitCodeOverridesDirectoryName(t *testing.T) {
	target := filepath.Join(t.TempDir(), "custom-directory")
	if _, err := initPlugin(pluginOptions{name: "catalog", dir: target}); err != nil {
		t.Fatalf("initPlugin failed: %v", err)
	}
	assertFileContains(t, target, "plugin.yaml", "code: catalog")
	assertFileContains(t, target, "go.mod", "module sparq.xin/sparq-plugins-catalog")
	assertFileContains(t, target, filepath.Join("app", "manifest", "manifest.go"), "plugin.RuntimeLocal")
	assertFileContains(t, target, filepath.Join("app", "manifest", "manifest.go"), "plugin.RuntimeHTTP")
	assertFileExists(t, target, filepath.Join("cmd", "catalog", "main.go"))
}

func TestInitPluginUsesExplicitMetadataAndModule(t *testing.T) {
	target := filepath.Join(t.TempDir(), "cms")
	if _, err := initPlugin(pluginOptions{
		name: "cms", title: `内容 "CMS"`, dir: target, module: "example.com/acme/cms",
	}); err != nil {
		t.Fatalf("initPlugin failed: %v", err)
	}
	assertFileContains(t, target, "plugin.yaml", `name: "内容 \"CMS\""`)
	assertFileContains(t, target, "go.mod", "module example.com/acme/cms")
	assertFileContains(t, target, "manifest_gen.go", `pluginmanifest "example.com/acme/cms/app/manifest"`)
}

func TestInitPluginRefusesToOverwriteExistingFile(t *testing.T) {
	target := filepath.Join(t.TempDir(), "cms")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	pluginYAML := filepath.Join(target, "plugin.yaml")
	if err := os.WriteFile(pluginYAML, []byte("existing\n"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	_, err := initPlugin(pluginOptions{name: "cms", dir: target})
	if err == nil || !strings.Contains(err.Error(), "不会覆盖") {
		t.Fatalf("expected overwrite error, got %v", err)
	}
	content, readErr := os.ReadFile(pluginYAML)
	if readErr != nil || string(content) != "existing\n" {
		t.Fatalf("existing file changed: content=%q err=%v", content, readErr)
	}
	assertPathNotExists(t, target, "go.mod")
}

func TestInitPluginOnlyGeneratesDeclaredRuntimeEntries(t *testing.T) {
	root := t.TempDir()
	localTarget := filepath.Join(root, "local-only")
	if _, err := initPlugin(pluginOptions{name: "local-only", dir: localTarget, runtimes: []string{"local"}}); err != nil {
		t.Fatalf("init local plugin failed: %v", err)
	}
	assertPathNotExists(t, localTarget, filepath.Join("cmd", "local-only", "main.go"))
	assertPathNotExists(t, localTarget, DevExampleFileName)
	assertFileContains(t, localTarget, filepath.Join("app", "manifest", "manifest.go"), "plugin.RuntimeLocal")
	assertFileExcludes(t, localTarget, filepath.Join("app", "manifest", "manifest.go"), "plugin.RuntimeHTTP")

	httpTarget := filepath.Join(root, "http-only")
	if _, err := initPlugin(pluginOptions{name: "http-only", dir: httpTarget, runtimes: []string{"http"}}); err != nil {
		t.Fatalf("init http plugin failed: %v", err)
	}
	assertFileExists(t, httpTarget, filepath.Join("cmd", "http-only", "main.go"))
	assertFileContains(t, httpTarget, filepath.Join("app", "manifest", "manifest.go"), "plugin.RuntimeHTTP")
	assertFileExcludes(t, httpTarget, filepath.Join("app", "manifest", "manifest.go"), "plugin.RuntimeLocal")
}

func TestInitPluginWithFrontendStandalone(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "cms")
	if _, err := initPlugin(pluginOptions{
		name: "cms", title: `内容 "CMS"`, dir: target, runtimes: []string{"local"}, components: []string{"go", "frontend"},
	}); err != nil {
		t.Fatalf("initPlugin failed: %v", err)
	}
	assertFileExists(t, target, filepath.Join("frontend", "index.html"))
	assertFileContains(t, target, filepath.Join("frontend", "vite.config.ts"), "base: command === 'serve' ? '/plugins/cms/' : './'")
	assertFileContains(t, target, "sparq.build.yaml", "package_manager: pnpm")
	assertFileContains(t, target, filepath.Join("app", "manifest", "manifest.go"), "plugin.ComponentKindFrontend")
	assertFileContains(t, target, filepath.Join("app", "manifest", "manifest.go"), "FrontendHostAPIV1")
	assertFileContains(t, target, filepath.Join("app", "manifest", "manifest.go"), "base.Frontend = &plugin.FrontendManifest")
	assertPathNotExists(t, target, filepath.Join("frontend", "admin", "index.html"))
	assertPathNotExists(t, target, filepath.Join("frontend", "summary", "index.html"))
}

func TestInitPluginFrontendDoesNotRequireWorkspace(t *testing.T) {
	target := filepath.Join(t.TempDir(), "cms")
	if _, err := initPlugin(pluginOptions{
		name: "cms", dir: target, components: []string{"go", "frontend"},
	}); err != nil {
		t.Fatalf("expected standalone frontend plugin to initialize, got %v", err)
	}
	assertFileExists(t, target, filepath.Join("frontend", "package.json"))
}

func TestAddFrontendToExistingPlugin(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "cms")
	if _, err := initPlugin(pluginOptions{name: "cms", dir: target, runtimes: []string{"local"}}); err != nil {
		t.Fatalf("initPlugin failed: %v", err)
	}
	frontend, err := AddFrontend(FrontendAddOptions{Dir: target})
	if err != nil {
		t.Fatalf("AddFrontend failed: %v", err)
	}
	if frontend != filepath.Join(target, "frontend") {
		t.Fatalf("frontend = %q", frontend)
	}
	assertFileExists(t, target, filepath.Join("frontend", "package.json"))
	assertFileContains(t, target, filepath.Join("frontend", "package.json"), `"@sparqjs/plugin-sdk": "^0.2.2"`)
	assertFileContains(t, target, filepath.Join("app", "manifest", "manifest.go"), "sparqToolGeneratedFrontend")
	assertFileContains(t, target, filepath.Join("app", "manifest", "frontend.go"), "ComponentKindFrontend")
	assertFileContains(t, target, "manifest_gen.go", "FrontendHostAPIV1")
}

func TestAddFrontendRefusesNonEmptyFrontend(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "cms")
	if _, err := initPlugin(pluginOptions{name: "cms", dir: target}); err != nil {
		t.Fatalf("initPlugin failed: %v", err)
	}
	frontendDir := filepath.Join(target, "frontend")
	if err := os.MkdirAll(frontendDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(frontendDir, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := AddFrontend(FrontendAddOptions{Dir: target}); err == nil || !strings.Contains(err.Error(), "前端目录非空") {
		t.Fatalf("expected non-empty frontend error, got %v", err)
	}
	assertPathNotExists(t, target, filepath.Join("app", "manifest", "frontend.go"))
}

func TestAddFrontendRefusesExistingFrontendDeclarationWithoutChanges(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "cms")
	if _, err := initPlugin(pluginOptions{name: "cms", dir: target}); err != nil {
		t.Fatalf("initPlugin failed: %v", err)
	}
	manifestPath := filepath.Join(target, "app", "manifest", "manifest.go")
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	content = append(content, []byte("\n// ComponentKindFrontend already handled manually\n")...)
	if err := os.WriteFile(manifestPath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := AddFrontend(FrontendAddOptions{Dir: target}); err == nil || !strings.Contains(err.Error(), "已包含前端声明") {
		t.Fatalf("expected existing declaration error, got %v", err)
	}
	assertPathNotExists(t, target, "frontend")
}

func TestInitPluginHTTPCreatesStaticDevelopmentExample(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sparq-app"), 0o755); err != nil {
		t.Fatalf("MkdirAll sparq-app failed: %v", err)
	}
	target := filepath.Join(root, "cms")
	if _, err := initPlugin(pluginOptions{name: "cms", dir: target, runtimes: []string{"http"}}); err != nil {
		t.Fatalf("initPlugin failed: %v", err)
	}
	assertFileExists(t, target, DevExampleFileName)
	assertPathNotExists(t, target, "plugins.dev.yaml")
	assertPathNotExists(t, target, "sparq.dev.yaml")
	assertFileContains(t, target, DevExampleFileName, "id: your-plugin-code")
	assertFileContains(t, target, DevExampleFileName, "所有相对路径均以当前配置文件所在目录为基准")
	assertFileExcludes(t, target, DevExampleFileName, "sparq-kernel", "sparq-protocol", "../../bin/sparq-tool")
}

func TestInitPluginCopiesSameDevelopmentExampleForEveryHTTPPlugin(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "catalog")
	second := filepath.Join(root, "reports")
	if _, err := initPlugin(pluginOptions{name: "catalog", dir: first, runtimes: []string{"http"}}); err != nil {
		t.Fatalf("init catalog plugin failed: %v", err)
	}
	if _, err := initPlugin(pluginOptions{name: "reports", dir: second, runtimes: []string{"http"}}); err != nil {
		t.Fatalf("init reports plugin failed: %v", err)
	}
	firstContent, err := os.ReadFile(filepath.Join(first, DevExampleFileName))
	if err != nil {
		t.Fatal(err)
	}
	secondContent, err := os.ReadFile(filepath.Join(second, DevExampleFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(firstContent) != string(secondContent) || string(firstContent) != sparqDevExampleTemplate {
		t.Fatal("development example must be copied without plugin-specific rendering")
	}
	config, err := dev.LoadConfig(filepath.Join(first, DevExampleFileName))
	if err != nil {
		t.Fatalf("development example is not a valid dev config: %v", err)
	}
	if config.Plugin.ID != "your-plugin-code" || config.Frontend.URL == "" {
		t.Fatalf("unexpected development example config: %#v", config)
	}
	if plan := dev.NewPlan(config, dev.Options{}); plan.FrontendEnabled {
		t.Fatal("development example must not enable frontend before it is configured")
	}
}

func TestParseRuntimes(t *testing.T) {
	runtimes, err := ParseRuntimes("both")
	if err != nil {
		t.Fatalf("ParseRuntimes failed: %v", err)
	}
	if strings.Join(runtimes, ",") != "local,http" {
		t.Fatalf("unexpected runtimes: %#v", runtimes)
	}
	defaults, err := ParseRuntimes("")
	if err != nil || strings.Join(defaults, ",") != "local,http" {
		t.Fatalf("default runtimes = %#v, err = %v", defaults, err)
	}
	if _, err := ParseRuntimes("lua"); err == nil {
		t.Fatal("expected unsupported runtime error")
	}
	if _, err := initPlugin(pluginOptions{name: "cms", dir: filepath.Join(t.TempDir(), "cms"), runtimes: []string{"lua"}}); err == nil {
		t.Fatal("expected Plugin API to reject unsupported runtime")
	}
}

func TestParseComponents(t *testing.T) {
	components, err := ParseComponents("go,frontend")
	if err != nil {
		t.Fatalf("ParseComponents failed: %v", err)
	}
	if strings.Join(components, ",") != "go,frontend" {
		t.Fatalf("unexpected components: %#v", components)
	}
	if _, err := ParseComponents("frontend"); err == nil {
		t.Fatal("expected frontend-only error")
	}
	if _, err := ParseComponents("go,lua"); err == nil {
		t.Fatal("expected unsupported component error")
	}
	if _, err := initPlugin(pluginOptions{name: "cms", dir: filepath.Join(t.TempDir(), "cms"), components: []string{"go", "lua"}}); err == nil {
		t.Fatal("expected Plugin API to reject unsupported component")
	}
}

func assertFileExists(t *testing.T, root string, relative string) {
	t.Helper()
	info, err := os.Stat(filepath.Join(root, relative))
	if err != nil || info.IsDir() {
		t.Fatalf("expected file %s: info=%v err=%v", relative, info, err)
	}
}

func assertPathNotExists(t *testing.T, root string, relative string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, relative)); !os.IsNotExist(err) {
		t.Fatalf("expected %s not to exist: %v", relative, err)
	}
}

func assertFileContains(t *testing.T, root string, relative string, expected string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, relative))
	if err != nil {
		t.Fatalf("ReadFile %s failed: %v", relative, err)
	}
	if !strings.Contains(string(content), expected) {
		t.Fatalf("%s does not contain %q", relative, expected)
	}
}

func assertFileExcludes(t *testing.T, root string, relative string, forbidden ...string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, relative))
	if err != nil {
		t.Fatalf("ReadFile %s failed: %v", relative, err)
	}
	for _, value := range forbidden {
		if strings.Contains(string(content), value) {
			t.Fatalf("%s unexpectedly contains %q", relative, value)
		}
	}
}
