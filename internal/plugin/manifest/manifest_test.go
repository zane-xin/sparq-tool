package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	permissionprotocol "github.com/zane-xin/sparq-protocol/contracts/permission"
	"github.com/zane-xin/sparq-protocol/plugin"
)

func TestLoadValidYAMLManifest(t *testing.T) {
	path := writeManifest(t, `
apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: storage-memory
  code: storage-memory
  name: Storage Memory
  version: 0.1.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec:
  depends_on: [settings]
  runtime:
    supported:
      - local
      - http
`)
	item, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if item.Metadata.Code != "storage-memory" || len(item.Spec.Runtime.Supported) != 2 ||
		len(item.Spec.DependsOn) != 1 || item.Spec.DependsOn[0] != "settings" {
		t.Fatalf("unexpected manifest: %#v", item)
	}
	protocolManifest, err := item.ProtocolManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(protocolManifest.DependsOn) != 1 || protocolManifest.DependsOn[0] != "settings" {
		t.Fatalf("protocol dependencies = %v", protocolManifest.DependsOn)
	}
}

func TestLoadMixedComponentManifest(t *testing.T) {
	path := writeManifest(t, `
apiVersion: sparq.io/v1
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
      dependencies:
        - component: api
          required: true
    - id: admin
      kind: frontend
      language: typescript
  provides:
    components:
      api: [mixed.read]
  requires:
    components:
      jobs: [settings.values.get]
  entries:
    - component: admin
      name: admin
      title: Mixed Admin
      type: page
      sandbox: iframe
      load_mode: local
      local_path: /plugins/mixed/admin/index.html
`)
	item, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	protocolManifest, err := item.ProtocolManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(protocolManifest.Components) != 3 || protocolManifest.Components[1].Dependencies[0].Component != "api" {
		t.Fatalf("components = %#v", protocolManifest.Components)
	}
	if len(protocolManifest.Provides) != 1 || protocolManifest.Provides[0].Component != "api" ||
		len(protocolManifest.Requires) != 1 || protocolManifest.Requires[0].Component != "jobs" {
		t.Fatalf("capability ownership = %#v, %#v", protocolManifest.Provides, protocolManifest.Requires)
	}
}

