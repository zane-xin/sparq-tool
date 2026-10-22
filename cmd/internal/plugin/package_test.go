package plugincmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	protocolplugin "github.com/zane-xin/sparq-protocol/plugin"
	"github.com/zane-xin/sparq-protocol/protocol"
)

func TestPackageCommandCreatesPluginPackage(t *testing.T) {
	root := t.TempDir()
	writePackageTestFile(t, filepath.Join(root, "plugin.yaml"), packageTestManifest)
	writePackageTestFile(t, filepath.Join(root, "release", "bin", "demo"), "bin")
	var output bytes.Buffer
	command := newPackageCommand()
	command.SetOut(&output)
	command.SetArgs([]string{"--output", "dist", root})
	if err := command.Execute(); err != nil {
		t.Fatalf("plugin package command failed: %v", err)
	}
	archivePath := filepath.Join(root, "dist", "demo-0.1.0.tar.gz")
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("archive missing: %v", err)
	}
	if !strings.Contains(output.String(), "插件包已生成") || !strings.Contains(output.String(), "包内校验清单: checksums.txt") {
		t.Fatalf("command output = %s", output.String())
	}
}

func TestPackageCommandUsesValidatedBuiltManifest(t *testing.T) {
	root := t.TempDir()
	writePackageTestFile(t, filepath.Join(root, "plugin.yaml"), packageTestManifest)
	built := protocolplugin.Manifest{
		ID: "demo", Code: "demo", Name: "Demo", Version: "0.1.0",
		Protocol: protocol.Compatibility{Range: ">=0.1.0 <0.2.0"}, Runtime: []protocolplugin.RuntimeKind{protocolplugin.RuntimeLocal},
	}
	payload, err := json.Marshal(built)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "built-manifest.json")
	writePackageTestFile(t, manifestPath, string(payload))
	command := newPackageCommand()
	command.SetArgs([]string{"--manifest-json", manifestPath, root})
	if err := command.Execute(); err != nil {
		t.Fatalf("plugin package with built manifest: %v", err)
	}
	archived := packageArchiveFile(t, filepath.Join(root, "release", "demo-0.1.0.tar.gz"), "manifest.json")
	if !bytes.Contains(archived, []byte(`"code": "demo"`)) {
		t.Fatalf("archived manifest = %s", archived)
	}
	built.Version = "0.2.0"
	payload, err = json.Marshal(built)
	if err != nil {
		t.Fatal(err)
	}
	writePackageTestFile(t, manifestPath, string(payload))
	command = newPackageCommand()
	command.SetArgs([]string{"--manifest-json", manifestPath, root})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "完整插件协议清单身份不一致") {
		t.Fatalf("plugin package mismatched manifest error = %v", err)
	}
}

func TestPackageCommandAutomaticallyUsesReleaseManifest(t *testing.T) {
	root := t.TempDir()
	writePackageTestFile(t, filepath.Join(root, "plugin.yaml"), packageTestManifest)
	built := protocolplugin.Manifest{
		ID: "demo", Code: "demo", Name: "Demo", Version: "0.1.0",
		Protocol: protocol.Compatibility{Range: ">=0.1.0 <0.2.0"}, Runtime: []protocolplugin.RuntimeKind{protocolplugin.RuntimeLocal},
		Description: "generated",
	}
	payload, err := json.Marshal(built)
	if err != nil {
		t.Fatal(err)
	}
	writePackageTestFile(t, filepath.Join(root, "release", "manifest.json"), string(payload))
	command := newPackageCommand()
	command.SetArgs([]string{root})
	if err := command.Execute(); err != nil {
		t.Fatalf("plugin package with release manifest: %v", err)
	}
	archived := packageArchiveFile(t, filepath.Join(root, "release", "demo-0.1.0.tar.gz"), "manifest.json")
	if !bytes.Contains(archived, []byte(`"description": "generated"`)) {
		t.Fatalf("archived manifest did not use release/manifest.json: %s", archived)
	}
}

func TestPackageCommandRejectsComposeWithoutReleaseManifest(t *testing.T) {
	root := t.TempDir()
	writePackageTestFile(t, filepath.Join(root, "plugin.yaml"), `apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: demo
  code: demo
  name: Demo
  version: 0.1.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec: {}
`)
	writePackageTestFile(t, filepath.Join(root, "go.mod"), "module example.com/demo\n\ngo 1.25\n")
	writePackageTestFile(t, filepath.Join(root, "manifest", "manifest.go"), `package manifest

import "github.com/zane-xin/sparq-protocol/plugin"

func Compose(base plugin.Manifest) (plugin.Manifest, error) { return base, nil }
`)
	command := newPackageCommand()
	command.SetArgs([]string{root})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "使用 manifest.Compose") || !strings.Contains(err.Error(), "plugin build") {
		t.Fatalf("compose package error = %v", err)
	}
}

func packageArchiveFile(t *testing.T, archivePath, target string) []byte {
	t.Helper()
	file, err := os.Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Name == target {
			body, readErr := io.ReadAll(reader)
			if readErr != nil {
				t.Fatal(readErr)
			}
			return body
		}
	}
	t.Fatalf("plugin package missing %s", target)
	return nil
}

func writePackageTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const packageTestManifest = `apiVersion: sparq.io/v1
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
    supported: [local]
`
