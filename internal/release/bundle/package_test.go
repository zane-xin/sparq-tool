package bundle

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	releasebuild "github.com/zane-xin/sparq-tool/internal/release/build"
)

func TestPackageCreatesCompleteUnpackedBundle(t *testing.T) {
	root := newTestWorkspace(t, false)
	output := filepath.Join(root, "packages", "linux")
	result, err := Package(Options{
		Root: root, Output: output, Version: "v0.2.0", Archive: ArchiveNone,
		Targets: []releasebuild.Platform{{OS: "linux", Arch: "amd64"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	bundleRoot := filepath.Join(result.Artifacts[0].Path, "sparq")
	for _, relative := range []string{
		"sparq", "sparq-updater", "config/app.yaml", "config/app.example.yaml",
		"public/admin/index.html", "public/assets/site.css", "README.md", "manifest.json", "checksums.txt",
		"public/admin/__app__/frontend.mjs", "public/admin/__app__/App-a1b2c3.js",
	} {
		if _, err := os.Stat(filepath.Join(bundleRoot, filepath.FromSlash(relative))); err != nil {
			t.Fatalf("bundle is missing %s: %v", relative, err)
		}
	}
	if _, err := os.Stat(filepath.Join(bundleRoot, "public", "admin", "plugin-assets")); !os.IsNotExist(err) {
		t.Fatalf("runtime plugin-assets should be excluded, error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(bundleRoot, "public", "admin", "assets", "old.js")); !os.IsNotExist(err) {
		t.Fatalf("stale Admin asset should be excluded, error = %v", err)
	}
	body, err := os.ReadFile(filepath.Join(bundleRoot, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Format != Format || manifest.Version != "0.2.0" || manifest.TargetOS != "linux" || len(manifest.Files) == 0 {
		t.Fatalf("manifest = %+v", manifest)
	}
	checksums, err := os.ReadFile(filepath.Join(bundleRoot, "checksums.txt"))
	if err != nil || !strings.Contains(string(checksums), "  manifest.json\n") {
		t.Fatalf("checksums = %q, %v", checksums, err)
	}
}

func TestPackageCreatesDeterministicTarGz(t *testing.T) {
	root := newTestWorkspace(t, false)
	target := []releasebuild.Platform{{OS: "linux", Arch: "amd64"}}
	first, err := Package(Options{Root: root, Output: filepath.Join(root, "packages", "first"), Version: "0.2.0", Archive: ArchiveTarGz, Targets: target})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Package(Options{Root: root, Output: filepath.Join(root, "packages", "second"), Version: "0.2.0", Archive: ArchiveTarGz, Targets: target})
	if err != nil {
		t.Fatal(err)
	}
	firstBody, err := os.ReadFile(first.Artifacts[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	secondBody, err := os.ReadFile(second.Artifacts[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstBody, secondBody) {
		t.Fatal("tar.gz output is not deterministic")
	}
	names := tarNames(t, first.Artifacts[0].Path)
	if !containsName(names, "sparq/manifest.json") || !containsName(names, "sparq/sparq") {
		t.Fatalf("tar names = %v", names)
	}
}

func TestPackageAutoUsesZipForWindows(t *testing.T) {
	root := newTestWorkspace(t, true)
	result, err := Package(Options{
		Root: root, Output: filepath.Join(root, "packages", "windows"), Version: "0.2.0", Archive: ArchiveAuto,
		Targets: []releasebuild.Platform{{OS: "windows", Arch: "amd64"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Artifacts[0].Archive != ArchiveZip || !strings.HasSuffix(result.Artifacts[0].Path, ".zip") {
		t.Fatalf("artifact = %+v", result.Artifacts[0])
	}
	reader, err := zip.OpenReader(result.Artifacts[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	if !containsName(names, "sparq/sparq.exe") || !containsName(names, "sparq/sparq-updater.exe") {
		t.Fatalf("zip names = %v", names)
	}
}

func TestPackageCreatesDeterministicZip(t *testing.T) {
	root := newTestWorkspace(t, true)
	target := []releasebuild.Platform{{OS: "windows", Arch: "amd64"}}
	first, err := Package(Options{Root: root, Output: filepath.Join(root, "packages", "zip-first"), Version: "0.2.0", Archive: ArchiveZip, Targets: target})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Package(Options{Root: root, Output: filepath.Join(root, "packages", "zip-second"), Version: "0.2.0", Archive: ArchiveZip, Targets: target})
	if err != nil {
		t.Fatal(err)
	}
	firstBody, err := os.ReadFile(first.Artifacts[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	secondBody, err := os.ReadFile(second.Artifacts[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstBody, secondBody) {
		t.Fatal("zip output is not deterministic")
	}
}

func TestPackageRequiresCurrentAdminFileManifest(t *testing.T) {
	root := newTestWorkspace(t, false)
	writeBundleTestFile(t, filepath.Join(root, "sparq-app"), "release/bin/public/admin/sparq-admin-manifest.json", `{"format":"sparq.admin.artifact.v1"}`, 0o644)
	_, err := Package(Options{
		Root: root, Output: filepath.Join(root, "packages", "invalid-admin"), Version: "0.2.0", Archive: ArchiveNone,
		Targets: []releasebuild.Platform{{OS: "linux", Arch: "amd64"}},
	})
	if err == nil || !strings.Contains(err.Error(), "重新运行 build app --admin") {
		t.Fatalf("Package() error = %v", err)
	}
}

func TestPackagePrefersSinglePlatformFlatBuildOutput(t *testing.T) {
	root := newTestWorkspace(t, false)
	appDir := filepath.Join(root, "sparq-app")
	writeBundleTestFile(t, appDir, "release/bin/linux/amd64/sparq", "stale runtime", 0o755)
	writeBundleTestFile(t, appDir, "release/bin/linux/amd64/sparq-updater", "stale updater", 0o755)
	result, err := Package(Options{
		Root: root, Output: filepath.Join(root, "packages", "flat"), Version: "0.2.0", Archive: ArchiveNone,
		Targets: []releasebuild.Platform{{OS: "linux", Arch: "amd64"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(result.Artifacts[0].Path, "sparq", "sparq"))
	if err != nil || string(body) != "runtime" {
		t.Fatalf("runtime = %q, %v", body, err)
	}
}

func TestPackageUsesQualifiedMultiPlatformBuildOutput(t *testing.T) {
	root := newTestWorkspace(t, false)
	appDir := filepath.Join(root, "sparq-app")
	writeBundleTestFile(t, appDir, "release/bin/sparq-linux-amd64", "linux runtime", 0o755)
	writeBundleTestFile(t, appDir, "release/bin/sparq-updater-linux-amd64", "linux updater", 0o755)
	writeBundleTestFile(t, appDir, "release/bin/sparq-windows-amd64.exe", "windows runtime", 0o755)
	writeBundleTestFile(t, appDir, "release/bin/sparq-updater-windows-amd64.exe", "windows updater", 0o755)
	result, err := Package(Options{
		Root: root, Output: filepath.Join(root, "packages", "qualified"), Version: "0.2.0", Archive: ArchiveNone,
		Targets: []releasebuild.Platform{{OS: "linux", Arch: "amd64"}, {OS: "windows", Arch: "amd64"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 2 {
		t.Fatalf("artifacts = %+v", result.Artifacts)
	}
	for _, artifact := range result.Artifacts {
		name := "sparq"
		updater := "sparq-updater"
		if artifact.Platform.OS == "windows" {
			name += ".exe"
			updater += ".exe"
		}
		bundleRoot := filepath.Join(artifact.Path, "sparq")
		if _, err := os.Stat(filepath.Join(bundleRoot, name)); err != nil {
			t.Fatalf("%s/%s runtime missing: %v", artifact.Platform.OS, artifact.Platform.Arch, err)
		}
		if _, err := os.Stat(filepath.Join(bundleRoot, updater)); err != nil {
			t.Fatalf("%s/%s updater missing: %v", artifact.Platform.OS, artifact.Platform.Arch, err)
		}
	}
}

func TestResolveInputBinaryAcceptsLegacyNestedOutput(t *testing.T) {
	input := t.TempDir()
	target := releasebuild.Platform{OS: "linux", Arch: "amd64"}
	legacy := filepath.Join(input, "linux", "amd64", "sparq")
	writeBundleTestFile(t, input, "linux/amd64/sparq", "runtime", 0o755)
	if actual := resolveInputBinary(input, "sparq", target, true); actual != legacy {
		t.Fatalf("resolveInputBinary() = %q, want %q", actual, legacy)
	}
}

func newTestWorkspace(t *testing.T, windows bool) string {
	t.Helper()
	root := t.TempDir()
	appDir := filepath.Join(root, "sparq-app")
	writeBundleTestFile(t, appDir, "go.mod", "module example.com/sparq-app\n", 0o644)
	writeBundleTestFile(t, appDir, "config/app.yaml", "app: {}\n", 0o644)
	writeBundleTestFile(t, appDir, "config/app.example.yaml", "app: {}\n", 0o644)
	writeBundleTestFile(t, appDir, "public/admin/index.html", "admin", 0o644)
	writeBundleTestFile(t, appDir, "public/admin/sparq-admin-manifest.json", `{"format":"sparq.admin.artifact.v1","files":["assets/main.js","index.html"]}`, 0o644)
	writeBundleTestFile(t, appDir, "public/admin/assets/main.js", "current", 0o644)
	writeBundleTestFile(t, appDir, "public/admin/assets/old.js", "stale", 0o644)
	writeBundleTestFile(t, appDir, "public/admin/plugin-assets/cms/index.js", "runtime", 0o644)
	writeBundleTestFile(t, appDir, "public/assets/site.css", "body{}", 0o644)
	writeBundleTestFile(t, appDir, "README.release.md", "# Sparq\n", 0o644)
	writeBundleTestFile(t, appDir, "release/bin/public/admin/index.html", "admin", 0o644)
	writeBundleTestFile(t, appDir, "release/bin/public/admin/sparq-admin-manifest.json", `{"format":"sparq.admin.artifact.v1","files":["assets/main.js","index.html"]}`, 0o644)
	writeBundleTestFile(t, appDir, "release/bin/public/admin/assets/main.js", "current", 0o644)
	writeBundleTestFile(t, appDir, "release/bin/public/admin/assets/old.js", "stale", 0o644)
	writeBundleTestFile(t, appDir, "release/bin/public/admin/__app__/frontend.mjs", "application entry", 0o644)
	writeBundleTestFile(t, appDir, "release/bin/public/admin/__app__/App-a1b2c3.js", "application chunk", 0o644)
	writeBundleTestFile(t, appDir, "release/bin/public/assets/captcha/runtime.js", "captcha", 0o644)
	writeBundleTestFile(t, appDir, "release/bin/public/assets/captcha/styles.css", "body{}", 0o644)
	runtimeName := "sparq"
	updaterName := "sparq-updater"
	if windows {
		runtimeName += ".exe"
		updaterName += ".exe"
	}
	writeBundleTestFile(t, appDir, "release/bin/"+runtimeName, "runtime", 0o755)
	writeBundleTestFile(t, appDir, "release/bin/"+updaterName, "updater", 0o755)
	return root
}

func writeBundleTestFile(t *testing.T, root, relative, body string, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func tarNames(t *testing.T, path string) []string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	names := make([]string, 0)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return names
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, header.Name)
	}
}

func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}
