package buildpipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	protocolplugin "github.com/zane-xin/sparq-protocol/plugin"
	"github.com/zane-xin/sparq-protocol/protocol"
	"github.com/zane-xin/sparq-tool/internal/plugin/manifest"
)

func TestBuildRunsGoAndFrontendStages(t *testing.T) {
	root := createPlugin(t, true, true)
	writeFile(t, root, ConfigFileName, `version: 1
go:
  test_packages: ["./..."]
  test_tags: integration
  tags: production,custom
frontend:
  package_manager: pnpm
  script: build
`)
	var calls []string
	run := func(_ context.Context, dir, name string, args ...string) ([]byte, error) {
		calls = append(calls, filepath.Base(dir)+":"+name+" "+strings.Join(args, " "))
		if name == "pnpm" {
			outputDir := filepath.Join(root, "frontend", "dist")
			writeFile(t, outputDir, "frontend.mjs", "built\n")
			writeFrontendBuildDescriptor(t, outputDir, "frontend.mjs", nil)
		}
		return nil, nil
	}
	result, err := build(context.Background(), Options{Dir: root, Target: TargetAll}, run)
	if err != nil {
		t.Fatal(err)
	}
	if !result.GoTested || result.BinaryPath == "" || !result.FrontendBuilt || result.FrontendPackageManager != "pnpm" {
		t.Fatalf("result = %+v", result)
	}
	want := []string{"001:go test -tags integration ./...", "001:go build -buildvcs=false -trimpath -ldflags -s -w -tags production,custom -o", "frontend:pnpm run build"}
	joined := strings.Join(calls, "\n")
	for _, expected := range want {
		if !strings.Contains(joined, expected) {
			t.Fatalf("calls missing %q:\n%s", expected, joined)
		}
	}
}

