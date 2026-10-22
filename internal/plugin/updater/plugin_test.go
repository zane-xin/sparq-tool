package updater

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpdatePluginCleansTransactionalBackup(t *testing.T) {
	root := t.TempDir()
	pluginsDir := filepath.Join(root, "plugins")
	current := filepath.Join(pluginsDir, "demo")
	next := filepath.Join(root, "next")
	writeManifest(t, current, "0.1.0")
	writeManifest(t, next, "0.2.0")
	result, err := UpdatePlugin(PluginUpdateOptions{Source: next, PluginsDir: pluginsDir})
	if err != nil {
		t.Fatalf("UpdatePlugin failed: %v", err)
	}
	if result.BackupDir != "" {
		t.Fatalf("expected transactional backup to be cleaned, got %s", result.BackupDir)
	}
	if !exists(filepath.Join(result.TargetDir, "plugin.yaml")) {
		t.Fatalf("expected updated plugin: %s", result.TargetDir)
	}
}

func writeManifest(t *testing.T, dir string, version string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	body := `apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: demo
  code: demo
  name: Demo
  version: ` + version + `
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec:
  runtime:
    supported:
      - local
`
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}
