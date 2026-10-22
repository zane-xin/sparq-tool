package build

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTargetBinaryNameUsesTargetOS(t *testing.T) {
	linux := Platform{OS: "linux", Arch: "amd64"}
	if actual := platformBinaryName("sparq", linux, true); actual != "sparq-linux-amd64" {
		t.Fatalf("platformBinaryName(linux multi) = %q", actual)
	}
	platform := Platform{OS: "windows", Arch: "amd64"}
	if actual := platformBinaryName("sparq", platform, false); actual != "sparq.exe" {
		t.Fatalf("platformBinaryName(single) = %q", actual)
	}
	if actual := platformBinaryName("sparq", platform, true); actual != "sparq-windows-amd64.exe" {
		t.Fatalf("platformBinaryName(multi) = %q", actual)
	}
}

func TestDefaultReleasePlatformsCoversSupportedMatrix(t *testing.T) {
	platforms := DefaultReleasePlatforms()
	if len(platforms) != 6 {
		t.Fatalf("DefaultReleasePlatforms() = %+v", platforms)
	}
	want := []Platform{{OS: "linux", Arch: "amd64"}, {OS: "linux", Arch: "arm64"}, {OS: "darwin", Arch: "amd64"}, {OS: "darwin", Arch: "arm64"}, {OS: "windows", Arch: "amd64"}, {OS: "windows", Arch: "arm64"}}
	for index := range want {
		if platforms[index] != want[index] {
			t.Fatalf("platforms[%d] = %+v, want %+v", index, platforms[index], want[index])
		}
	}
}

func TestParsePlatformsSupportsAllAndRejectsDuplicates(t *testing.T) {
	all, err := ParsePlatforms([]string{"all"})
	if err != nil || len(all) != 6 {
		t.Fatalf("ParsePlatforms(all) = %+v, %v", all, err)
	}
	selected, err := ParsePlatforms([]string{"linux/amd64", "windows/arm64"})
	if err != nil || len(selected) != 2 || selected[1] != (Platform{OS: "windows", Arch: "arm64"}) {
		t.Fatalf("ParsePlatforms(selected) = %+v, %v", selected, err)
	}
	if _, err := ParsePlatforms([]string{"linux/amd64", "linux/amd64"}); err == nil || !strings.Contains(err.Error(), "重复") {
		t.Fatalf("ParsePlatforms(duplicate) error = %v", err)
	}
	if _, err := ParsePlatforms([]string{"freebsd/amd64"}); err == nil || !strings.Contains(err.Error(), "不支持") {
		t.Fatalf("ParsePlatforms(unsupported) error = %v", err)
	}
}

func TestPublishAdminUpdatesShellAndPreservesRuntimeFiles(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "dist")
	destination := filepath.Join(root, "app", "public", "admin")
	writeAdminBuildTestFile(t, source, "index.html", "old shell")
	writeAdminBuildTestFile(t, source, "assets/main-old.js", "old asset")
	if err := publishAdmin(source, destination); err != nil {
		t.Fatal(err)
	}
	writeAdminBuildTestFile(t, destination, "plugin-assets/cms@1.0.0/index.js", "plugin")
	writeAdminBuildTestFile(t, destination, "__app__/frontend.mjs", "application")
	writeAdminBuildTestFile(t, destination, "platforms/sparq-website/frontend.mjs", "website")
	writeAdminBuildTestFile(t, destination, "plugins/legacy/frontend.mjs", "legacy plugin")
	writeAdminBuildTestFile(t, source, "index.html", "new shell")
	writeAdminBuildTestFile(t, source, "assets/main-new.js", "new asset")
	if err := publishAdmin(source, destination); err != nil {
		t.Fatal(err)
	}
	for relative, expected := range map[string]string{
		"index.html":                       "new shell",
		"assets/main-old.js":               "old asset",
		"assets/main-new.js":               "new asset",
		"plugin-assets/cms@1.0.0/index.js": "plugin",
		"__app__/frontend.mjs":             "application",
	} {
		body, err := os.ReadFile(filepath.Join(destination, filepath.FromSlash(relative)))
		if err != nil || string(body) != expected {
			t.Fatalf("published %s = %q, %v", relative, body, err)
		}
	}
	for _, relative := range []string{"platforms", "plugins"} {
		if _, err := os.Stat(filepath.Join(destination, relative)); !os.IsNotExist(err) {
			t.Fatalf("historical external directory %s was not removed: %v", relative, err)
		}
	}
}

