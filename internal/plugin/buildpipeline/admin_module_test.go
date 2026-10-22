package buildpipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateFrontendArtifact(t *testing.T) {
	frontendRoot := t.TempDir()
	outputDir := filepath.Join(frontendRoot, "dist")
	writeFile(t, frontendRoot, "package.json", `{"version":"1.2.3"}`)
	writeFile(t, outputDir, "frontend.mjs", "export default () => {}")
	writeFile(t, outputDir, "frontend.css", ":host { display: block }")
	writeFile(t, outputDir, "assets/chunk.js", "export const chunk = true")
	writeFile(t, outputDir, ".vite/manifest.json", "{}")
	writeFrontendBuildDescriptor(t, outputDir, "frontend.mjs", []string{"frontend.css"})

	artifactPath, err := generateFrontendArtifact(frontendRoot, outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if artifactPath != filepath.Join(frontendRoot, frontendArtifactName) {
		t.Fatalf("artifact path = %q", artifactPath)
	}
	body, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	var artifact generatedFrontendArtifact
	if err := json.Unmarshal(body, &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.Entry != "frontend.mjs" || !strings.HasPrefix(artifact.Version, "1.2.3-") ||
		len(artifact.Styles) != 1 || artifact.Styles[0] != "frontend.css" ||
		!strings.HasPrefix(artifact.Files[artifact.Entry], "sha256-") ||
		!strings.HasPrefix(artifact.Files["assets/chunk.js"], "sha256-") || artifact.Files[".vite/manifest.json"] != "" {
		t.Fatalf("artifact = %#v", artifact)
	}
}

func TestGenerateFrontendArtifactRequiresBuildDescriptor(t *testing.T) {
	frontendRoot := t.TempDir()
	outputDir := filepath.Join(frontendRoot, "dist")
	writeFile(t, frontendRoot, "package.json", `{"version":"1.2.3"}`)
	writeFile(t, outputDir, "frontend.mjs", "export {}")
	if _, err := generateFrontendArtifact(frontendRoot, outputDir); err == nil || !strings.Contains(err.Error(), frontendBuildDescriptorName) {
		t.Fatalf("error = %v", err)
	}
}

func TestGenerateFrontendArtifactValidatesBuildDescriptor(t *testing.T) {
	tests := []struct {
		name       string
		descriptor frontendBuildDescriptor
		files      map[string]string
		wantError  string
	}{
		{
			name:       "schema 不匹配",
			descriptor: frontendBuildDescriptor{Schema: "sparq.frontend-build/v2", Entry: "frontend.mjs"},
			files:      map[string]string{"frontend.mjs": "export {}"},
			wantError:  "schema 必须为",
		},
		{
			name:       "入口越界",
			descriptor: frontendBuildDescriptor{Schema: frontendBuildDescriptorSchema, Entry: "../frontend.mjs"},
			files:      map[string]string{"frontend.mjs": "export {}"},
			wantError:  "安全的相对路径",
		},
		{
			name:       "入口不存在",
			descriptor: frontendBuildDescriptor{Schema: frontendBuildDescriptorSchema, Entry: "missing.mjs"},
			files:      map[string]string{"frontend.mjs": "export {}"},
			wantError:  "entry 对应文件不存在",
		},
		{
			name: "样式重复",
			descriptor: frontendBuildDescriptor{
				Schema: frontendBuildDescriptorSchema, Entry: "frontend.mjs", Styles: []string{"root.css", "root.css"},
			},
			files:     map[string]string{"frontend.mjs": "export {}", "root.css": ":host{}"},
			wantError: "styles 路径重复",
		},
		{
			name: "样式不存在",
			descriptor: frontendBuildDescriptor{
				Schema: frontendBuildDescriptorSchema, Entry: "frontend.mjs", Styles: []string{"missing.css"},
			},
			files:     map[string]string{"frontend.mjs": "export {}"},
			wantError: "style 对应文件不存在",
		},
		{
			name:       "样式扩展名错误",
			descriptor: frontendBuildDescriptor{Schema: frontendBuildDescriptorSchema, Entry: "frontend.mjs", Styles: []string{"root.scss"}},
			files:      map[string]string{"frontend.mjs": "export {}", "root.scss": ":host{}"},
			wantError:  "必须指向 .css 文件",
		},
		{
			name:       "遗漏输出样式",
			descriptor: frontendBuildDescriptor{Schema: frontendBuildDescriptorSchema, Entry: "frontend.mjs"},
			files:      map[string]string{"frontend.mjs": "export {}", "root.css": ":host{}"},
			wantError:  "遗漏 CSS 文件: root.css",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			frontendRoot := t.TempDir()
			outputDir := filepath.Join(frontendRoot, "dist")
			writeFile(t, frontendRoot, "package.json", `{"version":"1.2.3"}`)
			for relative, content := range test.files {
				writeFile(t, outputDir, relative, content)
			}
			body, err := json.Marshal(test.descriptor)
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, outputDir, frontendBuildDescriptorName, string(body))

			_, err = generateFrontendArtifact(frontendRoot, outputDir)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func writeFrontendBuildDescriptor(t *testing.T, outputDir, entry string, styles []string) {
	t.Helper()
	body, err := json.Marshal(frontendBuildDescriptor{
		Schema: frontendBuildDescriptorSchema,
		Entry:  entry,
		Styles: styles,
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, outputDir, frontendBuildDescriptorName, string(body))
}