func TestGenerateGoUsesManifestSourceSet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultFileName)
	if err := os.WriteFile(path, []byte(`apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: reports
  code: reports
  name: Reports
  version: 0.1.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec:
  runtime:
    supported: [local, http]
  provides:
    capabilities: [reports.summary]
  contributions:
    version: 1
    features:
      - code: reports.tag
        name: Report tags
        module: reports
        resource: tag
        permissions:
          - action: list
            name: View report tags
            usage_role: entry
            data_scope_policy: not_applicable
    schema_resources:
      - file: schema/reports.tag.schema.json
        migration_owner: domain
        runtime_owner: domain
`), 0o644); err != nil {
		t.Fatal(err)
	}
	schemaPath := filepath.Join(dir, "schema", "reports.tag.schema.json")
	writeFile(t, schemaPath, `{
  "resource": "reports.tag",
  "table_name": "reports_tags",
  "schema": {"name": "tag", "title": "Tag"},
  "action_permissions": {"list": "reports.tag.list"}
}`)
	if err := os.WriteFile(filepath.Join(dir, "bootstrap.go"), []byte("package reports\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	item, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if files := item.ReferencedFiles(); len(files) != 1 || files[0] != "schema/reports.tag.schema.json" {
		t.Fatalf("ReferencedFiles = %#v", files)
	}
	if owner := item.Spec.Contributions.SchemaResources[0].MigrationOwner; owner != "domain" {
		t.Fatalf("schema resource migration owner = %q", owner)
	}
	if owner := item.Spec.Contributions.SchemaResources[0].RuntimeOwner; owner != plugin.SchemaRuntimeOwnerDomain {
		t.Fatalf("schema resource runtime owner = %q", owner)
	}
	result, err := GenerateGo(GoGenerateOptions{ManifestPath: path})
	if err != nil {
		t.Fatalf("GenerateGo failed: %v", err)
	}
	if !strings.Contains(string(result.Content), "package reports") ||
		!strings.Contains(string(result.Content), `reports.summary`) ||
		!strings.Contains(string(result.Content), `reports.tag`) ||
		!strings.Contains(string(result.Content), `migration_owner`) ||
		!strings.Contains(string(result.Content), `runtime_owner`) ||
		!strings.Contains(string(result.Content), "func Manifest() plugin.Manifest") {
		t.Fatalf("generated manifest = %s", result.Content)
	}
	if strings.Contains(string(result.Content), "pluginmanifest.Compose(value)") {
		t.Fatalf("不存在 %s 目录时不应组合 Go Manifest: %s", ManifestModuleDirName, result.Content)
	}
	if err := WriteGo(result); err != nil {
		t.Fatalf("WriteGo failed: %v", err)
	}
	if err := CheckGo(result); err != nil {
		t.Fatalf("CheckGo failed: %v", err)
	}
	unchangedTime := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(result.Output, unchangedTime, unchangedTime); err != nil {
		t.Fatal(err)
	}
	if err := WriteGo(result); err != nil {
		t.Fatalf("WriteGo unchanged result failed: %v", err)
	}
	info, err := os.Stat(result.Output)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(unchangedTime) {
		t.Fatalf("WriteGo changed unchanged output mtime to %s", info.ModTime())
	}
	updated := strings.ReplaceAll(string(mustReadFile(t, schemaPath)), `"title": "Tag"`, `"title": "Updated Tag"`)
	if err := os.WriteFile(schemaPath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	stale, err := GenerateGo(GoGenerateOptions{ManifestPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckGo(stale); err == nil {
		t.Fatal("CheckGo error = nil, want stale generated file")
	}
}

func TestGenerateGoMergesFrontendArtifact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultFileName)
	writeFile(t, path, `apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: reports
  code: reports
  name: Reports
  version: 0.1.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec:
  runtime:
    supported: [local]
  frontend:
    component: admin
    host_api: v1
    runtime_profile: admin-v1
`)
	writeFile(t, filepath.Join(dir, "bootstrap.go"), "package reports\n")
	writeFile(t, filepath.Join(dir, "frontend", "artifact.json"), `{"version":"0.1.0-abc123","entry":"modules/designer/entry.mjs","files":{"modules/designer/entry.mjs":"sha256-abc"}}`)
	result, err := GenerateGo(GoGenerateOptions{ManifestPath: path})
	if err != nil {
		t.Fatalf("GenerateGo failed: %v", err)
	}
	if !strings.Contains(string(result.Content), "modules/designer/entry.mjs") ||
		!strings.Contains(string(result.Content), "sha256-abc") {
		t.Fatalf("generated artifact missing: %s", result.Content)
	}
}

func TestCloneFrontendDoesNotShareArtifactState(t *testing.T) {
	original := &plugin.FrontendManifest{Artifact: &plugin.FrontendArtifact{
		Styles: []string{"frontend.css"}, Files: map[string]string{"frontend.css": "sha256-style"},
		Icons: map[string]string{"article": "icons/article.svg"},
	}}
	cloned := cloneFrontend(original)
	cloned.Artifact.Styles[0] = "changed.css"
	cloned.Artifact.Files["frontend.css"] = "changed"
	cloned.Artifact.Icons["article"] = "changed.svg"
	if original.Artifact.Styles[0] != "frontend.css" || original.Artifact.Files["frontend.css"] != "sha256-style" || original.Artifact.Icons["article"] != "icons/article.svg" {
		t.Fatalf("cloneFrontend shared artifact state: %#v", original.Artifact)
	}
}

func TestGenerateGoDiscoversConventionalFrontendRoot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultFileName)
	writeFile(t, path, `apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: reports
  code: reports
  name: Reports
  version: 0.1.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec:
  components:
    - id: admin-ui
      kind: frontend
      language: typescript
`)
	writeFile(t, filepath.Join(dir, "bootstrap.go"), "package reports\n")
	writeFile(t, filepath.Join(dir, "frontend", "src", "frontend.ts"), "export default () => ({})\n")
	writeFile(t, filepath.Join(dir, "frontend", "artifact.json"), `{"version":"0.1.0-abc123","entry":"frontend.mjs","styles":["frontend.css"],"files":{"frontend.mjs":"sha256-admin","frontend.css":"sha256-style"}}`)

	result, err := GenerateGo(GoGenerateOptions{ManifestPath: path, RequireFrontendArtifacts: true})
	if err != nil {
		t.Fatal(err)
	}
	generated := string(result.Content)
	for _, expected := range []string{"applyFrontendArtifacts", "frontend.mjs", "frontend.css", "Styles: artifact.Styles"} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("generated admin module missing %q: %s", expected, generated)
		}
	}
}

