package localplugins

import (
	"bytes"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zane-xin/sparq-tool/internal/plugin/manifest"
)

func TestGenerateIncludesLocalPlugins(t *testing.T) {
	root := t.TempDir()
	writeLocalPluginFile(t, filepath.Join(root, "go.work"), "use (\n\t./plugins/cms\n\t./sparq-app\n)\n")
	writeLocalPluginFile(t, filepath.Join(root, "plugins", "cms", "plugin.yaml"), `apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: cms
  code: cms
  name: CMS
  version: 0.1.0
  description: 内容管理
  docs: https://docs.example.com/cms
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec:
  runtime:
    supported:
      - local
  entries:
    - name: admin
      title: CMS Admin
      type: page
      sandbox: iframe
    - name: summary
      title: CMS Summary
      type: component
      sandbox: iframe
      load_mode: local
  capabilities:
    - code: cms.summary
      kind: dashboard-widget
      title: CMS Summary
      entry: summary
      required_permissions:
        - cms.summary.view
  contributions:
    version: 2
    features:
      - code: cms.summary
        name: CMS summary
        description: Read CMS summary metrics
        module: cms
        resource: summary
        permissions:
          - action: view
            name: View CMS summary
            usage_role: entry
            data_scope_policy: not_applicable
    menus:
      - code: cms.articles
        type: page
        title: CMS Articles
        icon: document
        sort: 20
        visible: true
        enabled: true
        entry_code: admin
        entry_payload:
          route: /articles
        feature_code: cms.summary
        entry_permission_action: view
`)
	writeLocalPluginFile(t, filepath.Join(root, "plugins", "cms", "go.mod"), "module example.com/cms\n")
	writeLocalPluginFile(t, filepath.Join(root, "plugins", "cms", "bootstrap.go"), "package cms\n")
	generateLocalPluginManifest(t, filepath.Join(root, "plugins", "cms", "plugin.yaml"))
	result, err := Generate(GenerateOptions{Root: root})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(result.Entries))
	}
	content := string(result.Content)
	if !strings.Contains(content, `cmsplugin "example.com/cms"`) {
		t.Fatalf("generated content missing import: %s", content)
	}
	for _, expected := range []string{
		`var sparqPluginPackages = []plugin.PluginPackage{`, `mustPluginPackage(`, `cmsplugin.Manifest()`, `Register: cmsplugin.Register`,
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("generated content missing %q: %s", expected, content)
		}
	}
	if strings.Contains(content, "ManifestSnapshot") || strings.Contains(content, "RegisterRuntime") || strings.Contains(content, "plugin.Scope") ||
		strings.Contains(content, "RealmCode") || strings.Contains(content, "ApplicationCode") {
		t.Fatalf("generated entry contains runtime or HostBinding assembly: %s", content)
	}
	formatted, err := format.Source(result.Content)
	if err != nil {
		t.Fatalf("generated content is invalid Go: %v", err)
	}
	if !bytes.Equal(formatted, result.Content) {
		t.Fatal("generated content is not gofmt stable")
	}
}

func TestGenerateSkipsHTTPOnlyPlugins(t *testing.T) {
	root := t.TempDir()
	writeLocalPluginFile(t, filepath.Join(root, "go.work"), "use ./plugins/cms\n")
	writeLocalPluginFile(t, filepath.Join(root, "plugins", "cms", "plugin.yaml"), `apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: cms
  code: cms
  name: cms
  version: 0.1.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec:
  runtime:
    supported:
      - http
`)
	writeLocalPluginFile(t, filepath.Join(root, "plugins", "cms", "go.mod"), "module example.com/cms\n")
	result, err := Generate(GenerateOptions{Root: root})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(result.Entries) != 0 {
		t.Fatalf("entry count = %d, want 0", len(result.Entries))
	}
	if !strings.Contains(string(result.Content), "var sparqPluginPackages = []plugin.PluginPackage{}") {
		t.Fatalf("unexpected empty generated content: %s", result.Content)
	}
}

