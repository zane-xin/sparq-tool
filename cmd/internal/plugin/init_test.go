package plugincmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCommandRequiresCode(t *testing.T) {
	command := newInitCommand()
	command.SetArgs(nil)
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "required flag(s) \"code\"") {
		t.Fatalf("expected required code error, got %v", err)
	}
}

func TestInitCommandUsesCodeAsDefaultDirectory(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	target := filepath.Join(root, "catalog")

	command := newInitCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--code", "catalog"})
	if err := command.Execute(); err != nil {
		t.Fatalf("plugin init failed: %v", err)
	}
	if !strings.Contains(output.String(), target) {
		t.Fatalf("output = %q, want target %q", output.String(), target)
	}
	if !strings.Contains(output.String(), "go test ./...") {
		t.Fatalf("output = %q, want verification command", output.String())
	}
	if !strings.Contains(output.String(), "验证命令") {
		t.Fatalf("output = %q, want neutral verification instructions", output.String())
	}
	content, err := os.ReadFile(filepath.Join(target, "plugin.yaml"))
	if err != nil {
		t.Fatalf("ReadFile plugin.yaml failed: %v", err)
	}
	if !strings.Contains(string(content), "code: catalog") {
		t.Fatalf("plugin.yaml does not contain inferred code: %s", content)
	}
	if _, err := os.Stat(filepath.Join(target, "cmd", "catalog", "main.go")); err != nil {
		t.Fatalf("expected default HTTP entry: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "internal")); !os.IsNotExist(err) {
		t.Fatalf("default scaffold must not create internal: %v", err)
	}
}

func TestInitCommandPassesCodeAndFlags(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "plugin-source")
	t.Chdir(root)

	command := newInitCommand()
	command.SetArgs([]string{
		"--code", "cms", "--path", target, "--name", "内容管理", "--module", "example.com/acme/cms", "--runtime", "both", "--with", "go",
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("plugin init failed: %v", err)
	}
	assertCommandFileContains(t, target, "plugin.yaml", `name: "内容管理"`)
	assertCommandFileContains(t, target, "go.mod", "module example.com/acme/cms")
	assertCommandFileContains(t, target, filepath.Join("app", "manifest", "manifest.go"), "plugin.RuntimeLocal")
	assertCommandFileContains(t, target, filepath.Join("app", "manifest", "manifest.go"), "plugin.RuntimeHTTP")
	if _, err := os.Stat(filepath.Join(target, "cmd", "cms", "main.go")); err != nil {
		t.Fatalf("expected HTTP entry: %v", err)
	}
}

func TestInitCommandRefusesExistingTargetDirectory(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "test")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	command := newInitCommand()
	command.SetArgs([]string{"--code", "test"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "插件目标目录已存在") {
		t.Fatalf("expected existing target error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "plugin.yaml")); !os.IsNotExist(err) {
		t.Fatalf("existing target was modified: %v", err)
	}
}

func TestInitCommandCreatesFrontendWhenRequested(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	target := filepath.Join(root, "catalog")

	command := newInitCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--code", "catalog", "--path", target, "--with-frontend"})
	if err := command.Execute(); err != nil {
		t.Fatalf("plugin init --with-frontend failed: %v", err)
	}
	assertCommandFileContains(t, target, filepath.Join("app", "manifest", "manifest.go"), "plugin.ComponentKindFrontend")
	assertCommandFileContains(t, target, filepath.Join("frontend", "package.json"), "@sparqjs/plugin-catalog-frontend")
	if !strings.Contains(output.String(), "sparq.dev.example.yaml") ||
		!strings.Contains(output.String(), "另存为 sparq.dev.yaml") {
		t.Fatalf("output = %q, want development example instructions", output.String())
	}
}

func TestInitCommandCreatesStandaloneFrontendPluginWithoutWorkspace(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	command := newInitCommand()
	command.SetArgs([]string{"--code", "catalog", "--with-frontend"})
	if err := command.Execute(); err != nil {
		t.Fatalf("plugin init --with-frontend failed: %v", err)
	}
	target := filepath.Join(root, "catalog")
	if _, err := os.Stat(filepath.Join(target, "frontend", "package.json")); err != nil {
		t.Fatalf("expected standalone frontend package: %v", err)
	}
	assertCommandFileContains(t, target, filepath.Join("frontend", "package.json"), `"@sparqjs/plugin-sdk": "^0.2.2"`)
	assertCommandFileContains(t, target, filepath.Join("frontend", "package.json"), `"@sparqjs/vite-plugin": "^0.2.2"`)
	assertCommandFileContains(t, target, filepath.Join("frontend", "vite.config.ts"), "createSparqTrustedRoot()")
	assertCommandFileContains(t, target, filepath.Join("frontend", "vite.config.ts"), "from '@sparqjs/vite-plugin'")
}

func TestPluginCommandIncludesBuildPackagePublishAndInit(t *testing.T) {
	command := New()
	for _, name := range []string{"add", "build", "download", "init", "package", "publish"} {
		child, _, err := command.Find([]string{name})
		if err != nil {
			t.Fatalf("find plugin %s: %v", name, err)
		}
		if child == command || child.Name() != name {
			t.Fatalf("plugin %s command not registered: %v", name, child)
		}
	}
}

func TestAddFrontendCommand(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "cms")
	t.Chdir(root)
	initCommand := newInitCommand()
	initCommand.SetArgs([]string{"--code", "cms"})
	if err := initCommand.Execute(); err != nil {
		t.Fatalf("plugin init failed: %v", err)
	}
	command := New()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"add", "frontend", "--path", target})
	if err := command.Execute(); err != nil {
		t.Fatalf("plugin add frontend failed: %v", err)
	}
	if !strings.Contains(output.String(), "插件前端已添加") {
		t.Fatalf("output = %q", output.String())
	}
	if _, err := os.Stat(filepath.Join(target, "frontend", "package.json")); err != nil {
		t.Fatalf("expected frontend package: %v", err)
	}
}

func assertCommandFileContains(t *testing.T, root string, relative string, expected string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, relative))
	if err != nil {
		t.Fatalf("ReadFile %s failed: %v", relative, err)
	}
	if !strings.Contains(string(content), expected) {
		t.Fatalf("%s does not contain %q", relative, expected)
	}
}
