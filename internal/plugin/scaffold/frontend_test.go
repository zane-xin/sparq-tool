package scaffold

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitFrontendCreatesSingleVueEntry(t *testing.T) {
	root := t.TempDir()
	createTestPlugin(t, root, "cms", "cms")
	target := filepath.Join(root, "cms", "frontend")
	got, err := initFrontend(frontendOptions{name: "cms", dir: target})
	if err != nil {
		t.Fatalf("initFrontend failed: %v", err)
	}
	if got != target {
		t.Fatalf("target = %q, want %q", got, target)
	}
	for _, relative := range []string{
		"package.json", "tsconfig.json", "vite.config.ts", "README.md", "index.html",
		filepath.Join("src", "App.vue"), filepath.Join("src", "main.ts"), filepath.Join("src", "frontend.ts"),
		filepath.Join("src", "bootstrap.ts"), filepath.Join("src", "style.scss"),
	} {
		assertFileExists(t, target, relative)
	}
	assertFileContains(t, target, "package.json", `"@sparqjs/plugin-sdk": "^0.2.2"`)
	assertFileContains(t, target, "package.json", `"@sparqjs/vite-plugin": "^0.2.2"`)
	assertFileContains(t, target, "vite.config.ts", "base: command === 'serve' ? '/plugins/cms/' : './'")
	assertFileContains(t, target, "vite.config.ts", "manifest: true")
	assertFileContains(t, target, "vite.config.ts", "createSparqTrustedRoot()")
	assertFileContains(t, target, "vite.config.ts", "from '@sparqjs/vite-plugin'")
	assertFileContains(t, target, "vite.config.ts", "entry: 'src/frontend.ts'")
	assertFileContains(t, target, "vite.config.ts", "fileName: () => 'frontend.mjs'")
	assertFileExcludes(t, target, "package.json", "check-trusted-root-dependencies.mjs", "workspace:*")
	assertFileExcludes(t, target, "vite.config.ts", "external: ['vue']")
	assertFileContains(t, target, filepath.Join("src", "bootstrap.ts"), "bootstrapSparqVuePlugin")
	assertFileContains(t, target, filepath.Join("src", "App.vue"), "useSparqPlugin")
	assertFrontendTreeExcludes(t, target, "dev_url", "realm_code", "application_code", "/ping")
	if _, err := os.Stat(filepath.Join(target, "build")); !os.IsNotExist(err) {
		t.Fatalf("前端脚手架不应复制 Vite 插件实现: %v", err)
	}
}

func TestInitFrontendRejectsNonEmptyTarget(t *testing.T) {
	root := t.TempDir()
	createTestPlugin(t, root, "cms", "cms")
	target := filepath.Join(root, "cms", "frontend")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "existing.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	_, err := initFrontend(frontendOptions{name: "cms", dir: target})
	if err == nil || !strings.Contains(err.Error(), "目标目录非空") {
		t.Fatalf("expected non-empty target error, got %v", err)
	}
}

func TestInitFrontendRequiresMatchingPluginManifest(t *testing.T) {
	root := t.TempDir()
	if _, err := initFrontend(frontendOptions{name: "cms", dir: filepath.Join(root, "cms", "frontend")}); err == nil || !strings.Contains(err.Error(), "plugin.yaml") {
		t.Fatalf("expected missing plugin manifest error, got %v", err)
	}
	createTestPlugin(t, root, "cms", "seo")
	if _, err := initFrontend(frontendOptions{name: "cms", dir: filepath.Join(root, "cms", "frontend")}); err == nil || !strings.Contains(err.Error(), `metadata.code 为 "seo"`) {
		t.Fatalf("expected plugin code mismatch error, got %v", err)
	}
}

func createTestPlugin(t *testing.T, workspaceRoot string, directory string, code string) {
	t.Helper()
	if _, err := initPlugin(pluginOptions{
		name: code, dir: filepath.Join(workspaceRoot, directory), runtimes: []string{"local"},
	}); err != nil {
		t.Fatalf("initPlugin failed: %v", err)
	}
}

func assertFrontendTreeExcludes(t *testing.T, root string, forbidden ...string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, value := range forbidden {
			if strings.Contains(string(content), value) {
				t.Fatalf("%s unexpectedly contains %q", path, value)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir failed: %v", err)
	}
}