func TestGenerateGoAllowsTrustedFrontendEntryWithoutEntryArtifact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultFileName)
	writeFile(t, path, `apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: cms
  code: cms
  name: CMS
  version: 0.1.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec:
  components:
    - id: admin
      kind: frontend
      language: typescript
  frontend:
    component: admin
    host_api: v1
    runtime_profile: admin-v1
  entries:
    - component: admin
      name: admin
      title: CMS 管理
      type: full-app
      sandbox: trusted
      load_mode: local
`)
	writeFile(t, filepath.Join(dir, "bootstrap.go"), "package cms\n")

	result, err := GenerateGo(GoGenerateOptions{ManifestPath: path})
	if err != nil {
		t.Fatalf("GenerateGo failed for trusted frontend entry: %v", err)
	}
	if strings.Contains(string(result.Content), "frontendEntryArtifactJSON") {
		t.Fatalf("trusted frontend entry should not embed entry artifact: %s", result.Content)
	}
}

func TestMergeFrontendEntryArtifactIgnoresStaleArtifactForTrustedEntry(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, DefaultFileName)
	writeFile(t, filepath.Join(dir, "frontend", "entry-artifact.json"), "not-json")
	value := plugin.Manifest{
		Components: []plugin.ComponentManifest{{ID: "admin", Kind: plugin.ComponentKindFrontend, Frontend: &plugin.FrontendComponent{}}},
		Entries: []plugin.EntryManifest{{
			Component: "admin", Name: "admin", Title: "CMS 管理", Type: plugin.EntryTypeFullApp,
			Sandbox: plugin.EntrySandboxTrusted, LoadMode: plugin.EntryLoadModeLocal,
		}},
	}
	if err := MergeFrontendEntryArtifact(&value, manifestPath); err != nil {
		t.Fatalf("MergeFrontendEntryArtifact failed for trusted entry: %v", err)
	}
}

