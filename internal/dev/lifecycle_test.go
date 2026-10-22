package dev

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zane-xin/sparq-protocol/plugin"
)

func TestSourceDigestTracksIncludedFilesAndIgnoresExcludedFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "plugin.go"), []byte("package plugin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "storage"), 0o755); err != nil {
		t.Fatal(err)
	}
	ignored := filepath.Join(root, "storage", "runtime.json")
	if err := os.WriteFile(ignored, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan := Plan{
		BaseDir: root, PluginCWD: root, ReloadEnabled: true, ReloadPaths: []string{root},
		ReloadExtensions: []string{".go", ".json"}, ReloadFiles: []string{"go.mod"}, ReloadExclude: []string{"storage"},
	}
	first, err := SourceDigest(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ignored, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := SourceDigest(plan)
	if err != nil || second != first {
		t.Fatalf("excluded digest = %q, want %q, error = %v", second, first, err)
	}
	if err := os.WriteFile(filepath.Join(root, "plugin.go"), []byte("package plugin\n// changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	third, err := SourceDigest(plan)
	if err != nil || third == first {
		t.Fatalf("changed digest = %q, first = %q, error = %v", third, first, err)
	}
}

func TestLifecycleSessionHeartbeatInterval(t *testing.T) {
	now := time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)
	interval, err := lifecycleSessionHeartbeatInterval(LifecycleSession{ExpiresAt: now.Add(20 * time.Second).Format(time.RFC3339)}, now)
	if err != nil || interval != 10*time.Second {
		t.Fatalf("heartbeat interval = %s, error = %v", interval, err)
	}
	if _, err := lifecycleSessionHeartbeatInterval(LifecycleSession{ExpiresAt: now.Add(-time.Second).Format(time.RFC3339)}, now); err == nil {
		t.Fatal("heartbeat interval accepted an expired session")
	}
}

func TestRunLifecycleTestRequiresDevelopmentSecret(t *testing.T) {
	_, err := RunLifecycleTest(context.Background(), Plan{}, LifecycleTestOptions{}, Streams{})
	if err == nil || err.Error() != "dev lifecycle test 要求 app.dev_secret 至少 32 字节" {
		t.Fatalf("RunLifecycleTest() error = %v", err)
	}
}

func TestRunLifecycleTestRejectsUnsupportedAndUnconfirmedDestructivePhases(t *testing.T) {
	plan := Plan{AppDevSecret: "0123456789abcdef0123456789abcdef"}
	if _, err := RunLifecycleTest(context.Background(), plan, LifecycleTestOptions{Phase: plugin.PhaseStart}, Streams{}); err == nil || !strings.Contains(err.Error(), "不支持") {
		t.Fatalf("unsupported phase error = %v", err)
	}
	if _, err := RunLifecycleTest(context.Background(), plan, LifecycleTestOptions{Phase: plugin.PhaseRollback}, Streams{}); err == nil || !strings.Contains(err.Error(), "--allow-destructive") {
		t.Fatalf("destructive phase error = %v", err)
	}
}

func TestResolvePluginComponent(t *testing.T) {
	tests := []struct {
		name       string
		manifest   plugin.Manifest
		configured string
		want       string
		wantError  string
	}{
		{
			name:     "legacy default",
			manifest: plugin.Manifest{Runtime: []plugin.RuntimeKind{plugin.RuntimeHTTP}},
			want:     string(plugin.DefaultComponentID),
		},
		{
			name: "unique remote backend",
			manifest: plugin.Manifest{Components: []plugin.ComponentManifest{
				{ID: "backend", Kind: plugin.ComponentKindBackend, Runtime: []plugin.RuntimeKind{plugin.RuntimeLocal, plugin.RuntimeHTTP}},
				{ID: "frontend", Kind: plugin.ComponentKindFrontend},
			}},
			want: "backend",
		},
		{
			name: "explicit worker",
			manifest: plugin.Manifest{Components: []plugin.ComponentManifest{
				{ID: "backend", Kind: plugin.ComponentKindBackend, Runtime: []plugin.RuntimeKind{plugin.RuntimeHTTP}},
				{ID: "jobs", Kind: plugin.ComponentKindWorker, Runtime: []plugin.RuntimeKind{plugin.RuntimeHTTP}},
			}},
			configured: "jobs",
			want:       "jobs",
		},
		{
			name: "ambiguous remote components",
			manifest: plugin.Manifest{Components: []plugin.ComponentManifest{
				{ID: "backend", Kind: plugin.ComponentKindBackend, Runtime: []plugin.RuntimeKind{plugin.RuntimeHTTP}},
				{ID: "jobs", Kind: plugin.ComponentKindWorker, Runtime: []plugin.RuntimeKind{plugin.RuntimeHTTP}},
			}},
			wantError: "请设置 plugin.component",
		},
		{
			name: "unknown explicit component",
			manifest: plugin.Manifest{Components: []plugin.ComponentManifest{
				{ID: "backend", Kind: plugin.ComponentKindBackend, Runtime: []plugin.RuntimeKind{plugin.RuntimeHTTP}},
			}},
			configured: "missing",
			wantError:  "未在完整 Manifest 中声明",
		},
		{
			name: "frontend is not a remote backend",
			manifest: plugin.Manifest{Components: []plugin.ComponentManifest{
				{ID: "frontend", Kind: plugin.ComponentKindFrontend},
			}},
			configured: "frontend",
			wantError:  "不是支持 http runtime",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			component, err := resolvePluginComponent(test.manifest, test.configured)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("resolvePluginComponent() error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(component) != test.want {
				t.Fatalf("resolvePluginComponent() = %q, want %q", component, test.want)
			}
		})
	}
}

func TestLifecycleSessionRequestEncodesExplicitEmptyOperations(t *testing.T) {
	payload, err := json.Marshal(LifecycleSessionRequest{Operations: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"operations":[]`) {
		t.Fatalf("lifecycle session request = %s", payload)
	}
}
