package lifecycle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemovePluginMovesDirectory(t *testing.T) {
	root := t.TempDir()
	pluginsDir := filepath.Join(root, "plugins")
	pluginDir := filepath.Join(pluginsDir, "demo")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	result, err := Remove(RemoveOptions{Name: "demo", PluginsDir: pluginsDir})
	if err != nil {
		t.Fatalf("removePlugin failed: %v", err)
	}
	if exists(pluginDir) {
		t.Fatal("expected plugin directory moved")
	}
	if !exists(result.BackupDir) {
		t.Fatalf("expected backup directory: %s", result.BackupDir)
	}
}
