package packager

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestPackagePluginCreatesPackageV1Archive(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "plugin.yaml"), "apiVersion: sparq.io/v1\n")
	writeFile(t, filepath.Join(root, "README.md"), "# Demo\n")
	writeFile(t, filepath.Join(root, "release", "bin", "demo"), "bin")
	writeFile(t, filepath.Join(root, "schema", "demo.tag.schema.json"), "{}")
	writeFile(t, filepath.Join(root, "frontend", "dist", "assets", "app.js"), "console.log('demo')")
	writeFile(t, filepath.Join(root, "frontend", "dist", ".vite", "manifest.json"), "{}")
	writeFile(t, filepath.Join(root, "frontend", "src", "App.vue"), "<template />")
	result, err := PackagePlugin(PluginPackageOptions{
		SourceDir:       root,
		OutputDir:       filepath.Join(root, "release"),
		Code:            "demo",
		Name:            "Demo",
		Version:         "0.1.0",
		ProtocolRange:   ">=0.1.0 <0.2.0",
		ManifestJSON:    []byte(`{"code":"demo","version":"0.1.0"}`),
		ReferencedFiles: []string{"schema/demo.tag.schema.json"},
		CreatedAt:       time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("PackagePlugin failed: %v", err)
	}
	if _, err := os.Stat(result.ArchivePath); err != nil {
		t.Fatalf("archive missing: %v", err)
	}
	if result.ChecksumPath != "checksums.txt" {
		t.Fatalf("ChecksumPath = %q, want checksums.txt", result.ChecksumPath)
	}
	if _, err := os.Stat(result.ArchivePath + ".sha256"); !os.IsNotExist(err) {
		t.Fatalf("external checksum file exists or stat failed: %v", err)
	}
	contents := archiveContents(t, result.ArchivePath)
	entries := sortedKeys(contents)
	for _, want := range []string{
		"plugin.yaml",
		"manifest.json",
		"release.json",
		"checksums.txt",
		"README.md",
		"schema/demo.tag.schema.json",
		"bin/" + binaryName("demo"),
		"frontend/assets/app.js",
	} {
		if !slices.Contains(entries, want) {
			t.Fatalf("archive entries = %#v, want %s", entries, want)
		}
	}
	if slices.Contains(entries, "release/bin/demo") || slices.Contains(entries, "frontend/src/App.vue") || slices.Contains(entries, "frontend/.vite/manifest.json") {
		t.Fatalf("archive entries contain source or old release path: %#v", entries)
	}
	if !strings.Contains(string(contents["manifest.json"]), `"code":"demo"`) {
		t.Fatalf("manifest.json = %s", contents["manifest.json"])
	}
	var release struct {
		Format string `json:"format"`
		Plugin struct {
			Code    string `json:"code"`
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"plugin"`
		Protocol struct {
			Range string `json:"range"`
		} `json:"protocol"`
		Artifacts struct {
			Manifest string `json:"manifest"`
			Backend  string `json:"backend"`
			Frontend string `json:"frontend"`
		} `json:"artifacts"`
		SourceFiles []string `json:"source_files"`
		CreatedAt   string   `json:"created_at"`
	}
	if err := json.Unmarshal(contents["release.json"], &release); err != nil {
		t.Fatalf("decode release.json failed: %v", err)
	}
	if release.Format != "sparq.plugin.package.v1" ||
		release.Plugin.Code != "demo" ||
		release.Plugin.Name != "Demo" ||
		release.Plugin.Version != "0.1.0" ||
		release.Protocol.Range != ">=0.1.0 <0.2.0" ||
		release.Artifacts.Manifest != "manifest.json" ||
		release.Artifacts.Backend != "bin/"+binaryName("demo") ||
		release.Artifacts.Frontend != "frontend" ||
		release.CreatedAt != "2026-08-02T00:00:00Z" {
		t.Fatalf("release.json = %#v", release)
	}
	if strings.Join(release.SourceFiles, ",") != "plugin.yaml,schema/demo.tag.schema.json" {
		t.Fatalf("source_files = %#v", release.SourceFiles)
	}
	checksums := string(contents["checksums.txt"])
	for _, want := range []string{"plugin.yaml", "manifest.json", "release.json", "schema/demo.tag.schema.json", "bin/" + binaryName("demo"), "frontend/assets/app.js"} {
		if !strings.Contains(checksums, "  "+want+"\n") {
			t.Fatalf("checksums.txt = %s, want %s", checksums, want)
		}
	}
	if strings.Contains(checksums, "checksums.txt") {
		t.Fatalf("checksums.txt should not include itself: %s", checksums)
	}
}

func TestPackagePluginCreatesManagedRemoteArchive(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "plugin.yaml"), "apiVersion: sparq.io/v1\n")
	backend := filepath.Join(root, "matrix", "linux", "amd64", "cms")
	frontend := filepath.Join(root, "web-output")
	writeFile(t, backend, "linux/amd64")
	writeFile(t, filepath.Join(frontend, "assets", "app.js"), "export default {}")

	result, err := PackagePlugin(PluginPackageOptions{
		SourceDir: root, OutputDir: filepath.Join(root, "release"), Code: "cms", Name: "CMS", Version: "1.2.3",
		ManifestJSON: []byte(`{"code":"cms","version":"1.2.3"}`), DeliveryKind: DeliveryKindManagedRemote,
		Runtime: "http", TargetOS: "linux", TargetArch: "amd64", BackendPath: backend, FrontendPath: frontend,
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(result.ArchivePath) != "cms-1.2.3-linux-amd64.tar.gz" {
		t.Fatalf("ArchivePath = %s", result.ArchivePath)
	}
	contents := archiveContents(t, result.ArchivePath)
	if string(contents["bin/cms"]) != "linux/amd64" || string(contents["frontend/assets/app.js"]) != "export default {}" {
		t.Fatalf("managed remote entries = %#v", sortedKeys(contents))
	}
	var release struct {
		Artifacts struct {
			Backend  string `json:"backend"`
			Frontend string `json:"frontend"`
		} `json:"artifacts"`
		Delivery releaseDeliveryJSON `json:"delivery"`
	}
	if err := json.Unmarshal(contents["release.json"], &release); err != nil {
		t.Fatal(err)
	}
	if release.Artifacts.Backend != "bin/cms" || release.Artifacts.Frontend != "frontend" ||
		release.Delivery.Kind != DeliveryKindManagedRemote || release.Delivery.Runtime != "http" ||
		release.Delivery.TargetOS != "linux" || release.Delivery.TargetArch != "amd64" {
		t.Fatalf("release.json = %#v", release)
	}
}

func TestPackagePluginRejectsManagedRemoteWithoutFrontend(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "plugin.yaml"), "apiVersion: sparq.io/v1\n")
	backend := filepath.Join(root, "cms")
	writeFile(t, backend, "binary")
	_, err := PackagePlugin(PluginPackageOptions{
		SourceDir: root, Code: "cms", Version: "1.2.3", ManifestJSON: []byte(`{"code":"cms"}`),
		DeliveryKind: DeliveryKindManagedRemote, Runtime: "http", TargetOS: "linux", TargetArch: "amd64", BackendPath: backend,
	})
	if err == nil || !strings.Contains(err.Error(), "前端构建产物") {
		t.Fatalf("error = %v", err)
	}
}

func TestPackagePluginRejectsReferencedFileOutsideSource(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "plugin.yaml"), "apiVersion: sparq.io/v1\n")
	_, err := PackagePlugin(PluginPackageOptions{
		SourceDir:       root,
		OutputDir:       filepath.Join(root, "release"),
		Code:            "demo",
		Version:         "0.1.0",
		ManifestJSON:    []byte(`{"code":"demo"}`),
		ReferencedFiles: []string{"../secret.json"},
	})
	if err == nil {
		t.Fatal("PackagePlugin error = nil, want outside source error")
	}
}

func TestPackagePluginIsDeterministicWithoutCreatedAt(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "plugin.yaml"), "apiVersion: sparq.io/v1\n")
	writeFile(t, filepath.Join(root, "release", "bin", "demo"), "binary")
	options := PluginPackageOptions{
		SourceDir: root, OutputDir: filepath.Join(root, "release"), Code: "demo", Name: "Demo", Version: "0.1.0",
		ProtocolRange: ">=0.1.0 <0.2.0", ManifestJSON: []byte(`{"code":"demo","version":"0.1.0"}`),
	}
	first, err := PackagePlugin(options)
	if err != nil {
		t.Fatal(err)
	}
	firstBody, err := os.ReadFile(first.ArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first.ArchivePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(first.ArchivePath); err != nil {
		t.Fatal(err)
	}
	second, err := PackagePlugin(options)
	if err != nil {
		t.Fatal(err)
	}
	secondBody, err := os.ReadFile(second.ArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	firstSum := sha256.Sum256(firstBody)
	secondSum := sha256.Sum256(secondBody)
	if firstSum != secondSum {
		t.Fatalf("same package input produced different SHA-256: %x != %x", firstSum, secondSum)
	}
	contents := archiveContents(t, second.ArchivePath)
	var release struct {
		CreatedAt string `json:"created_at"`
	}
	if err := json.Unmarshal(contents["release.json"], &release); err != nil {
		t.Fatal(err)
	}
	if release.CreatedAt != "1970-01-01T00:00:00Z" {
		t.Fatalf("default created_at = %q", release.CreatedAt)
	}
}

func archiveContents(t *testing.T, archivePath string) map[string][]byte {
	t.Helper()
	file, err := os.Open(archivePath)
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
	entries := make(map[string][]byte)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return entries
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		entries[header.Name] = body
	}
}

func sortedKeys(values map[string][]byte) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func writeFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}
