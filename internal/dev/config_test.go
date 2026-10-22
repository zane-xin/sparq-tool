package dev

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zane-xin/sparq-protocol/plugin"
	"github.com/zane-xin/sparq-protocol/remoteauth"
	"github.com/zane-xin/sparq-tool/internal/watch"
)

func TestParseConfig(t *testing.T) {
	config := ParseConfig([]byte(`
app:
  command: go run ./sparq-app/cmd/sparq
  cwd: ..
  endpoint: http://127.0.0.1:8080
  dev_secret: 0123456789abcdef0123456789abcdef
  health: /__healthz__
  timeout: 20s
plugin:
  id: cms
  component: backend
  manifest: plugin.yaml
  endpoint: http://127.0.0.1:19091
  command: go run ./cmd/cms
  cwd: .
  health: /healthz
  secret: 0123456789abcdef0123456789abcdef
  env:
    SPARQ_DATABASE_DSN: ./storage/sparq.sqlite
  reload:
    enabled: true
    paths: [., ../../sparq-kernel]
    extensions: [go]
    files: [go.mod]
    exclude: [frontend, storage]
    debounce: 250ms
    build: go build ./...
frontend:
  enabled: true
  command: pnpm dev
  cwd: frontend
  url: http://127.0.0.1:5173
  health: /plugins/cms/admin/
  env:
    BROWSER: none
`))
	if config.App.Endpoint != "http://127.0.0.1:8080" || config.App.Timeout != "20s" || config.App.DevSecret == "" {
		t.Fatalf("unexpected app config: %#v", config)
	}
	if config.Plugin.Command != "go run ./cmd/cms" || config.Plugin.Secret == "" || config.Plugin.Component != "backend" {
		t.Fatalf("unexpected plugin config: %#v", config)
	}
	if config.Plugin.Env["SPARQ_DATABASE_DSN"] != "./storage/sparq.sqlite" {
		t.Fatalf("unexpected plugin database environment: %#v", config.Plugin.Env)
	}
	if !config.Plugin.Reload.Enabled || config.Plugin.Reload.Debounce != "250ms" || len(config.Plugin.Reload.Paths) != 2 {
		t.Fatalf("unexpected plugin reload config: %#v", config.Plugin.Reload)
	}
	if !config.Frontend.Enabled || config.Frontend.Health != "/plugins/cms/admin/" || config.Frontend.Env["BROWSER"] != "none" {
		t.Fatalf("unexpected frontend config: %#v", config)
	}
}

func TestParseConfigRejectsRemovedFrontendEntries(t *testing.T) {
	_, err := parseConfig([]byte("frontend:\n  entries:\n    admin: http://127.0.0.1:5173/admin/\n"))
	if err == nil || !strings.Contains(err.Error(), "entries") {
		t.Fatalf("parseConfig() error = %v", err)
	}
}

func TestNewPlanAppliesReloadDefaults(t *testing.T) {
	baseDir := t.TempDir()
	pluginDir := filepath.Join(baseDir, "plugin")
	if err := os.Mkdir(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	plan := NewPlan(Config{
		baseDir: baseDir,
		Plugin: PluginConfig{
			CWD:    "plugin",
			Reload: ReloadConfig{Enabled: true},
		},
	}, Options{})
	if len(plan.ReloadPaths) != 1 || plan.ReloadPaths[0] != pluginDir {
		t.Fatalf("reload paths = %#v", plan.ReloadPaths)
	}
	if strings.Join(plan.ReloadExtensions, ",") != ".go" || strings.Join(plan.ReloadFiles, ",") != "go.mod,go.sum" {
		t.Fatalf("reload includes = extensions:%#v files:%#v", plan.ReloadExtensions, plan.ReloadFiles)
	}
	if plan.ReloadBuild != "go build ./..." || plan.ReloadDebounce.Duration() != 300*time.Millisecond {
		t.Fatalf("reload defaults = build:%q debounce:%s", plan.ReloadBuild, plan.ReloadDebounce.Duration())
	}
}

func TestPlanRejectsMissingReloadPath(t *testing.T) {
	manifestPath := writeTestManifest(t, "cms")
	plan := Plan{
		PluginID:         "cms",
		AppEndpoint:      "http://127.0.0.1:19090",
		PluginEndpoint:   "http://127.0.0.1:19091",
		PluginCommand:    "go run ./cmd/cms",
		PluginSecret:     "0123456789abcdef0123456789abcdef",
		ManifestPath:     manifestPath,
		AppHealthPath:    "/__healthz__",
		PluginHealthPath: "/healthz",
		AppTimeout:       newDuration("", time.Second),
		PluginTimeout:    newDuration("", time.Second),
		FrontendTimeout:  newDuration("", time.Second),
		ReloadEnabled:    true,
		ReloadPaths:      []string{filepath.Join(t.TempDir(), "missing")},
		ReloadExtensions: []string{".go"},
		ReloadBuild:      "go build ./...",
		ReloadDebounce:   newDuration("", 300*time.Millisecond),
	}
	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "热更新监听目录") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestParseConfigRejectsUnknownFields(t *testing.T) {
	_, err := parseConfig([]byte("plugin:\n  id: cms\n  unknown: value\n"))
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("parseConfig() error = %v", err)
	}
}