func TestBuildSeparatesTestAndReleaseTags(t *testing.T) {
	for _, scenario := range []struct {
		name, configuration, tags string
	}{
		{name: "missing config", tags: "production"},
		{name: "default config", configuration: "version: 1\ngo:\n  optimization: debug\n", tags: "production"},
		{name: "development override", configuration: "version: 1\ngo:\n  tags: ''\n"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := createPlugin(t, true, false)
			if scenario.configuration != "" {
				writeFile(t, root, ConfigFileName, scenario.configuration)
			}
			var calls []string
			_, err := build(context.Background(), Options{Dir: root, Target: TargetGo}, func(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
				calls = append(calls, name+" "+strings.Join(args, " "))
				return nil, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(calls) != 2 || calls[0] != "go test ./..." {
				t.Fatalf("test command = %v", calls)
			}
			if scenario.tags == "" {
				if strings.Contains(calls[1], "-tags") {
					t.Fatalf("development build = %s", calls[1])
				}
			} else if !strings.Contains(calls[1], "-tags "+scenario.tags+" ") {
				t.Fatalf("release build = %s", calls[1])
			}
		})
	}
}

func TestBuildFrontendGeneratesViteEntryArtifact(t *testing.T) {
	root := createPlugin(t, false, true)
	writeFile(t, filepath.Join(root, "frontend"), "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	result, err := build(context.Background(), Options{Dir: root, Target: TargetFrontend}, func(_ context.Context, _ string, _ string, _ ...string) ([]byte, error) {
		output := filepath.Join(root, "frontend", "dist")
		writeFile(t, output, "frontend.mjs", "export {}")
		writeFile(t, output, "assets/index.js", `import { createApp } from "vue"`)
		writeFile(t, output, ".vite/manifest.json", `{"index.html":{"file":"assets/index.js","isEntry":true}}`)
		writeFrontendBuildDescriptor(t, output, "frontend.mjs", nil)
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.FrontendArtifactPath != filepath.Join(root, "frontend", entryArtifactFileName) {
		t.Fatalf("result = %#v", result)
	}
}

func TestRequireSharedFrontendEntryArtifactsAcceptsTrustedRoot(t *testing.T) {
	manifest := protocolplugin.Manifest{Frontend: &protocolplugin.FrontendManifest{
		HostAPI: protocolplugin.FrontendHostAPIV1, RuntimeProfile: protocolplugin.FrontendRuntimeProfileAdminV1,
	}}
	if err := requireSharedFrontendEntryArtifacts(manifest); err != nil {
		t.Fatal(err)
	}
}

func TestRequireSharedFrontendEntryArtifactsRejectsLocalIframeWithoutArtifact(t *testing.T) {
	manifest := protocolplugin.Manifest{
		Components: []protocolplugin.ComponentManifest{{ID: "admin", Kind: protocolplugin.ComponentKindFrontend, Frontend: &protocolplugin.FrontendComponent{}}},
		Entries: []protocolplugin.EntryManifest{{
			Component: "admin", Name: "admin", Type: protocolplugin.EntryTypePage,
			Sandbox: protocolplugin.EntrySandboxIframe, LoadMode: protocolplugin.EntryLoadModeLocal,
		}},
	}
	if err := requireSharedFrontendEntryArtifacts(manifest); err == nil || !strings.Contains(err.Error(), "frontend/entry-artifact.json") {
		t.Fatalf("error = %v, want missing local iframe artifact error", err)
	}
}

func TestBuildAllowsLocalPluginWithoutRemoteEntry(t *testing.T) {
	root := createPlugin(t, true, false)
	if err := os.RemoveAll(filepath.Join(root, "cmd")); err != nil {
		t.Fatal(err)
	}
	result, err := build(context.Background(), Options{Dir: root, Target: TargetAll}, func(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
		if name != "go" || strings.Join(args, " ") != "test ./..." {
			return nil, fmt.Errorf("unexpected command: %s %s", name, strings.Join(args, " "))
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.GoTested || result.BinaryPath != "" {
		t.Fatalf("result = %+v", result)
	}
}

func TestBuildFrontendTargetRequiresFrontend(t *testing.T) {
	root := createPlugin(t, true, false)
	_, err := build(context.Background(), Options{Dir: root, Target: TargetFrontend}, func(context.Context, string, string, ...string) ([]byte, error) {
		return nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "未启用前端构建阶段") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildRejectsEmptyFrontendOutput(t *testing.T) {
	root := createPlugin(t, false, true)
	writeFile(t, filepath.Join(root, "frontend"), "package-lock.json", "{}\n")
	_, err := build(context.Background(), Options{Dir: root, Target: TargetFrontend}, func(context.Context, string, string, ...string) ([]byte, error) {
		return nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "未生成输出目录") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildRejectsUnknownConfigField(t *testing.T) {
	root := createPlugin(t, true, false)
	writeFile(t, root, ConfigFileName, "version: 1\nunknown: true\n")
	_, err := build(context.Background(), Options{Dir: root}, func(context.Context, string, string, ...string) ([]byte, error) {
		return nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "field unknown not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildAllowsDebugOptimization(t *testing.T) {
	root := createPlugin(t, true, false)
	writeFile(t, root, ConfigFileName, "version: 1\ngo:\n  optimization: debug\n")
	var buildArguments string
	_, err := build(context.Background(), Options{Dir: root}, func(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
		if name == "go" && len(args) > 0 && args[0] == "build" {
			buildArguments = strings.Join(args, " ")
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if buildArguments == "" || strings.Contains(buildArguments, "-trimpath") || strings.Contains(buildArguments, "-ldflags") {
		t.Fatalf("build arguments = %q", buildArguments)
	}
}

func TestBuildRejectsUnknownOptimization(t *testing.T) {
	root := createPlugin(t, true, false)
	writeFile(t, root, ConfigFileName, "version: 1\ngo:\n  optimization: fast\n")
	_, err := build(context.Background(), Options{Dir: root}, func(context.Context, string, string, ...string) ([]byte, error) {
		return nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "go.optimization 必须是 size 或 debug") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildExportsValidatedManifest(t *testing.T) {
	root := createPlugin(t, true, false)
	built := protocolplugin.Manifest{
		ID: "demo", Code: "demo", Name: "Demo", Version: "1.0.0", Description: "composed",
		Protocol: protocol.Compatibility{Range: ">=0.1.0 <0.2.0"}, Runtime: []protocolplugin.RuntimeKind{protocolplugin.RuntimeLocal},
	}
	payload, err := json.Marshal(built)
	if err != nil {
		t.Fatal(err)
	}
	result, err := buildWithRunners(context.Background(), Options{Dir: root}, func(context.Context, string, string, ...string) ([]byte, error) {
		return nil, nil
	}, func(context.Context, string) ([]byte, error) {
		return payload, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(root, "release", "manifest.json")
	if result.ManifestPath != wantPath {
		t.Fatalf("manifest path = %q, want %q", result.ManifestPath, wantPath)
	}
	body, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"description": "composed"`) {
		t.Fatalf("manifest body = %s", body)
	}
}

func TestResolveManifestUsesExportedLifecycleDeclaration(t *testing.T) {
	root := createPlugin(t, true, false)
	item, err := manifest.Load(filepath.Join(root, manifest.DefaultFileName))
	if err != nil {
		t.Fatal(err)
	}
	built := protocolplugin.Manifest{
		ID: "demo", Code: "demo", Name: "Demo", Version: "1.0.0",
		Protocol: protocol.Compatibility{Range: ">=0.1.0 <0.2.0"}, Runtime: []protocolplugin.RuntimeKind{protocolplugin.RuntimeHTTP},
		Lifecycle: protocolplugin.LifecycleManifest{Operations: []protocolplugin.LifecyclePhase{protocolplugin.PhaseMigrate, protocolplugin.PhaseInstall}},
	}
	payload, err := json.Marshal(built)
	if err != nil {
		t.Fatal(err)
	}
	complete, err := resolveManifest(context.Background(), root, filepath.Join(root, manifest.DefaultFileName), item, func(context.Context, string) ([]byte, error) {
		return payload, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !complete.Lifecycle.Supports(protocolplugin.PhaseMigrate) || !complete.Lifecycle.Supports(protocolplugin.PhaseInstall) {
		t.Fatalf("resolved lifecycle = %#v", complete.Lifecycle)
	}
	if _, err := os.Stat(filepath.Join(root, "release", "manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("ResolveManifest wrote release output: %v", err)
	}
}

func TestResolveManifestAcceptsNonDefaultManifestPath(t *testing.T) {
	root := createPlugin(t, false, false)
	manifestPath := filepath.Join(root, "plugin.dev.yaml")
	if err := os.Rename(filepath.Join(root, manifest.DefaultFileName), manifestPath); err != nil {
		t.Fatal(err)
	}
	generated, err := manifest.GenerateGo(manifest.GoGenerateOptions{ManifestPath: manifestPath})
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.WriteGo(generated); err != nil {
		t.Fatal(err)
	}

	complete, err := ResolveManifest(context.Background(), manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if complete.Code != "demo" || complete.Version != "1.0.0" {
		t.Fatalf("resolved identity = %s@%s", complete.Code, complete.Version)
	}
}

func TestBuildRejectsExportedManifestIdentityMismatch(t *testing.T) {
	root := createPlugin(t, true, false)
	built := protocolplugin.Manifest{
		ID: "other", Code: "other", Name: "Other", Version: "1.0.0",
		Protocol: protocol.Compatibility{Range: ">=0.1.0 <0.2.0"}, Runtime: []protocolplugin.RuntimeKind{protocolplugin.RuntimeLocal},
	}
	payload, err := json.Marshal(built)
	if err != nil {
		t.Fatal(err)
	}
	_, err = buildWithRunners(context.Background(), Options{Dir: root}, func(context.Context, string, string, ...string) ([]byte, error) {
		return nil, nil
	}, func(context.Context, string) ([]byte, error) {
		return payload, nil
	})
	if err == nil || !strings.Contains(err.Error(), "完整 Manifest 身份不一致") {
		t.Fatalf("identity mismatch error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "release", "manifest.json")); !os.IsNotExist(statErr) {
		t.Fatalf("failed build retained release manifest: %v", statErr)
	}
}

func TestBuildFrontendTargetDoesNotExportManifest(t *testing.T) {
	root := createPlugin(t, false, true)
	writeFile(t, filepath.Join(root, "frontend"), "package-lock.json", "{}\n")
	exported := false
	result, err := buildWithRunners(context.Background(), Options{Dir: root, Target: TargetFrontend}, func(_ context.Context, _ string, name string, _ ...string) ([]byte, error) {
		if name == "npm" {
			outputDir := filepath.Join(root, "frontend", "dist")
			writeFile(t, outputDir, "frontend.mjs", "built\n")
			writeFrontendBuildDescriptor(t, outputDir, "frontend.mjs", nil)
		}
		return nil, nil
	}, func(context.Context, string) ([]byte, error) {
		exported = true
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if exported || result.ManifestPath != "" {
		t.Fatalf("frontend result = %+v, exported = %v", result, exported)
	}
	if _, statErr := os.Stat(filepath.Join(root, "release", "manifest.json")); !os.IsNotExist(statErr) {
		t.Fatalf("frontend build wrote release manifest: %v", statErr)
	}
}

func TestLimitedBufferRecordsOutputLimit(t *testing.T) {
	buffer := &limitedBuffer{limit: 4}
	written, err := buffer.Write([]byte("abcdef"))
	if err != nil || written != 6 || !buffer.exceeded || buffer.String() != "abcd" {
		t.Fatalf("limited buffer = %q, written = %d, exceeded = %v, error = %v", buffer.String(), written, buffer.exceeded, err)
	}
}

func createPlugin(t *testing.T, withGo, withFrontend bool) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "plugin.yaml", `apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: demo
  code: demo
  name: Demo
  version: 1.0.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec:
  runtime:
    supported: [local]
`)
	writeFile(t, root, "bootstrap.go", "package demo\n")
	if withGo {
		writeFile(t, root, "go.mod", "module example.com/demo\n\ngo 1.25\n")
		writeFile(t, root, "cmd/demo/main.go", "package main\nfunc main() {}\n")
	}
	if withFrontend {
		writeFile(t, filepath.Join(root, "frontend"), "package.json", `{"version":"1.0.0","scripts":{"build":"vite build"}}`)
	}
	generated, err := manifest.GenerateGo(manifest.GoGenerateOptions{ManifestPath: filepath.Join(root, "plugin.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.WriteGo(generated); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeFile(t *testing.T, root, relative, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