/*
	func TestGenerateGoMergesFrontendArtifactArray(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, DefaultFileName)
		writeFile(t, path, `apiVersion: sparq.io/v1

kind: Plugin
metadata:

	id: reports
	code: reports
	name: Reports
	version: 0.1.0

sparq:

	protocol: ">=0.1.0 <0.2.0"

spec:

	runtime:
	  supported: [local]
	frontend_modules:
	  - id: designer
	    component: admin
	    host_api: v1
	    provides:
	      designer_steps: [reports.rules]
	  - id: admin-extension
	    component: admin
	    host_api: v1
	    provides:
	      admin_extensions: [schema-page]

`)

		writeFile(t, filepath.Join(dir, "bootstrap.go"), "package reports\n")
		writeFile(t, filepath.Join(dir, "frontend", "artifact.json"), `[
	  {"module":"designer","version":"0.1.0-abc123","entry":"modules/designer/entry.mjs","files":{"modules/designer/entry.mjs":"sha256-designer"}},
	  {"module":"admin-extension","version":"0.1.0-def456","entry":"modules/admin-extension/entry.mjs","files":{"modules/admin-extension/entry.mjs":"sha256-extension"}}

]`)

		result, err := GenerateGo(GoGenerateOptions{ManifestPath: path, RequireFrontendArtifacts: true})
		if err != nil {
			t.Fatalf("GenerateGo failed: %v", err)
		}
		generated := string(result.Content)
		for _, expected := range []string{"modules/designer/entry.mjs", "sha256-designer", "modules/admin-extension/entry.mjs", "sha256-extension"} {
			if !strings.Contains(generated, expected) {
				t.Fatalf("generated artifact missing %q: %s", expected, generated)
			}
		}
	}

	func TestGenerateGoRejectsEmptyFrontendArtifactArray(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, DefaultFileName)
		writeFile(t, path, `apiVersion: sparq.io/v1

kind: Plugin
metadata:

	id: reports
	code: reports
	name: Reports
	version: 0.1.0

sparq:

	protocol: ">=0.1.0 <0.2.0"

spec:

	runtime:
	  supported: [local]
	frontend_modules:
	  - id: designer
	    component: admin
	    host_api: v1
	    provides:
	      designer_steps: [reports.rules]

`)

		writeFile(t, filepath.Join(dir, "bootstrap.go"), "package reports\n")
		writeFile(t, filepath.Join(dir, "artifact.json"), `[]`)
		if _, err := GenerateGo(GoGenerateOptions{ManifestPath: path}); err == nil || !strings.Contains(err.Error(), "数组不能为空") {
			t.Fatalf("GenerateGo error = %v, want empty array error", err)
		}
	}

	func TestGenerateGoRejectsFrontendArtifactWithMissingEntryDigest(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, DefaultFileName)
		writeFile(t, path, `apiVersion: sparq.io/v1

kind: Plugin
metadata:

	id: reports
	code: reports
	name: Reports
	version: 0.1.0

sparq:

	protocol: ">=0.1.0 <0.2.0"

spec:

	runtime:
	  supported: [local]
	frontend_modules:
	  - id: designer
	    component: admin
	    host_api: v1
	    provides:
	      designer_steps: [reports.rules]

`)

		writeFile(t, filepath.Join(dir, "bootstrap.go"), "package reports\n")
		writeFile(t, filepath.Join(dir, "artifact.json"), `[{"module":"designer","version":"0.1.0","entry":"modules/designer/entry.mjs","files":{"modules/designer/other.mjs":"sha256-other"}}]`)
		if _, err := GenerateGo(GoGenerateOptions{ManifestPath: path}); err == nil || !strings.Contains(err.Error(), "未包含入口文件摘要") {
			t.Fatalf("GenerateGo error = %v, want missing entry digest error", err)
		}
	}

	func TestGenerateGoRequiresFrontendArtifactForReleaseBuild(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, DefaultFileName)
		writeFile(t, path, `apiVersion: sparq.io/v1

kind: Plugin
metadata:

	id: reports
	code: reports
	name: Reports
	version: 0.1.0

sparq:

	protocol: ">=0.1.0 <0.2.0"

spec:

	runtime:
	  supported: [local]
	frontend_modules:
	  - id: designer
	    component: admin
	    host_api: v1
	    provides:
	      designer_steps: [reports.rules]

`)

		writeFile(t, filepath.Join(dir, "bootstrap.go"), "package reports\n")
		_, err := GenerateGo(GoGenerateOptions{ManifestPath: path, RequireFrontendArtifacts: true})
		if err == nil || !strings.Contains(err.Error(), "未找到 frontend/artifact.json") {
			t.Fatalf("missing frontend artifact error = %v", err)
		}
	}

	func TestGenerateGoRejectsUnknownFrontendArtifactModule(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, DefaultFileName)
		writeFile(t, path, `apiVersion: sparq.io/v1

kind: Plugin
metadata:

	id: reports
	code: reports
	name: Reports
	version: 0.1.0

sparq:

	protocol: ">=0.1.0 <0.2.0"

spec:

	runtime:
	  supported: [local]

`)

		writeFile(t, filepath.Join(dir, "bootstrap.go"), "package reports\n")
		writeFile(t, filepath.Join(dir, "artifact.json"), `{"module":"designer","version":"0.1.0","entry":"modules/designer/entry.mjs","files":{"modules/designer/entry.mjs":"sha256-abc"}}`)
		if _, err := GenerateGo(GoGenerateOptions{ManifestPath: path}); err == nil || !strings.Contains(err.Error(), "未声明") {
			t.Fatalf("GenerateGo error = %v, want unknown module", err)
		}
	}
*/
func TestGenerateGoComposesOptionalGoManifestModule(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultFileName)
	writeFile(t, path, `apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: reports
  code: reports
  name: Reports
  version: 0.1.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
`)
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/reports\n")
	writeFile(t, filepath.Join(dir, "bootstrap.go"), "package reports\n")
	writeFile(t, filepath.Join(dir, ManifestModuleDirName, "manifest.go"), `package manifest

import "github.com/zane-xin/sparq-protocol/plugin"

func Compose(base plugin.Manifest) (plugin.Manifest, error) {
	return base, nil
}
`)

	result, err := GenerateGo(GoGenerateOptions{ManifestPath: path})
	if err != nil {
		t.Fatalf("GenerateGo failed: %v", err)
	}
	generated := string(result.Content)
	if !strings.Contains(generated, `pluginmanifest "example.com/reports/manifest"`) ||
		!strings.Contains(generated, "composed, err := pluginmanifest.Compose(value)") ||
		!strings.Contains(generated, `panic("compose generated plugin manifest: " + err.Error())`) ||
		!strings.Contains(generated, "return composed") {
		t.Fatalf("generated composed manifest = %s", generated)
	}
}

