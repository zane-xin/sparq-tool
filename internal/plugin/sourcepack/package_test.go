package sourcepack

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/zane-xin/sparq-tool/internal/plugin/manifest"
)

func TestCreateBuildsSourceArchiveAndExcludesLocalFiles(t *testing.T) {
	root := t.TempDir()
	writePluginFile(t, root, "plugin.yaml", testManifest)
	writePluginFile(t, root, "go.mod", "module example.com/cms\n\ngo 1.25\n")
	writePluginFile(t, root, "bootstrap.go", "package cms\n")
	writePluginFile(t, root, "internal/service.go", "package internal\n")
	writePluginFile(t, root, "frontend/src/main.ts", "export {}\n")
	writePluginFile(t, root, "frontend/dist/entry.js", "built\n")
	writePluginFile(t, root, "frontend/node_modules/pkg/index.js", "dependency\n")
	writePluginFile(t, root, ".env", "SECRET=value\n")
	writePluginFile(t, root, "runtime.sqlite", "database\n")
	generated, err := manifest.GenerateGo(manifest.GoGenerateOptions{ManifestPath: filepath.Join(root, "plugin.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.WriteGo(generated); err != nil {
		t.Fatal(err)
	}

	result, err := Create(Options{SourceDir: root, OutputDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != "cms" || result.Version != "1.2.3" || len(result.SHA256) != 64 || result.SizeBytes == 0 {
		t.Fatalf("result = %+v", result)
	}
	entries := archiveEntries(t, result.ArchivePath)
	want := []string{"cms/bootstrap.go", "cms/frontend/src/main.ts", "cms/go.mod", "cms/internal/service.go", "cms/manifest_gen.go", "cms/plugin.yaml"}
	if strings.Join(entries, ",") != strings.Join(want, ",") {
		t.Fatalf("archive entries = %v, want %v", entries, want)
	}
}

func TestCreateRejectsStaleGeneratedManifest(t *testing.T) {
	root := t.TempDir()
	writePluginFile(t, root, "plugin.yaml", testManifest)
	writePluginFile(t, root, "go.mod", "module example.com/cms\n\ngo 1.25\n")
	writePluginFile(t, root, "bootstrap.go", "package cms\n")
	writePluginFile(t, root, "manifest_gen.go", "package cms\n")
	_, err := Create(Options{SourceDir: root, OutputDir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "已过期") {
		t.Fatalf("Create() error = %v", err)
	}
}

func TestFingerprintTracksSourcesButIgnoresGeneratedOutputs(t *testing.T) {
	root := t.TempDir()
	writePluginFile(t, root, "plugin.yaml", testManifest)
	writePluginFile(t, root, "go.mod", "module example.com/cms\n\ngo 1.25\n")
	writePluginFile(t, root, "bootstrap.go", "package cms\n")
	writePluginFile(t, root, "frontend/src/main.ts", "export const version = 1\n")
	writePluginFile(t, root, "manifest_gen.go", "generated one\n")
	writePluginFile(t, root, "frontend/artifact.json", `{"generated":1}`)
	writePluginFile(t, root, "frontend/dist/entry.js", "built one\n")
	writePluginFile(t, root, "release/cms.tar.gz", "package one\n")
	first, err := Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	writePluginFile(t, root, "manifest_gen.go", "generated two\n")
	writePluginFile(t, root, "frontend/artifact.json", `{"generated":2}`)
	writePluginFile(t, root, "frontend/dist/entry.js", "built two\n")
	writePluginFile(t, root, "release/cms.tar.gz", "package two\n")
	generatedChanged, err := Fingerprint(root)
	if err != nil || generatedChanged.SHA256 != first.SHA256 {
		t.Fatalf("generated output fingerprint = %+v, %v, want %s", generatedChanged, err, first.SHA256)
	}
	writePluginFile(t, root, "frontend/src/main.ts", "export const version = 2\n")
	sourceChanged, err := Fingerprint(root)
	if err != nil || sourceChanged.SHA256 == first.SHA256 {
		t.Fatalf("source fingerprint = %+v, %v, old %s", sourceChanged, err, first.SHA256)
	}
}

func TestCreateRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	writePluginFile(t, root, "plugin.yaml", testManifest)
	writePluginFile(t, root, "go.mod", "module example.com/cms\n\ngo 1.25\n")
	writePluginFile(t, root, "bootstrap.go", "package cms\n")
	generated, err := manifest.GenerateGo(manifest.GoGenerateOptions{ManifestPath: filepath.Join(root, "plugin.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.WriteGo(generated); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "plugin.yaml"), filepath.Join(root, "linked.yaml")); err != nil {
		t.Fatal(err)
	}
	_, err = Create(Options{SourceDir: root, OutputDir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "符号链接") {
		t.Fatalf("Create() error = %v", err)
	}
}

func TestCreateAppliesSparqIgnoreWithoutExposingProtectedFiles(t *testing.T) {
	root := t.TempDir()
	writePluginFile(t, root, "plugin.yaml", testManifest)
	writePluginFile(t, root, "go.mod", "module example.com/cms\n\ngo 1.25\n")
	writePluginFile(t, root, "go.sum", "dependency checksum\n")
	writePluginFile(t, root, "sparq.build.yaml", "version: 1\n")
	writePluginFile(t, root, "bootstrap.go", "package cms\n")
	writePluginFile(t, root, "docs/public.md", "public\n")
	writePluginFile(t, root, "docs/private.md", "private\n")
	writePluginFile(t, root, "cache/generated.txt", "generated\n")
	writePluginFile(t, root, ".npmrc", "//registry.example.test/:_authToken=secret\n")
	writePluginFile(t, root, "deploy/private.key", "secret\n")
	writePluginFile(t, root, ".sparqignore", "docs/**\n!docs/public.md\ncache/\nmanifest_gen.go\ngo.sum\nsparq.build.yaml\n!.npmrc\n")
	generated, err := manifest.GenerateGo(manifest.GoGenerateOptions{ManifestPath: filepath.Join(root, "plugin.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.WriteGo(generated); err != nil {
		t.Fatal(err)
	}

	result, err := Create(Options{SourceDir: root, OutputDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	entries := archiveEntries(t, result.ArchivePath)
	want := []string{"cms/.sparqignore", "cms/bootstrap.go", "cms/docs/public.md", "cms/go.mod", "cms/go.sum", "cms/manifest_gen.go", "cms/plugin.yaml", "cms/sparq.build.yaml"}
	if strings.Join(entries, ",") != strings.Join(want, ",") {
		t.Fatalf("archive entries = %v, want %v", entries, want)
	}
}

func TestCreateRejectsSymlinkedSparqIgnore(t *testing.T) {
	root := t.TempDir()
	writePluginFile(t, root, "plugin.yaml", testManifest)
	writePluginFile(t, root, "go.mod", "module example.com/cms\n\ngo 1.25\n")
	writePluginFile(t, root, "bootstrap.go", "package cms\n")
	generated, err := manifest.GenerateGo(manifest.GoGenerateOptions{ManifestPath: filepath.Join(root, "plugin.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.WriteGo(generated); err != nil {
		t.Fatal(err)
	}
	rules := filepath.Join(t.TempDir(), "rules")
	if err := os.WriteFile(rules, []byte("cache/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(rules, filepath.Join(root, ".sparqignore")); err != nil {
		t.Fatal(err)
	}
	_, err = Create(Options{SourceDir: root, OutputDir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), ".sparqignore 必须是普通文件") {
		t.Fatalf("Create() error = %v", err)
	}
}

func TestCreateRejectsInvalidSparqIgnoreRule(t *testing.T) {
	root := t.TempDir()
	writePluginFile(t, root, "plugin.yaml", testManifest)
	writePluginFile(t, root, "go.mod", "module example.com/cms\n\ngo 1.25\n")
	writePluginFile(t, root, "bootstrap.go", "package cms\n")
	writePluginFile(t, root, ".sparqignore", "../outside\n")
	generated, err := manifest.GenerateGo(manifest.GoGenerateOptions{ManifestPath: filepath.Join(root, "plugin.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.WriteGo(generated); err != nil {
		t.Fatal(err)
	}
	_, err = Create(Options{SourceDir: root, OutputDir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), ".sparqignore") || !strings.Contains(err.Error(), "路径模式无效") {
		t.Fatalf("Create() error = %v", err)
	}
}

func TestParseIgnoreRuleKeepsEscapedNegationLiteral(t *testing.T) {
	rule, included, err := parseIgnoreRule(`\!important.txt`)
	if err != nil || !included || rule.negated || rule.pattern != "!important.txt" {
		t.Fatalf("rule = %+v, included = %v, error = %v", rule, included, err)
	}
}

func TestIgnoreMatcherKeepsManifestReferencedFiles(t *testing.T) {
	rule, included, err := parseIgnoreRule("schema/**")
	if err != nil || !included {
		t.Fatal(err)
	}
	matcher := ignoreMatcher{rules: []ignoreRule{rule}, required: requiredSourcePaths([]string{"schema/cms.documents.schema.json"})}
	if matcher.excluded("schema/cms.documents.schema.json", false) {
		t.Fatal("Manifest 引用的 Schema 文件不能被 .sparqignore 排除")
	}
}

func archiveEntries(t *testing.T, archivePath string) []string {
	t.Helper()
	file, err := os.Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	var entries []string
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, header.Name)
	}
	sort.Strings(entries)
	return entries
}

func writePluginFile(t *testing.T, root, relative, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const testManifest = `apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: cms
  code: cms
  name: CMS
  version: 1.2.3
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec:
  runtime:
    supported:
      - local
`
