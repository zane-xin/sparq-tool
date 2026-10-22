package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallPluginFromDirectory(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	pluginsDir := filepath.Join(root, "plugins")
	writeFile(t, filepath.Join(source, "plugin.yaml"), `apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: demo
  code: demo
  name: Demo
  version: 0.1.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec:
  runtime:
    supported:
      - local
`)
	result, err := InstallPlugin(PluginInstallOptions{Source: source, PluginsDir: pluginsDir})
	if err != nil {
		t.Fatalf("InstallPlugin failed: %v", err)
	}
	if result.Code != "demo" {
		t.Fatalf("code = %s, want demo", result.Code)
	}
	if _, err := os.Stat(filepath.Join(pluginsDir, "demo", "plugin.yaml")); err != nil {
		t.Fatalf("installed manifest missing: %v", err)
	}
}

func writeFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}
