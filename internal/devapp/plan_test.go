package devapp

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zane-xin/sparq-tool/internal/toolchain"
	"github.com/zane-xin/sparq-tool/internal/watch"
)

func TestLoadPlanResolvesAppAndWatchPaths(t *testing.T) {
	root := t.TempDir()
	frontendDir := filepath.Join(root, "frontend")
	backendDir := filepath.Join(root, "app")
	configDir := filepath.Join(backendDir, "config")
	for _, path := range []string{frontendDir, configDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	watchFile := filepath.Join(configDir, "sparq.watch.yaml")
	if err := os.WriteFile(watchFile, []byte(`
cwd: ..
run:
  command: go run ./cmd/app
watch:
  paths: [.]
health:
  url: http://127.0.0.1:19000
  path: /healthz
`), 0o600); err != nil {
		t.Fatal(err)
	}
	appFile := filepath.Join(root, "sparq.dev.yaml")
	if err := os.WriteFile(appFile, []byte(`
backend:
  watch_file: app/config/sparq.watch.yaml
frontend:
  cwd: frontend
  command: pnpm dev
  url: http://127.0.0.1:5176/platforms/example/
  timeout: 20s
  env:
    BROWSER: none
`), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := LoadPlan(appFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Validate(); err != nil {
		t.Fatal(err)
	}
	if plan.BackendWatchFile != watchFile || plan.Backend.CWD != backendDir || plan.FrontendCWD != frontendDir {
		t.Fatalf("unexpected resolved paths: %+v", plan)
	}
	if plan.FrontendCommand != "pnpm" || strings.Join(plan.FrontendArgs, " ") != "dev" || strings.Join(plan.FrontendEnv, ",") != "BROWSER=none" || plan.FrontendTimeout.String() != "20s" {
		t.Fatalf("unexpected frontend plan: %+v", plan)
	}
}

func TestParseConfigRejectsUnknownField(t *testing.T) {
	_, err := parseConfig([]byte("backend:\n  watch: sparq.watch.yaml\n"))
	if err == nil || !strings.Contains(err.Error(), "watch") {
		t.Fatalf("parseConfig() error = %v", err)
	}
}

func TestLoadPlanRequiresBackendWatchFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sparq.dev.yaml")
	if err := os.WriteFile(path, []byte("frontend:\n  command: pnpm dev\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadPlan(path)
	if err == nil || !strings.Contains(err.Error(), "backend.watch_file") {
		t.Fatalf("LoadPlan() error = %v", err)
	}
}

func TestNewPlanCombinesCorepackPrefixWithFrontendCommand(t *testing.T) {
	plan := NewPlan(Config{
		Frontend:  FrontendConfig{Command: "pnpm dev"},
		Toolchain: toolchain.Toolchain{Frontend: toolchain.Spec{Command: "corepack", Args: []string{"pnpm"}}},
	}, watch.Plan{})
	if plan.FrontendCommand != "corepack" || strings.Join(plan.FrontendArgs, " ") != "pnpm dev" {
		t.Fatalf("frontend command = %q %q", plan.FrontendCommand, plan.FrontendArgs)
	}
}

func TestNewPlanResolvesFrontendToolOverrides(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  string
		spec toolchain.Spec
		args []string
	}{
		{"default", "pnpm dev", toolchain.Spec{Command: "pnpm"}, []string{"dev"}},
		{"versioned", "pnpm dev", toolchain.Spec{Command: "pnpm10"}, []string{"dev"}},
		{"absolute", "pnpm dev", toolchain.Spec{Command: filepath.Join(t.TempDir(), "pnpm")}, []string{"dev"}},
		{"versioned basename", "pnpm10 dev", toolchain.Spec{Command: filepath.Join(t.TempDir(), "pnpm10")}, []string{"dev"}},
		{"wrapper", "pnpm dev", toolchain.Spec{Command: "corepack", Args: []string{"pnpm"}}, []string{"pnpm", "dev"}},
		{"explicit wrapper", "corepack pnpm dev", toolchain.Spec{Command: "corepack", Args: []string{"pnpm"}}, []string{"pnpm", "dev"}},
		{"arguments only", "dev --host 127.0.0.1", toolchain.Spec{Command: "pnpm10"}, []string{"dev", "--host", "127.0.0.1"}},
		{"fixed arguments", "pnpm dev", toolchain.Spec{Command: "pnpm10", Args: []string{"--offline"}}, []string{"--offline", "dev"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := NewPlan(Config{Frontend: FrontendConfig{Command: test.raw}, Toolchain: toolchain.Toolchain{Frontend: test.spec}}, watch.Plan{})
			if plan.FrontendCommand != test.spec.Command || !slices.Equal(plan.FrontendArgs, test.args) {
				t.Fatalf("frontend = %q %q, want %q %q", plan.FrontendCommand, plan.FrontendArgs, test.spec.Command, test.args)
			}
		})
	}
}

func TestLoadPlanRejectsBuildOnlyFrontendMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sparq.dev.yaml")
	if err := os.WriteFile(path, []byte("frontend:\n  mode: source-build\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadPlan(path)
	if err == nil || !strings.Contains(err.Error(), "仅适用于构建命令") {
		t.Fatalf("LoadPlan() error = %v", err)
	}
}

func TestNewPlanDoesNotPublishDevelopmentURLForArtifact(t *testing.T) {
	plan := NewPlan(Config{
		Frontend: FrontendConfig{Mode: "artifact", URL: "http://127.0.0.1:5176/root.ts"},
	}, watch.Plan{})
	if plan.FrontendEnabled || len(plan.Backend.RunEnv) != 0 {
		t.Fatalf("plan = %+v", plan)
	}
}