func TestGenerateSelectsGoLocalComponentFromMixedPackage(t *testing.T) {
	root := t.TempDir()
	writeLocalPluginFile(t, filepath.Join(root, "go.work"), "use ./plugins/mixed\n")
	manifestPath := filepath.Join(root, "plugins", "mixed", "plugin.yaml")
	writeLocalPluginFile(t, manifestPath, `apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: mixed
  code: mixed
  name: Mixed
  version: 1.0.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec:
  components:
    - id: api
      kind: backend
      language: go
      runtime: [local, http]
    - id: jobs
      kind: worker
      language: lua
      runtime: [local]
    - id: admin
      kind: frontend
      language: typescript
`)
	writeLocalPluginFile(t, filepath.Join(root, "plugins", "mixed", "go.mod"), "module example.com/mixed\n")
	writeLocalPluginFile(t, filepath.Join(root, "plugins", "mixed", "bootstrap.go"), "package mixed\n")
	generateLocalPluginManifest(t, manifestPath)
	result, err := Generate(GenerateOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 1 || result.Entries[0].ComponentID != "api" {
		t.Fatalf("entries = %#v", result.Entries)
	}
	content := string(result.Content)
	for _, expected := range []string{
		`var sparqPluginPackages = []plugin.PluginPackage{`,
		`mustPluginPackage(`,
		`mixedplugin.Manifest()`,
		`plugin.PluginComponent{`,
		`ID:       "api"`,
		`Register: mixedplugin.Register`,
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("generated content missing %q: %s", expected, content)
		}
	}
	if strings.Contains(content, `ComponentID: "api"`) {
		t.Fatalf("explicit component was generated through a legacy component wrapper: %s", content)
	}
}

func TestGenerateRejectsStalePluginManifest(t *testing.T) {
	root := t.TempDir()
	writeLocalPluginFile(t, filepath.Join(root, "go.work"), "use ./plugins/cms\n")
	manifestPath := filepath.Join(root, "plugins", "cms", "plugin.yaml")
	writeLocalPluginFile(t, manifestPath, `apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: cms
  code: cms
  name: CMS
  version: 0.1.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec:
  runtime:
    supported: [local]
`)
	writeLocalPluginFile(t, filepath.Join(root, "plugins", "cms", "bootstrap.go"), "package cms\n")
	writeLocalPluginFile(t, filepath.Join(root, "plugins", "cms", "go.mod"), "module example.com/cms\n")
	generateLocalPluginManifest(t, manifestPath)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, bytes.ReplaceAll(data, []byte("0.1.0"), []byte("0.2.0")), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(GenerateOptions{Root: root}); err == nil || !strings.Contains(err.Error(), "manifest_gen.go") {
		t.Fatalf("Generate error = %v, want stale manifest error", err)
	}
}

func TestGenerateUsesCommandPackageOutputByDefault(t *testing.T) {
	root := t.TempDir()
	writeLocalPluginFile(t, filepath.Join(root, "go.work"), "use ./sparq-app\n")
	result, err := Generate(GenerateOptions{Root: root})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	want := filepath.Join(root, "sparq-app", "cmd", "sparq", "plugins_gen.go")
	if result.Output != want {
		t.Fatalf("Output = %q, want %q", result.Output, want)
	}
}

func TestDiscoverReadsPluginsFromGoWorkUse(t *testing.T) {
	root := t.TempDir()
	writeLocalPluginFile(t, filepath.Join(root, "go.work"), "use (\n\t./modules/cms\n\t./modules/plain\n)\n")
	writeLocalPluginFile(t, filepath.Join(root, "modules", "cms", "plugin.yaml"), `apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: cms
  code: cms
  name: CMS
  version: 0.1.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec:
  runtime:
    supported: [local]
`)
	writeLocalPluginFile(t, filepath.Join(root, "modules", "cms", "bootstrap.go"), "package cms\n")
	writeLocalPluginFile(t, filepath.Join(root, "modules", "cms", "go.mod"), "module example.com/cms\n")
	writeLocalPluginFile(t, filepath.Join(root, "modules", "plain", "go.mod"), "module example.com/plain\n")

	entries, err := Discover(DiscoverOptions{Root: root})
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(entries))
	}
	if entries[0].Code != "cms" || entries[0].Path != "modules/cms" {
		t.Fatalf("entry = %#v", entries[0])
	}
}

func TestParseGoWorkUsePaths(t *testing.T) {
	paths, err := parseGoWorkUsePaths("use ./single\nuse (\n\t./plugins/cms\n\t\"./plugins/seo\"\n)\n")
	if err != nil {
		t.Fatalf("parseGoWorkUsePaths failed: %v", err)
	}
	if strings.Join(paths, ",") != "./single,./plugins/cms,./plugins/seo" {
		t.Fatalf("paths = %#v", paths)
	}
}

func writeLocalPluginFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}

func generateLocalPluginManifest(t *testing.T, path string) {
	t.Helper()
	result, err := manifest.GenerateGo(manifest.GoGenerateOptions{ManifestPath: path})
	if err != nil {
		t.Fatalf("GenerateGo failed: %v", err)
	}
	if err := manifest.WriteGo(result); err != nil {
		t.Fatalf("WriteGo failed: %v", err)
	}
}
