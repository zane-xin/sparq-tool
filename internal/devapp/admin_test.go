package devapp

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zane-xin/sparq-tool/internal/watch"
)

func TestAdminPlanResolvesIndependentSourceAndProxy(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "app")
	for _, dir := range []string{appDir, filepath.Join(root, "shell"), filepath.Join(appDir, "frontend")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(appDir, "sparq.watch.yaml"), []byte(`
run:
  command: go run ./cmd/app
health:
  url: http://127.0.0.1:19090
  path: /__healthz__
`), 0o600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(appDir, "sparq.dev.yaml")
	if err := os.WriteFile(file, []byte(`
backend:
  watch_file: sparq.watch.yaml
admin:
  cwd: ../shell
  command: pnpm dev
  url: http://127.0.0.1:5188
frontend:
  cwd: frontend
  command: pnpm dev
  url: http://127.0.0.1:5176/src/frontend.ts
toolchain:
  frontend:
    command: corepack
    args: [pnpm]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := LoadPlan(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Validate(); err != nil {
		t.Fatal(err)
	}
	if plan.Admin.CWD != filepath.Join(root, "shell") || plan.Admin.Command != "corepack" || !slices.Equal(plan.Admin.Args, []string{"pnpm", "dev"}) {
		t.Fatalf("Admin source/toolchain = %+v", plan.Admin)
	}
	if !slices.Contains(plan.Admin.Env, "VITE_SPARQ_API_PROXY_TARGET=http://127.0.0.1:19090") {
		t.Fatalf("Admin proxy = %v", plan.Admin.Env)
	}
	if !slices.Contains(plan.Backend.RunEnv, "SPARQ_DEV_FRONTEND_URL=http://127.0.0.1:5176/src/frontend.ts") || strings.Contains(strings.Join(plan.Backend.RunEnv, "\n"), "5188") {
		t.Fatalf("business entry = %v", plan.Backend.RunEnv)
	}
	if len(plan.Frontends()) != 2 {
		t.Fatalf("frontends = %+v", plan.Frontends())
	}
}

func TestAdminProxyOverridesAndDisabledBusinessFrontend(t *testing.T) {
	for _, test := range []struct {
		name, target, want string
		env                map[string]string
	}{
		{name: "backend origin", want: "http://127.0.0.1:19090"},
		{name: "explicit target", target: "http://localhost:19091", want: "http://localhost:19091"},
		{name: "env override", target: "http://localhost:19091", env: map[string]string{"VITE_SPARQ_API_PROXY_TARGET": "http://localhost:19092"}, want: "http://localhost:19092"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			plan := NewPlan(Config{
				Backend:  BackendConfig{WatchFile: "watch.yaml"},
				Admin:    &AdminConfig{FrontendConfig: FrontendConfig{CWD: root, Command: "pnpm dev", URL: "http://127.0.0.1:5188", Env: test.env}, APIProxyTarget: test.target},
				Frontend: FrontendConfig{Mode: "disabled"},
			}, watch.NewPlan(watch.Config{CWD: root, Run: watch.CommandConfig{Command: "go run ."}, Health: watch.HealthConfig{URL: "http://127.0.0.1:19090"}}))
			if err := plan.Validate(); err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(plan.Admin.Env, "VITE_SPARQ_API_PROXY_TARGET="+test.want) || len(plan.Backend.RunEnv) != 0 || len(plan.Frontends()) != 1 {
				t.Fatalf("Admin-only plan = %+v", plan)
			}
		})
	}
}

func TestAdminPlanRejectsInvalidDevelopmentConfiguration(t *testing.T) {
	for _, test := range []struct {
		name, want string
		change     func(*AdminConfig)
	}{
		{"mode", "admin.mode", func(a *AdminConfig) { a.Mode = "source-build" }},
		{"command", "admin.command", func(a *AdminConfig) { a.Command = "" }},
		{"url", "admin.url", func(a *AdminConfig) { a.URL = "" }},
		{"timeout", "admin.timeout", func(a *AdminConfig) { a.Timeout = "bad" }},
		{"stop", "admin.stop.grace_timeout", func(a *AdminConfig) { a.Stop.GraceTimeout = "bad" }},
		{"cwd", "工作目录", func(a *AdminConfig) { a.CWD = filepath.Join(t.TempDir(), "missing") }},
		{"proxy", "admin.api_proxy_target", func(a *AdminConfig) { a.APIProxyTarget = "ftp://localhost" }},
		{"port collision", "端口冲突", func(a *AdminConfig) { a.URL = "http://127.0.0.1:19090" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			admin := &AdminConfig{FrontendConfig: FrontendConfig{CWD: root, Command: "pnpm dev", URL: "http://127.0.0.1:5188"}}
			test.change(admin)
			plan := NewPlan(Config{Backend: BackendConfig{WatchFile: "watch.yaml"}, Admin: admin, Frontend: FrontendConfig{Mode: "disabled"}}, watch.NewPlan(watch.Config{
				CWD: root, Run: watch.CommandConfig{Command: "go run ."}, Health: watch.HealthConfig{URL: "http://127.0.0.1:19090"},
			}))
			if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() = %v, want %q", err, test.want)
			}
		})
	}
	for _, mode := range []string{"artifact", "disabled"} {
		plan := NewPlan(Config{Admin: &AdminConfig{FrontendConfig: FrontendConfig{Mode: mode}}, Frontend: FrontendConfig{Mode: "disabled"}}, watch.Plan{})
		if len(plan.Frontends()) != 0 {
			t.Fatalf("disabled frontends = %+v", plan.Frontends())
		}
	}
}

func TestAdminAndBusinessFrontendCannotSharePort(t *testing.T) {
	root := t.TempDir()
	frontend := FrontendConfig{CWD: root, Command: "pnpm dev", URL: "http://127.0.0.1:5188"}
	plan := NewPlan(Config{
		Backend: BackendConfig{WatchFile: "watch.yaml"},
		Admin:   &AdminConfig{FrontendConfig: frontend}, Frontend: frontend,
	}, watch.NewPlan(watch.Config{
		CWD: root, Run: watch.CommandConfig{Command: "go run ."},
		Health: watch.HealthConfig{URL: "http://127.0.0.1:19090"},
	}))
	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "应用前端 与 Admin Shell") {
		t.Fatalf("frontend port collision error = %v", err)
	}
}
