package devcmd

import (
	"strings"
	"testing"

	"github.com/zane-xin/sparq-protocol/plugin"
	"github.com/zane-xin/sparq-tool/internal/dev"
)

func TestNewExposesPluginAppAndLifecycleModes(t *testing.T) {
	command := New()
	if command.Name() != "dev" {
		t.Fatalf("command name = %q", command.Name())
	}
	seen := map[string]bool{}
	for _, child := range command.Commands() {
		seen[child.Name()] = true
	}
	if !seen["plugin"] || !seen["app"] || !seen["lifecycle"] {
		t.Fatalf("dev subcommands = %#v", seen)
	}
	lifecycle, _, err := command.Find([]string{"lifecycle", "test"})
	if err != nil || lifecycle.Name() != "test" {
		t.Fatalf("find dev lifecycle test = %v, %v", lifecycle, err)
	}
}

func TestPrintLifecycleTestReport(t *testing.T) {
	var builder strings.Builder
	printLifecycleTestReport(&builder, dev.LifecycleTestReport{
		Plugin: "cms", Mode: "phase", Phase: "migrate", Status: "succeeded",
		ManifestDigest: "sha256:manifest", Operations: []plugin.LifecyclePhase{plugin.PhaseMigrate, plugin.PhaseInstall},
		OperationID: "operation-1",
	})
	output := builder.String()
	for _, expected := range []string{"cms", "诊断阶段: migrate", "migrate, install", "sha256:manifest", "operation-1"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("report output missing %q: %s", expected, output)
		}
	}
}

func TestPrintPlan(t *testing.T) {
	var builder strings.Builder
	printPlan(&builder, dev.Plan{
		PluginID:           "cms",
		PluginComponent:    "content",
		PluginCommand:      "go run ./cmd/cms",
		FrontendEnabled:    true,
		FrontendCommand:    "pnpm dev:designer",
		FrontendURL:        "http://127.0.0.1:5176/src/frontend.ts",
		FrontendHealthPath: "http://127.0.0.1:5176/healthz",
	})
	output := builder.String()
	for _, expected := range []string{
		"插件组件: content",
		"插件后端命令",
		"插件前端根地址: http://127.0.0.1:5176/src/frontend.ts",
		"插件前端根就绪检查: http://127.0.0.1:5176/healthz",
		"开发态 token -> 前端覆盖登记 -> 插件前端",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("plan output missing %q: %s", expected, output)
		}
	}
	if strings.Contains(output, "Surface 默认地址") {
		t.Fatalf("unexpected plan output: %s", builder.String())
	}
}
