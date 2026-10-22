package watchcmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWatchCommandInitDoesNotOverwriteExistingConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "sparq.watch.yaml")
	command := New()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"--file", configPath, "--init"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "policy: never") || !strings.Contains(string(content), "mode: notify") {
		t.Fatalf("generated config is incomplete:\n%s", content)
	}

	command = New()
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"--file", configPath, "--init"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "不会覆盖") {
		t.Fatalf("second init error = %v", err)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(content) {
		t.Fatal("second init changed existing config")
	}
}

func TestWatchCommandDryRun(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "sparq.watch.yaml")
	if err := os.WriteFile(configPath, []byte(`
run:
  command: go run ./cmd/app
watch:
  extensions: [go]
health:
  url: http://127.0.0.1:8080
  path: /healthz
`), 0o600); err != nil {
		t.Fatal(err)
	}
	command := New()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"--file", configPath, "--dry-run"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"运行命令: go run ./cmd/app", "构建命令: 未配置", "健康检查: http://127.0.0.1:8080/healthz"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("dry-run output missing %q:\n%s", expected, output.String())
		}
	}
}