func TestPublishConfiguredPathsCopiesOnlyDeclaredFilesAndDirectories(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "app")
	outputDir := filepath.Join(root, "bin")
	writeAdminBuildTestFile(t, appDir, "config/app.yaml", "new app\n")
	writeAdminBuildTestFile(t, appDir, "config/app.dev.yaml", "development\n")
	writeAdminBuildTestFile(t, appDir, "themes/website@0.0.1/theme.yaml", "theme\n")
	writeAdminBuildTestFile(t, outputDir, "config/app.yaml", "old app\n")
	writeAdminBuildTestFile(t, outputDir, "config/runtime.yaml", "runtime\n")
	if err := publishConfiguredPaths(appDir, outputDir, []BuildCopyEntry{{Source: "config/app.yaml", Target: "config/app.yaml"}, {Source: "themes", Target: "themes"}}); err != nil {
		t.Fatal(err)
	}
	for relative, expected := range map[string]string{
		"config/app.yaml":                 "new app\n",
		"config/runtime.yaml":             "runtime\n",
		"themes/website@0.0.1/theme.yaml": "theme\n",
	} {
		body, err := os.ReadFile(filepath.Join(outputDir, filepath.FromSlash(relative)))
		if err != nil || string(body) != expected {
			t.Fatalf("published %s = %q, %v", relative, body, err)
		}
	}
	if _, err := os.Stat(filepath.Join(outputDir, "config", "app.dev.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("未声明的开发配置被发布: %v", err)
	}
}

func TestPublishAdminRejectsRuntimeDirectory(t *testing.T) {
	source := filepath.Join(t.TempDir(), "dist")
	writeAdminBuildTestFile(t, source, "index.html", "shell")
	writeAdminBuildTestFile(t, source, "plugin-assets/cms@1.0.0/index.js", "conflict")
	err := publishAdmin(source, filepath.Join(t.TempDir(), "admin"))
	if err == nil || !strings.Contains(err.Error(), "运行期目录") {
		t.Fatalf("publishAdmin() error = %v", err)
	}
}

func TestPublishAdminRejectsApplicationFrontendDirectory(t *testing.T) {
	source := filepath.Join(t.TempDir(), "dist")
	writeAdminBuildTestFile(t, source, "index.html", "shell")
	writeAdminBuildTestFile(t, source, "__app__/frontend.mjs", "conflict")
	err := publishAdmin(source, filepath.Join(t.TempDir(), "admin"))
	if err == nil || !strings.Contains(err.Error(), "应用前端目录") {
		t.Fatalf("publishAdmin() error = %v", err)
	}
}

func TestPublishApplicationFrontendReplacesBuildAtomically(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "dist")
	destination := filepath.Join(root, "public", "admin", "__app__")
	writeAdminBuildTestFile(t, source, "frontend.mjs", "new entry")
	writeAdminBuildTestFile(t, source, "App-a1b2.js", "new chunk")
	writeAdminBuildTestFile(t, destination, "frontend.mjs", "old entry")
	writeAdminBuildTestFile(t, destination, "App-old.js", "old chunk")
	if err := publishApplicationFrontend(source, destination); err != nil {
		t.Fatal(err)
	}
	for relative, expected := range map[string]string{"frontend.mjs": "new entry", "App-a1b2.js": "new chunk"} {
		body, err := os.ReadFile(filepath.Join(destination, relative))
		if err != nil || string(body) != expected {
			t.Fatalf("published application frontend %s = %q, %v", relative, body, err)
		}
	}
	if _, err := os.Stat(filepath.Join(destination, "App-old.js")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old application chunk still exists: %v", err)
	}
}

func TestPublishApplicationFrontendRequiresFixedEntry(t *testing.T) {
	source := t.TempDir()
	writeAdminBuildTestFile(t, source, "App-a1b2.js", "chunk")
	err := publishApplicationFrontend(source, filepath.Join(t.TempDir(), "__app__"))
	if err == nil || !strings.Contains(err.Error(), "frontend.mjs") {
		t.Fatalf("publishApplicationFrontend() error = %v", err)
	}
}

func TestPublishAdminRejectsExternalDevelopmentDirectory(t *testing.T) {
	source := filepath.Join(t.TempDir(), "dist")
	writeAdminBuildTestFile(t, source, "index.html", "shell")
	writeAdminBuildTestFile(t, source, "platforms/sparq-website/frontend.mjs", "website")
	err := publishAdmin(source, filepath.Join(t.TempDir(), "admin"))
	if err == nil || !strings.Contains(err.Error(), "二次开发目录") {
		t.Fatalf("publishAdmin() error = %v", err)
	}
}

func TestAppRelativePathRejectsEscape(t *testing.T) {
	if _, err := appRelativePath(t.TempDir(), "../outside", "Admin Shell 发布目录"); err == nil {
		t.Fatal("appRelativePath() error = nil")
	}
}

func TestPublishAdminRejectsDestinationSymlink(t *testing.T) {
	if testing.Short() {
		t.Skip("符号链接测试需要文件系统支持")
	}
	root := t.TempDir()
	source := filepath.Join(root, "dist")
	destination := filepath.Join(root, "admin")
	writeAdminBuildTestFile(t, source, "index.html", "shell")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(destination, "assets")); err != nil {
		t.Skipf("创建符号链接失败: %v", err)
	}
	writeAdminBuildTestFile(t, source, "assets/main.js", "asset")
	err := publishAdmin(source, destination)
	if err == nil || !strings.Contains(err.Error(), "非普通目录") {
		t.Fatalf("publishAdmin() error = %v", err)
	}
}

func writeAdminBuildTestFile(t *testing.T, root string, relative string, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