func TestGenerateGoComposesApplicationManifestModule(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, DefaultFileName)
	writeFile(t, manifestPath, `apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: reports
  code: reports
  name: Reports
  version: 0.1.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
`)
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/reports\n")
	writeFile(t, filepath.Join(dir, "bootstrap.go"), "package reports\n")
	writeFile(t, filepath.Join(dir, filepath.FromSlash(appManifestModuleDirName), "manifest.go"), `package manifest

import "github.com/zane-xin/sparq-protocol/plugin"

func Compose(base plugin.Manifest) (plugin.Manifest, error) { return base, nil }
`)

	result, err := GenerateGo(GoGenerateOptions{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("GenerateGo failed: %v", err)
	}
	if generated := string(result.Content); !strings.Contains(generated, `pluginmanifest "example.com/reports/app/manifest"`) {
		t.Fatalf("generated application manifest import = %s", generated)
	}
}

func TestLoadManifestModuleValidatesBaseMetadataWithoutRuntime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultFileName)
	writeFile(t, path, `apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: reports
  code: reports
  name: Reports
  version: 0.1.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
`)
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/reports\n")
	writeFile(t, filepath.Join(dir, ManifestModuleDirName, "manifest.go"), validManifestModuleSource())

	if _, err := Load(path); err != nil {
		t.Fatalf("Load base manifest with Go module failed: %v", err)
	}
}

func TestLoadManifestModuleRejectsYAMLSpecDeclarations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultFileName)
	writeFile(t, path, `apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: reports
  code: reports
  name: Reports
  version: 0.1.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec:
  runtime:
    supported: [local]
`)
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/reports\n")
	writeFile(t, filepath.Join(dir, ManifestModuleDirName, "manifest.go"), validManifestModuleSource())

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "只允许声明基础元数据") {
		t.Fatalf("Load mixed YAML and Go manifest error = %v", err)
	}
}

func TestLoadManifestModuleRejectsYAMLLifecycleDeclaration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultFileName)
	writeFile(t, path, `apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: reports
  code: reports
  name: Reports
  version: 0.1.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec:
  lifecycle:
    operations: [migrate]
`)
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/reports\n")
	writeFile(t, filepath.Join(dir, ManifestModuleDirName, "manifest.go"), validManifestModuleSource())

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "只允许声明基础元数据") {
		t.Fatalf("Load mixed YAML lifecycle and Go manifest error = %v", err)
	}
}

func TestLoadManifestModuleRejectsMissingComposeFunction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultFileName)
	writeFile(t, path, `apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: reports
  code: reports
  name: Reports
  version: 0.1.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
`)
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/reports\n")
	writeFile(t, filepath.Join(dir, ManifestModuleDirName, "manifest.go"), "package manifest\n")

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "缺少 Compose 函数") {
		t.Fatalf("Load manifest module without Compose error = %v", err)
	}
}

func TestLoadRejectsExternalSchemaResourceOutsidePluginDirectory(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, DefaultFileName)
	writeFile(t, manifestPath, validManifestWithSchemaFile("../outside.schema.json"))

	_, err := Load(manifestPath)
	if err == nil || !strings.Contains(err.Error(), "必须位于插件目录内") {
		t.Fatalf("Load error = %v, want outside plugin directory error", err)
	}
}