func TestNewPlanResolvesPathsAndDefaults(t *testing.T) {
	baseDir := t.TempDir()
	plan := NewPlan(Config{
		baseDir: baseDir,
		App:     AppConfig{Endpoint: "http://127.0.0.1:19090"},
		Plugin: PluginConfig{
			ID: "cms", Endpoint: "http://127.0.0.1:19091", Command: "go run ./cmd/cms",
			CWD: "plugin", Component: "backend", Manifest: "plugin/plugin.yaml", Secret: "0123456789abcdef0123456789abcdef",
		},
	}, Options{})
	if plan.PluginCWD != filepath.Join(baseDir, "plugin") || plan.ManifestPath != filepath.Join(baseDir, "plugin", "plugin.yaml") {
		t.Fatalf("unexpected resolved paths: %+v", plan)
	}
	if plan.AppHealthPath != "/__healthz__" || plan.PluginHealthPath != "/healthz" {
		t.Fatalf("unexpected health defaults: %+v", plan)
	}
	if plan.PluginComponent != "backend" {
		t.Fatalf("plugin component = %q", plan.PluginComponent)
	}
}

func TestPlanRequiresPluginSecret(t *testing.T) {
	plan := Plan{
		PluginID: "cms", AppEndpoint: "http://127.0.0.1:19090",
		PluginEndpoint: "http://127.0.0.1:19091", PluginCommand: "go run ./cmd/cms",
	}
	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "plugin.secret") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestPlanRejectsInvalidTimeoutAndRequiresManifest(t *testing.T) {
	plan := Plan{
		PluginID:         "cms",
		AppEndpoint:      "http://127.0.0.1:19090",
		PluginEndpoint:   "http://127.0.0.1:19091",
		PluginCommand:    "go run ./cmd/cms",
		PluginSecret:     "0123456789abcdef0123456789abcdef",
		ManifestPath:     filepath.Join(t.TempDir(), "missing.yaml"),
		AppHealthPath:    "/__healthz__",
		PluginHealthPath: "/healthz",
		AppTimeout:       newDuration("not-a-duration", time.Second),
		PluginTimeout:    newDuration("", time.Second),
		FrontendTimeout:  newDuration("", time.Second),
	}
	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "app.timeout") {
		t.Fatalf("Validate() error = %v", err)
	}

	plan.AppTimeout = newDuration("", time.Second)
	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "插件清单不存在") {
		t.Fatalf("Validate() error = %v", err)
	}
}
func TestPluginAndFrontendEnvironment(t *testing.T) {
	plan := Plan{
		AppEndpoint: "http://127.0.0.1:19090", PluginID: "remote-echo",
		PluginEndpoint: "http://127.0.0.1:19091", PluginComponent: "backend", PluginSecret: "secret",
		LifecycleSessionID: "session-1", LifecycleSourceDigest: "sha256:source",
		PluginEnv:   map[string]string{"CUSTOM_PLUGIN_ENV": "value"},
		FrontendEnv: map[string]string{"CUSTOM_FRONTEND_ENV": "value"},
	}
	pluginEnv := strings.Join(pluginEnvironment(plan), "\n")
	for _, expected := range []string{
		"SPARQ_REMOTE_ECHO_APP_URL=http://127.0.0.1:19090",
		"SPARQ_REMOTE_ECHO_BASE_URL=http://127.0.0.1:19091",
		"SPARQ_REMOTE_ECHO_ADDR=127.0.0.1:19091",
		"SPARQ_REMOTE_ECHO_COMPONENT_ID=backend",
		"SPARQ_DEV_LIFECYCLE_SESSION_ID=session-1",
		"SPARQ_DEV_LIFECYCLE_SOURCE_DIGEST=sha256:source",
		"CUSTOM_PLUGIN_ENV=value",
	} {
		if !strings.Contains(pluginEnv, expected) {
			t.Fatalf("plugin environment missing %q:\n%s", expected, pluginEnv)
		}
	}
	frontendEnv := strings.Join(frontendEnvironment(plan, "dev-token"), "\n")
	if !strings.Contains(frontendEnv, "VITE_SPARQ_DEV_TOKEN=dev-token") ||
		!strings.Contains(frontendEnv, "VITE_SPARQ_PLUGIN_ID=remote-echo") {
		t.Fatalf("unexpected frontend environment:\n%s", frontendEnv)
	}
}

