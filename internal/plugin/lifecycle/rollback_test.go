package lifecycle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRollbackPluginRestoresLatestBackup(t *testing.T) {
	root := t.TempDir()
	pluginsDir := filepath.Join(root, "plugins")
	backup := filepath.Join(pluginsDir, ".removed", "demo-20260101000000")
	if err := os.MkdirAll(backup, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	result, err := Rollback(RollbackOptions{Name: "demo", PluginsDir: pluginsDir})
	if err != nil {
		t.Fatalf("rollbackPlugin failed: %v", err)
	}
	if result.TargetDir != filepath.Join(pluginsDir, "demo") {
		t.Fatalf("target = %s", result.TargetDir)
	}
	if !exists(result.TargetDir) {
		t.Fatal("expected restored plugin directory")
	}
}