func TestLoadRejectsInvalidExternalSchemaResource(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, DefaultFileName)
	writeFile(t, manifestPath, validManifestWithSchemaFile("schema/reports.tag.schema.json"))
	writeFile(t, filepath.Join(dir, "schema", "reports.tag.schema.json"), `{
  "resource": "reports.tag",
  "schema": {"name": "tag", "title": "Tag"},
  "action_permissions": {"list": "reports.tag.list"},
  "unknown": true
}`)

	_, err := Load(manifestPath)
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("Load error = %v, want unknown field error", err)
	}
}

func TestLoadRejectsSchemaResourceFileWithInlineFields(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, DefaultFileName)
	writeFile(t, manifestPath, strings.Replace(
		validManifestWithSchemaFile("schema/reports.tag.schema.json"),
		"      - file: schema/reports.tag.schema.json",
		"      - file: schema/reports.tag.schema.json\n        resource: reports.tag",
		1,
	))

	_, err := Load(manifestPath)
	if err == nil || !strings.Contains(err.Error(), "不能与 resource") {
		t.Fatalf("Load error = %v, want mixed declaration error", err)
	}
}

func TestLoadRejectsInlineSchemaResource(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, DefaultFileName)
	writeFile(t, manifestPath, strings.Replace(
		validManifestWithSchemaFile("schema/reports.tag.schema.json"),
		"      - file: schema/reports.tag.schema.json",
		"      - resource: reports.tag\n        schema: {name: tag, title: Tag}\n        action_permissions: {list: reports.tag.list}",
		1,
	))

	_, err := Load(manifestPath)
	if err == nil || !strings.Contains(err.Error(), "必须通过插件内 JSON 文件声明") {
		t.Fatalf("Load error = %v, want external file requirement", err)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestValidateReportsRequiredFields(t *testing.T) {
	err := Validate(Manifest{})
	if err == nil {
		t.Fatal("expected validation error")
	}
	validationErr, ok := err.(ValidationError)
	if !ok {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if len(validationErr.Issues) == 0 {
		t.Fatal("expected validation issues")
	}
}

func TestValidateRejectsMismatchedCode(t *testing.T) {
	err := Validate(Manifest{
		APIVersion: APIVersion,
		Kind:       KindPlugin,
		Metadata: Metadata{
			ID:      "demo",
			Code:    "other",
			Name:    "Demo",
			Version: "0.1.0",
		},
		Sparq: Sparq{Protocol: ">=0.1.0 <0.2.0"},
		Spec:  Spec{Runtime: Runtime{Supported: []string{"local"}}},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestLoadNestedContributions(t *testing.T) {
	path := writeManifest(t, `
apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: reports
  code: reports
  name: Reports
  version: 1.0.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec:
  runtime:
    supported: [local]
  entries:
    - name: admin
      title: Reports
      type: page
      sandbox: iframe
      load_mode: local
    - name: summary
      title: Report Summary
      type: component
      sandbox: iframe
      load_mode: local
  capabilities:
    - code: reports.summary
      kind: dashboard-widget
      title: Report Summary
      entry: summary
      required_permissions:
        - admin.reports.view
  provides:
    capabilities:
      - reports.summary
      - site.route.resolve
      - site.template.data.resolve
  requires:
    capabilities:
      - storage.object.read
  site_directives:
    - name: cate
      provider: reports-site
      kind: data
      version: v1
  contributions:
    version: 3
    features:
      - code: admin.reports
        name: Reports
        module: admin
        resource: reports
        permissions:
          - action: view
            name: View reports
            usage_role: entry
            data_scope_policy: required
    schema_resources:
      - file: schema/reports.tag.schema.json
    menus:
      - code: admin.reports
        type: page
        title: Reports
        sort: 30
        visible: true
        enabled: true
        entry_code: admin
        entry_payload:
          view: summary
        feature_code: admin.reports
        entry_permission_action: view
`)
	writeFile(t, filepath.Join(filepath.Dir(path), "schema", "reports.tag.schema.json"), `{
  "resource": "reports.tag",
  "table_name": "reports_tags",
  "schema": {"name": "tag", "title": "Tag"},
  "action_permissions": {"list": "admin.reports.view"}
}`)
	item, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if item.Spec.Contributions.Version != 3 || item.Spec.Contributions.Features[0].Permissions[0].DataScopePolicy != permissionprotocol.DataScopeRequired {
		t.Fatalf("unexpected contributions: %#v", item.Spec.Contributions)
	}
	protocolManifest, err := item.ProtocolManifest()
	if err != nil {
		t.Fatalf("ProtocolManifest failed: %v", err)
	}
	if payload := string(protocolManifest.Contributions.Menus[0].EntryPayload); payload != `{"view":"summary"}` {
		t.Fatalf("entry payload = %q", payload)
	}
	if len(protocolManifest.Contributions.SchemaResources) != 1 ||
		protocolManifest.Contributions.SchemaResources[0].Resource != "reports.tag" ||
		string(protocolManifest.Contributions.SchemaResources[0].Schema) != `{"name":"tag","title":"Tag"}` {
		t.Fatalf("schema resources = %#v", protocolManifest.Contributions.SchemaResources)
	}
	if len(protocolManifest.Capabilities) != 1 || protocolManifest.Capabilities[0].Kind != "dashboard-widget" ||
		protocolManifest.Capabilities[0].Entry != "summary" || len(protocolManifest.Capabilities[0].RequiredPermissions) != 1 {
		t.Fatalf("frontend capabilities = %#v", protocolManifest.Capabilities)
	}
	if len(protocolManifest.Provides) != 3 || protocolManifest.Provides[0].Capability != "reports.summary" ||
		protocolManifest.Provides[1].Capability != "site.route.resolve" ||
		protocolManifest.Provides[2].Capability != "site.template.data.resolve" {
		t.Fatalf("provides = %#v", protocolManifest.Provides)
	}
	if len(protocolManifest.Requires) != 1 || protocolManifest.Requires[0].Capability != "storage.object.read" {
		t.Fatalf("requires = %#v", protocolManifest.Requires)
	}
	if len(protocolManifest.SiteDirectives) != 1 || protocolManifest.SiteDirectives[0].Name != "cate" ||
		protocolManifest.SiteDirectives[0].Provider != "reports-site" {
		t.Fatalf("site directives = %#v", protocolManifest.SiteDirectives)
	}
}

func TestLoadSettingsTemplateContribution(t *testing.T) {
	path := writeManifest(t, `
apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: auth-github
  code: auth-github
  name: GitHub 登录
  version: 1.0.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec:
  runtime:
    supported: [local]
  contributions:
    version: 1
    settings_templates:
      - code: auth.external.github
        title: GitHub 登录
        schema_version: 1
        activation_mode: immediate
        schema:
          format: sparq-schema/v1
          data:
            name: auth.external.github
            title: GitHub 登录
            fields: []
`)
	item, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	protocolManifest, err := item.ProtocolManifest()
	if err != nil {
		t.Fatalf("ProtocolManifest() error = %v", err)
	}
	if len(protocolManifest.Contributions.SettingsTemplates) != 1 ||
		protocolManifest.Contributions.SettingsTemplates[0].Code != "auth.external.github" {
		t.Fatalf("settings templates = %#v", protocolManifest.Contributions.SettingsTemplates)
	}
	if !json.Valid(protocolManifest.Contributions.SettingsTemplates[0].Schema.Data) {
		t.Fatalf("settings template schema = %s", protocolManifest.Contributions.SettingsTemplates[0].Schema.Data)
	}
}

func TestLoadRejectsUnknownNestedManifestField(t *testing.T) {
	path := writeManifest(t, `
apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: reports
  code: reports
  name: Reports
  version: 1.0.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec:
  runtime:
    supported: [local]
  contributions:
    version: 1
    permissionz: []
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "permissionz") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func writeManifest(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plugin.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	return path
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func validManifestModuleSource() string {
	return `package manifest

import "github.com/zane-xin/sparq-protocol/plugin"

func Compose(base plugin.Manifest) (plugin.Manifest, error) {
	return base, nil
}
`
}

func validManifestWithSchemaFile(schemaFile string) string {
	return `apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: reports
  code: reports
  name: Reports
  version: 1.0.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec:
  runtime:
    supported: [local]
  contributions:
    version: 1
    features:
      - code: reports.tag
        name: Report tags
        module: reports
        resource: tag
        permissions:
          - action: list
            name: View report tags
            usage_role: entry
            data_scope_policy: not_applicable
    schema_resources:
      - file: ` + schemaFile + "\n"
}
