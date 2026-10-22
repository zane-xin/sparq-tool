package localcmd

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestLocalCommandSurface(t *testing.T) {
	command := New()
	if command.Use != "local" {
		t.Fatalf("command use = %q, want local", command.Use)
	}
	got := make([]string, 0, len(command.Commands()))
	for _, child := range command.Commands() {
		got = append(got, child.Name())
	}
	sort.Strings(got)
	want := []string{"check", "list", "sync"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("local commands = %v, want %v", got, want)
	}
}

func TestLocalListCommandReadsGoWorkPlugins(t *testing.T) {
	root := t.TempDir()
	writeLocalCommandFile(t, filepath.Join(root, "go.work"), "use ./plugins/cms\n")
	writeLocalCommandFile(t, filepath.Join(root, "plugins", "cms", "plugin.yaml"), `apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: cms
  code: cms
  name: CMS
  version: 0.1.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec:
  runtime:
    supported: [local]
`)
	writeLocalCommandFile(t, filepath.Join(root, "plugins", "cms", "go.mod"), "module example.com/cms\n")

	var output bytes.Buffer
	command := newListCommand()
	command.SetOut(&output)
	command.SetArgs([]string{root})
	if err := command.Execute(); err != nil {
		t.Fatalf("local list command failed: %v", err)
	}
	if !strings.Contains(output.String(), "cms\t0.1.0\texample.com/cms\tplugins/cms") {
		t.Fatalf("command output = %s", output.String())
	}
}

func writeLocalCommandFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}
