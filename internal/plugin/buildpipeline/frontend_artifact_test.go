package buildpipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateFrontendEntryArtifactFromViteManifest(t *testing.T) {
	frontendRoot := t.TempDir()
	outputDir := filepath.Join(frontendRoot, "dist")
	writeFile(t, outputDir, "assets/index-abc.js", `import { createApp } from "vue"; export { createApp }`)
	writeFile(t, outputDir, "assets/index-abc.css", "body{}")
	writeFile(t, outputDir, "assets/hash-worker-def.js", "self.onmessage = () => {}")
	writeFile(t, outputDir, ".vite/manifest.json", `{
  "index.html": {
    "file": "assets/index-abc.js",
    "name": "index",
    "src": "index.html",
    "isEntry": true,
    "css": ["assets/index-abc.css"]
  }
}`)
	path, err := generateFrontendEntryArtifact(frontendRoot, outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(frontendRoot, entryArtifactFileName) {
		t.Fatalf("artifact path = %q", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var artifact frontendEntryArtifact
	if err := json.Unmarshal(body, &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.Entry != "assets/index-abc.js" || len(artifact.Styles) != 1 || len(artifact.Files) != 3 || !strings.HasPrefix(artifact.Files[artifact.Entry], "sha256-") {
		t.Fatalf("artifact = %#v", artifact)
	}
	if !strings.HasPrefix(artifact.Files["assets/hash-worker-def.js"], "sha256-") {
		t.Fatalf("worker digest missing: %#v", artifact.Files)
	}
}

func TestGenerateFrontendEntryArtifactRejectsMultipleEntries(t *testing.T) {
	frontendRoot := t.TempDir()
	outputDir := filepath.Join(frontendRoot, "dist")
	writeFile(t, outputDir, ".vite/manifest.json", `{
  "first.html": {"file":"assets/first.js","isEntry":true},
  "second.html": {"file":"assets/second.js","isEntry":true}
}`)
	_, err := generateFrontendEntryArtifact(frontendRoot, outputDir)
	if err == nil || !strings.Contains(err.Error(), "恰好包含一个入口") {
		t.Fatalf("error = %v", err)
	}
}

func TestGenerateFrontendEntryArtifactRejectsMissingManifestOutput(t *testing.T) {
	frontendRoot := t.TempDir()
	outputDir := filepath.Join(frontendRoot, "dist")
	writeFile(t, outputDir, "assets/existing.js", "export {}")
	writeFile(t, outputDir, ".vite/manifest.json", `{
  "index.html": {"file":"assets/missing.js","isEntry":true}
}`)
	_, err := generateFrontendEntryArtifact(frontendRoot, outputDir)
	if err == nil || !strings.Contains(err.Error(), "输出文件不存在") {
		t.Fatalf("error = %v", err)
	}
}
