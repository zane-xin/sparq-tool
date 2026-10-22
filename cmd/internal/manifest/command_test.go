package manifestcmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestValidateCommandUsesDefaultManifest(t *testing.T) {
	dir := t.TempDir()
	writeCommandManifest(t, filepath.Join(dir, "plugin.yaml"), validCommandManifest)
	t.Chdir(dir)

	var output bytes.Buffer
	command := newValidateCommand()
	command.SetOut(&output)
	if err := command.Execute(); err != nil {
		t.Fatalf("manifest validate failed: %v", err)
	}
	if result := output.String(); !strings.Contains(result, "plugin.yaml (reports 0.1.0)") {
		t.Fatalf("output = %q", result)
	}
}

func TestValidateCommandAcceptsExplicitManifestPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.yaml")
	writeCommandManifest(t, path, validCommandManifest)

	for _, args := range [][]string{{path}, {"--file", path}} {
		command := newValidateCommand()
		command.SetArgs(args)
		if err := command.Execute(); err != nil {
			t.Fatalf("manifest validate %v failed: %v", args, err)
		}
	}
}

func TestManifestCommandsRejectConflictingPaths(t *testing.T) {
	commands := map[string]*cobra.Command{
		"validate": newValidateCommand(),
		"generate": newGenerateCommand(),
	}
	for name, command := range commands {
		t.Run(name, func(t *testing.T) {
			command.SetArgs([]string{"position.yaml", "--file", "flag.yaml"})
			err := command.Execute()
			if err == nil || !strings.Contains(err.Error(), "不能同时通过位置参数和 --file") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestValidateCommandReportsManifestPathOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.yaml")
	writeCommandManifest(t, path, "apiVersion: [\n")

	command := newValidateCommand()
	command.SetArgs([]string{path})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "校验插件清单 "+path+" 失败") ||
		!strings.Contains(err.Error(), "解析 YAML 插件清单失败") {
		t.Fatalf("error = %v", err)
	}
}

func writeCommandManifest(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const validCommandManifest = `apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: reports
  code: reports
  name: Reports
  version: 0.1.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec:
  runtime:
    supported: [local, http]
`