func TestWaitHealthStopsWhenProcessExits(t *testing.T) {
	process, err := watch.StartProcess(watch.Command{Name: "插件后端", Command: "exit 0"})
	if err != nil {
		t.Fatal(err)
	}
	<-process.Done()
	exits := make(chan processExit, 1)
	exits <- processExit{process: process, err: process.Err()}
	err = waitHealthWithExits(context.Background(), "http://127.0.0.1:1", "", time.Minute, exits, nil)
	if err == nil || !strings.Contains(err.Error(), "插件后端 已退出") {
		t.Fatalf("waitHealthWithExits() error = %v", err)
	}
}

func TestEnsureManagedEndpointsAvailableRejectsOccupiedAppEndpoint(t *testing.T) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	plan := Plan{
		AppCommand:     "go run ./cmd/sparq",
		AppEndpoint:    fmt.Sprintf("http://127.0.0.1:%d", port),
		PluginEndpoint: "http://127.0.0.1:1",
	}
	err = ensureManagedEndpointsAvailable(plan)
	if err == nil || !strings.Contains(err.Error(), "宿主地址不可用") {
		t.Fatalf("ensureManagedEndpointsAvailable() error = %v", err)
	}
}

func TestWaitHealthRetriesUntilReady(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		if requests < 2 {
			response.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := waitHealth(context.Background(), server.URL, "", time.Second, nil); err != nil {
		t.Fatalf("waitHealth() error = %v", err)
	}
	if requests < 2 {
		t.Fatalf("health requests = %d", requests)
	}
}

func TestWaitHealthUsesConfiguredPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/plugins/cms/admin/" || request.Header.Get("Accept") != "*/*" {
			response.WriteHeader(http.StatusNotFound)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := waitHealth(context.Background(), server.URL, "/plugins/cms/admin/", time.Second, nil); err != nil {
		t.Fatalf("waitHealth() configured path error = %v", err)
	}
}

func TestWaitHealthSignsPrivatePluginProbe(t *testing.T) {
	verifier := remoteauth.NewVerifier(remoteauth.StaticCredentials{"cms": "0123456789abcdef0123456789abcdef"})
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if _, err := verifier.Verify(request, nil); err != nil {
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	signer, err := remoteauth.NewSigner(plugin.Identity{PluginID: "cms"}, "0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if err := waitHealth(context.Background(), server.URL, "", time.Second, signer); err != nil {
		t.Fatalf("waitHealth() signed probe error = %v", err)
	}
}

func TestDevTokenClientRequestsAndRevokesToken(t *testing.T) {
	revoked := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/dev/auth/token":
			if request.Header.Get("Authorization") != "Bearer 0123456789abcdef0123456789abcdef" {
				t.Fatalf("token authorization = %q", request.Header.Get("Authorization"))
			}
			var input devTokenRequest
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil || input.Plugin != "cms" || len(input.Permissions) != 1 || input.Permissions[0] != "dev.frontend" {
				t.Fatalf("token request = %+v, error = %v", input, err)
			}
			_ = json.NewEncoder(response).Encode(devTokenResponse{
				Token: "dev-token", ExpiresAt: "2026-07-26T18:00:00Z",
			})
		case "/dev/auth/revoke":
			revoked = true
			response.WriteHeader(http.StatusNoContent)
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	token, err := requestDevToken(context.Background(), server.URL, "0123456789abcdef0123456789abcdef", "cms")
	if err != nil || token.Token != "dev-token" {
		t.Fatalf("requestDevToken() = %+v, error = %v", token, err)
	}
	if err := revokeDevToken(context.Background(), server.URL, token.Token); err != nil {
		t.Fatalf("revokeDevToken() error = %v", err)
	}
	if !revoked {
		t.Fatal("expected token revoke request")
	}
}

func TestRegisterDevFrontend(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/dev/frontend" || request.Method != http.MethodPost {
			response.WriteHeader(http.StatusNotFound)
			return
		}
		var input devFrontendRequest
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if input.Token != "dev-token" || input.Plugin != "cms" || input.RootURL == "" {
			t.Fatalf("frontend request = %+v", input)
		}
		called = true
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	if err := registerDevFrontend(context.Background(), server.URL, "dev-token", "cms", server.URL+"/admin/"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("frontend registration request was not received")
	}
}

func TestDevFrontendHeartbeatIntervalUsesHalfOfRemainingLifetime(t *testing.T) {
	now := time.Date(2026, 8, 4, 8, 0, 0, 0, time.UTC)
	interval, err := devFrontendHeartbeatInterval(devTokenResponse{
		Token: "dev-token", ExpiresAt: now.Add(time.Hour).Format(time.RFC3339),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if interval != 30*time.Minute {
		t.Fatalf("heartbeat interval = %s", interval)
	}
}

func TestKeepDevFrontendRegisteredReportsHeartbeatFailure(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		if requests == 1 {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		response.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errors := keepDevFrontendRegistered(
		ctx,
		server.URL,
		"dev-token",
		"cms",
		server.URL+"/admin/",
		10*time.Millisecond,
	)
	select {
	case err := <-errors:
		if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
			t.Fatalf("heartbeat error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("development frontend heartbeat did not run")
	}
}

func TestPlanRejectsNonHTTPFrontendRoot(t *testing.T) {
	manifestPath := writeComposedTestManifest(t, "cms")
	frontendDir := filepath.Join(filepath.Dir(manifestPath), "frontend")
	writeDevTestFile(t, filepath.Join(frontendDir, "src", "frontend.ts"), "export default () => ({})")
	plan := Plan{
		FrontendEnabled: true,
		FrontendCommand: "pnpm dev",
		FrontendCWD:     frontendDir,
		FrontendURL:     "ftp://127.0.0.1/src/frontend.ts",
	}
	plan.PluginID = "cms"
	plan.AppEndpoint = "http://127.0.0.1:19090"
	plan.PluginEndpoint = "http://127.0.0.1:19091"
	plan.PluginCommand = "go run ./cmd/cms"
	plan.PluginSecret = "0123456789abcdef0123456789abcdef"
	plan.AppDevSecret = "0123456789abcdef0123456789abcdef"
	plan.ManifestPath = manifestPath
	plan.AppHealthPath = "/__healthz__"
	plan.PluginHealthPath = "/healthz"
	plan.AppTimeout = newDuration("", time.Second)
	plan.PluginTimeout = newDuration("", time.Second)
	plan.FrontendTimeout = newDuration("", time.Second)
	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "frontend.url") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestPlanAcceptsExplicitFrontendRootFromComposedManifest(t *testing.T) {
	manifestPath := writeComposedTestManifest(t, "cms")
	frontendDir := filepath.Join(filepath.Dir(manifestPath), "frontend")
	writeDevTestFile(t, filepath.Join(frontendDir, "src", "frontend.ts"), "export default () => ({})")
	plan := Plan{
		PluginID:         "cms",
		AppEndpoint:      "http://127.0.0.1:19090",
		PluginEndpoint:   "http://127.0.0.1:19091",
		PluginCommand:    "go run ./cmd/cms",
		PluginSecret:     "0123456789abcdef0123456789abcdef",
		AppDevSecret:     "0123456789abcdef0123456789abcdef",
		ManifestPath:     manifestPath,
		AppHealthPath:    "/__healthz__",
		PluginHealthPath: "/healthz",
		AppTimeout:       newDuration("", time.Second),
		PluginTimeout:    newDuration("", time.Second),
		FrontendTimeout:  newDuration("", time.Second),
		FrontendEnabled:  true,
		FrontendCommand:  "pnpm dev:designer",
		FrontendCWD:      frontendDir,
		FrontendURL:      "http://127.0.0.1:5176/src/frontend.ts",
	}
	if err := plan.Validate(); err != nil {
		t.Fatalf("Validate() composed frontend module error = %v", err)
	}

	plan.FrontendURL = ""
	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "没有可登记") {
		t.Fatalf("Validate() empty composed frontend config error = %v", err)
	}
}

func TestFrontendRootUsesRootReadinessTarget(t *testing.T) {
	plan := Plan{FrontendURL: "http://127.0.0.1:5176/src/frontend.ts", FrontendHealthPath: "/healthz"}
	targets := plan.frontendReadinessTargets()
	if len(targets) != 1 || targets[0].Name != "插件前端" || targets[0].URL != "http://127.0.0.1:5176/src/frontend.ts" || targets[0].HealthPath != "/healthz" {
		t.Fatalf("frontend readiness targets = %#v", targets)
	}
}

func TestFrontendRootURLDerivesSourceEntryFromDevServer(t *testing.T) {
	plan := Plan{FrontendURL: "http://127.0.0.1:5178/plugins/cms"}
	if got := plan.frontendRootURL(); got != "http://127.0.0.1:5178/plugins/cms/src/frontend.ts" {
		t.Fatalf("frontend root URL = %q", got)
	}
}

func TestPlanDiscoversConventionalFrontendRoot(t *testing.T) {
	root := t.TempDir()
	writeDevTestFile(t, filepath.Join(root, "frontend", "src", "frontend.ts"), "export default () => ({})")
	config := Config{
		Plugin:  PluginConfig{ID: "cms"},
		baseDir: root,
	}
	plan := NewPlan(config, Options{})
	if !plan.FrontendEnabled || plan.FrontendCommand != "pnpm dev" || plan.FrontendCWD != filepath.Join(root, "frontend") {
		t.Fatalf("unexpected frontend discovery plan = %#v", plan)
	}
	if plan.FrontendURL != "http://127.0.0.1:5175/plugins/cms" || plan.frontendRootURL() != "http://127.0.0.1:5175/plugins/cms/src/frontend.ts" {
		t.Fatalf("unexpected frontend root URL = %q", plan.frontendRootURL())
	}
}

func TestPlanDoesNotEnableFrontendForLegacyAdminEntry(t *testing.T) {
	root := t.TempDir()
	writeDevTestFile(t, filepath.Join(root, "frontend", "src", "admin.ts"), "export default () => ({})")
	plan := NewPlan(Config{Plugin: PluginConfig{ID: "cms"}, baseDir: root}, Options{})
	if plan.FrontendEnabled {
		t.Fatalf("legacy admin entry enabled frontend: %#v", plan)
	}
}

func writeDevTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRegisterDevFrontendRejectsUnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	if err := registerDevFrontend(context.Background(), server.URL, "dev-token", "cms", server.URL+"/admin/"); err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("registerDevFrontend() error = %v", err)
	}
}

func writeTestManifest(t *testing.T, pluginID string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plugin.yaml")
	content := fmt.Sprintf(`apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: %s
  code: %s
  name: Test Plugin
  version: 1.0.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec:
  runtime:
    supported: [http]
`, pluginID, pluginID)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeComposedTestManifest(t *testing.T, pluginID string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.yaml")
	content := fmt.Sprintf(`apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: %s
  code: %s
  name: Test Plugin
  version: 1.0.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
`, pluginID, pluginID)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/"+pluginID+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifestDir := filepath.Join(dir, "manifest")
	if err := os.MkdirAll(manifestDir, 0o700); err != nil {
		t.Fatal(err)
	}
	compose := `package manifest

import "github.com/zane-xin/sparq-protocol/plugin"

func Compose(base plugin.Manifest) (plugin.Manifest, error) { return base, nil }
`
	if err := os.WriteFile(filepath.Join(manifestDir, "manifest.go"), []byte(compose), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
