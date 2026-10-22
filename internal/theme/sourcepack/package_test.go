package sourcepack

import (
	"archive/zip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestCreateBuildsInstallableThemeArchive(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "template.yaml", `apiVersion: sparq.io/v1
kind: Template
metadata:
  id: studio
  code: studio
  version: 1.2.3
sparq:
  protocol: ">=0.1.0 <0.2.0"
`)
	writeFile(t, root, "theme.yaml", validThemeManifest("studio", "1.2.3", "home"))
	writeFile(t, root, "views/home.html", "home")
	writeFile(t, root, "assets/app.css", "body {}")
	writeFile(t, root, "README.md", "development notes")
	writeFile(t, root, "src/theme.scss", "$color: red")
	writeFile(t, root, "package.json", `{}`)
	writeFile(t, root, ".gitignore", "dist")
	result, err := Create(Options{SourceDir: root, OutputDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != "studio" || result.Version != "1.2.3" || len(result.SHA256) != 64 || result.FileCount != 4 {
		t.Fatalf("result = %#v", result)
	}
	reader, err := zip.OpenReader(result.ArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var names []string
	for _, item := range reader.File {
		if !item.FileInfo().IsDir() {
			names = append(names, item.Name)
		}
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "assets/app.css,template.yaml,theme.yaml,views/home.html" {
		t.Fatalf("archive entries = %v", names)
	}
}

func TestCreateIncludesThemeLicenses(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "template.yaml", `apiVersion: sparq.io/v1
kind: Template
metadata:
  id: studio
  code: studio
  version: 1.2.3
sparq:
  protocol: ">=0.1.0 <0.2.0"
`)
	writeFile(t, root, "theme.yaml", validThemeManifest("studio", "1.2.3", "home"))
	writeFile(t, root, "views/home.html", "home")
	writeFile(t, root, "assets/app.css", "body {}")
	writeFile(t, root, "licenses/OFL-1.1.txt", "font license")
	writeFile(t, root, "licenses/icons.md", "icon license")
	result, err := Create(Options{SourceDir: root, OutputDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if result.FileCount != 6 {
		t.Fatalf("theme package file count = %d, want 6", result.FileCount)
	}
	reader, err := zip.OpenReader(result.ArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var names []string
	for _, item := range reader.File {
		if !item.FileInfo().IsDir() {
			names = append(names, item.Name)
		}
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "assets/app.css,licenses/OFL-1.1.txt,licenses/icons.md,template.yaml,theme.yaml,views/home.html" {
		t.Fatalf("archive entries = %v", names)
	}
}

func TestCreateExplainsMisplacedLicenseFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "template.yaml", `apiVersion: sparq.io/v1
kind: Template
metadata:
  id: studio
  code: studio
  version: 1.2.3
sparq:
  protocol: ">=0.1.0 <0.2.0"
`)
	writeFile(t, root, "theme.yaml", validThemeManifest("studio", "1.2.3", "home"))
	writeFile(t, root, "views/home.html", "home")
	writeFile(t, root, "assets/OFL.txt", "font license")
	_, err := Create(Options{SourceDir: root, OutputDir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "许可证文本请放入 licenses/ 目录") {
		t.Fatalf("Create() error = %v", err)
	}
}

func TestCreateRejectsMismatchedManifests(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "template.yaml", `apiVersion: sparq.io/v1
kind: Template
metadata:
  id: studio
  code: studio
  version: 1.2.3
sparq:
  protocol: ">=0.1.0 <0.2.0"
`)
	writeFile(t, root, "theme.yaml", validThemeManifest("editorial", "1.2.3", "home"))
	if err := os.MkdirAll(filepath.Join(root, "views"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(Options{SourceDir: root, OutputDir: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "不一致") {
		t.Fatalf("Create() error = %v", err)
	}
}

func TestCreateIncludesCoverAndMultipleViews(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "template.yaml", `apiVersion: sparq.io/v1
kind: Template
metadata:
  id: studio
  code: studio
  version: 0.1.2
sparq:
  protocol: ">=0.1.0 <0.2.0"
`)
	writeFile(t, root, "theme.yaml", validThemeManifest("studio", "0.1.2", "home"))
	writeFile(t, root, "cover.png", "\x89PNG\r\n\x1a\n")
	writeFile(t, root, "assets/css/site.css", "body {}")
	for _, view := range []string{"channel_default", "detail_default", "list_default", "page_default"} {
		writeFile(t, root, "views/"+view+".html", "<main>"+view+"</main>")
	}
	result, err := Create(Options{
		SourceDir: root,
		OutputDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != "studio" || result.Version != "0.1.2" || result.FileCount != 8 || result.SizeBytes == 0 {
		t.Fatalf("Studio 主题打包结果 = %#v", result)
	}
	reader, err := zip.OpenReader(result.ArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var names []string
	for _, item := range reader.File {
		if !item.FileInfo().IsDir() {
			names = append(names, item.Name)
		}
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "assets/css/site.css,cover.png,template.yaml,theme.yaml,views/channel_default.html,views/detail_default.html,views/list_default.html,views/page_default.html" {
		t.Fatalf("archive entries = %v", names)
	}
}

func TestCreateRejectsInvalidThemeCapabilities(t *testing.T) {
	type replacement struct {
		old string
		new string
	}
	cases := map[string]replacement{
		"missing form factor":  {old: "formFactors: [desktop, tablet, mobile]\n"},
		"unknown form factor":  {old: "formFactors: [desktop, tablet, mobile]", new: "formFactors: [desktop, watch]"},
		"missing color scheme": {old: "  colorSchemes: [light]\n"},
		"missing primary":      {old: "    primary: \"#2563EB\"\n"},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			manifest := strings.Replace(validThemeManifest("studio", "1.2.3", "home"), change.old, change.new, 1)
			root := t.TempDir()
			writeFile(t, root, "template.yaml", `apiVersion: sparq.io/v1
kind: Template
metadata:
  id: studio
  code: studio
  version: 1.2.3
sparq:
  protocol: ">=0.1.0 <0.2.0"
`)
			writeFile(t, root, "theme.yaml", manifest)
			writeFile(t, root, "views/home.html", "home")
			writeFile(t, root, "assets/app.css", "body {}")
			if _, err := Create(Options{SourceDir: root, OutputDir: t.TempDir()}); err == nil {
				t.Fatal("Create() error = nil")
			}
		})
	}
}

func TestUpdateVersionSynchronizesThemeManifests(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "template.yaml", `# 上架清单
apiVersion: sparq.io/v1
kind: Template
metadata:
  id: studio
  code: studio
  version: 1.2.3 # 发布版本
sparq:
  protocol: ">=0.1.0 <0.2.0"
`)
	writeFile(t, root, "theme.yaml", "# 运行清单\n"+validThemeManifest("studio", "1.2.3 # 运行版本", "home"))
	writeFile(t, root, "views/home.html", "home")
	writeFile(t, root, "assets/app.css", "body {}")
	if err := UpdateVersion(root, "1.3.0-beta.1+build.7"); err != nil {
		t.Fatal(err)
	}
	version, err := ReadVersion(root)
	if err != nil || version != "1.3.0-beta.1+build.7" {
		t.Fatalf("ReadVersion() = %q, %v", version, err)
	}
	for _, file := range []string{"template.yaml", "theme.yaml"} {
		body, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), "version: 1.3.0-beta.1+build.7") || !strings.Contains(string(body), "# ") {
			t.Fatalf("%s 未保留注释或版本未更新:\n%s", file, body)
		}
	}
	result, err := Create(Options{SourceDir: root, OutputDir: t.TempDir()})
	if err != nil || result.Version != "1.3.0-beta.1+build.7" {
		t.Fatalf("Create() = %+v, %v", result, err)
	}
}

func TestReadVersionRejectsMismatchedThemeManifests(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "template.yaml", `apiVersion: sparq.io/v1
kind: Template
metadata:
  id: studio
  code: studio
  version: 1.2.3
sparq:
  protocol: ">=0.1.0 <0.2.0"
`)
	writeFile(t, root, "theme.yaml", validThemeManifest("studio", "1.2.4", "home"))
	if _, err := ReadVersion(root); err == nil || !strings.Contains(err.Error(), "不一致") {
		t.Fatalf("ReadVersion() error = %v", err)
	}
}

func writeFile(t *testing.T, root, relative, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func validThemeManifest(code, version, _ string) string {
	return "code: " + code + "\nname: Studio\nversion: " + version + `
appearance:
  colorSchemes: [light]
  palette:
    primary: "#2563EB"
formFactors: [desktop, tablet, mobile]
`
}
