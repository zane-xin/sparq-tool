package themecmd

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	pluginmanifest "github.com/zane-xin/sparq-tool/internal/plugin/manifest"
	remoteauth "github.com/zane-xin/sparq-tool/internal/remote/auth"
)

func TestThemePublishPackagesAndUploadsReviewedArchive(t *testing.T) {
	requestCount := 0
	digest := ""
	size := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestCount++
		if request.Header.Get("Authorization") != "Bearer theme-token" {
			t.Fatalf("request = %s, headers = %+v", request.URL.Path, request.Header)
		}
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/cloud-api/v1/developer/template-source-uploads/31/finalize" {
			if request.Method != http.MethodPost {
				t.Fatalf("finalize method = %s", request.Method)
			}
			_, _ = writer.Write([]byte(`{"code":0,"data":{"id":31,"version_id":32,"version":"0.1.3","origin":"ci","sha256":"` + digest + `","size_bytes":` + strconv.Itoa(size) + `,"status":"completed","created_at":100,"completed_at":101,"expires_at":200}}`))
			return
		}
		if request.URL.Path != "/cloud-api/v1/developer/templates/studio/source-uploads" || !strings.HasPrefix(request.Header.Get("Idempotency-Key"), "attempt:") {
			t.Fatalf("request = %s, headers = %+v", request.URL.Path, request.Header)
		}
		file, header, err := request.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(file)
		_ = file.Close()
		if readErr != nil || !strings.HasSuffix(header.Filename, "studio-0.1.3-theme.zip") || request.FormValue("changelog") != "调整首页" {
			t.Fatalf("upload = %s, %d bytes, changelog = %q, error = %v", header.Filename, len(body), request.FormValue("changelog"), readErr)
		}
		reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if err != nil {
			t.Fatal(err)
		}
		for _, manifestName := range []string{"template.yaml", "theme.yaml"} {
			manifest := zipFileBody(t, reader, manifestName)
			if !strings.Contains(manifest, "version: 0.1.3") {
				t.Fatalf("%s = %s", manifestName, manifest)
			}
		}
		hash := sha256.Sum256(body)
		digest = fmt.Sprintf("%x", hash)
		size = len(body)
		_, _ = writer.Write([]byte(`{"code":0,"data":{"id":31,"version_id":32,"version":"0.1.3","origin":"ci","sha256":"` + digest + `","size_bytes":` + strconv.FormatInt(int64(len(body)), 10) + `,"status":"uploading","created_at":100,"expires_at":200}}`))
	}))
	defer server.Close()

	root := createTestTheme(t)
	configPath := filepath.Join(t.TempDir(), "auth.json")
	if _, err := remoteauth.SaveSession(configPath, "develop", remoteauth.Session{Endpoint: server.URL, AccessToken: "theme-token"}); err != nil {
		t.Fatal(err)
	}
	command := newPublishCommandWithTerminalCheck(func(io.Reader) bool { return false })
	var output, progress bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&progress)
	command.SetArgs([]string{root, "--config", configPath, "--profile", "develop", "--bump", "patch", "-m", "调整首页"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if requestCount != 2 {
		t.Fatalf("request count = %d", requestCount)
	}
	for _, expected := range []string{"主题发布申请已提交", "应用: studio", "版本: 0.1.3", "发布记录: 31", "状态: completed"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("output missing %q:\n%s", expected, output.String())
		}
	}
	previous := -1
	for _, expected := range []string{"[1/4] 准备发布版本", "已更新 template.yaml 和 theme.yaml", "[2/4] 校验并打包主题", "[3/4] 创建或恢复源码会话", "完成：source_upload_id=31", "[4/4] 完成提交并进入审核"} {
		current := strings.Index(progress.String(), expected)
		if current <= previous {
			t.Fatalf("progress missing or out of order at %q:\n%s", expected, progress.String())
		}
		previous = current
	}
	version, err := readThemeVersion(root)
	if err != nil || version != "0.1.3" {
		t.Fatalf("theme version = %q, %v", version, err)
	}
}

func TestThemePublishReturnsPlatformPublishError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusNotFound)
		_, _ = writer.Write([]byte(`{"code":40400,"message":"应用不存在","data":null}`))
	}))
	defer server.Close()
	root := createTestTheme(t)
	configPath := filepath.Join(t.TempDir(), "auth.json")
	if _, err := remoteauth.SaveSession(configPath, "develop", remoteauth.Session{Endpoint: server.URL, AccessToken: "theme-token"}); err != nil {
		t.Fatal(err)
	}
	command := newPublishCommandWithTerminalCheck(func(io.Reader) bool { return false })
	command.SetArgs([]string{root, "--config", configPath, "--profile", "develop", "--keep-version"})
	err := command.Execute()
	if err == nil {
		t.Fatal("publish error = nil")
	}
	for _, expected := range []string{`提交主题 "studio" 版本 0.1.2 的审核包失败`, "应用不存在"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("error missing %q: %v", expected, err)
		}
	}
}

func TestChooseThemePublishVersionDefaultsToPatch(t *testing.T) {
	current, err := pluginmanifest.ParseVersion("1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	command := &cobra.Command{}
	command.SetIn(strings.NewReader("\n"))
	command.SetErr(io.Discard)
	target, err := chooseThemePublishVersion(command, current, publishOptions{}, func(io.Reader) bool { return true })
	if err != nil || target.String() != "1.2.4" {
		t.Fatalf("target = %s, error = %v", target, err)
	}
}

func TestResolveThemePublishStateDoesNotRepeatVersionBump(t *testing.T) {
	root := createTestTheme(t)
	command := &cobra.Command{}
	command.SetErr(io.Discard)
	options := publishOptions{bump: "patch"}
	state, resumed, err := resolveThemePublishState(command, root, "https://cloud.example.test", "develop", options, func(io.Reader) bool { return false })
	if err != nil || resumed || state.OriginalVersion != "0.1.2" || state.TargetVersion != "0.1.3" {
		t.Fatalf("first state = %+v, resumed=%v, error=%v", state, resumed, err)
	}
	if err := applyThemePublishVersion(command, root, state.OriginalVersion, state.TargetVersion); err != nil {
		t.Fatal(err)
	}
	recovered, resumed, err := resolveThemePublishState(command, root, "https://cloud.example.test", "develop", options, func(io.Reader) bool { return false })
	if err != nil || !resumed || recovered.TargetVersion != "0.1.3" {
		t.Fatalf("recovered state = %+v, resumed=%v, error=%v", recovered, resumed, err)
	}
}

func TestThemePublishRequiresVersionStrategyOutsideTerminal(t *testing.T) {
	root := createTestTheme(t)
	command := newPublishCommandWithTerminalCheck(func(io.Reader) bool { return false })
	command.SetArgs([]string{root})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "--bump patch") || !strings.Contains(err.Error(), "--keep-version") {
		t.Fatalf("error = %v", err)
	}
}

func createTestTheme(t *testing.T) string {
	t.Helper()
	t.Setenv("SPARQ_HOME", t.TempDir())
	root := t.TempDir()
	files := map[string]string{
		"template.yaml": `apiVersion: sparq.io/v1
kind: Template
metadata:
  id: studio
  code: studio
  version: 0.1.2
sparq:
  protocol: ">=0.1.0 <0.2.0"
`,
		"theme.yaml": `code: studio
name: Studio
version: 0.1.2
appearance:
  colorSchemes: [light]
  palette:
    primary: "#2563EB"
formFactors: [desktop, tablet, mobile]
`,
		"views/home.html": "<main>Studio</main>",
		"assets/site.css": "body {}",
	}
	for relative, body := range files {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func zipFileBody(t *testing.T, reader *zip.Reader, name string) string {
	t.Helper()
	for _, item := range reader.File {
		if item.Name != name {
			continue
		}
		file, err := item.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(file)
		_ = file.Close()
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	t.Fatalf("zip missing %s", name)
	return ""
}

func readThemeVersion(root string) (string, error) {
	body, err := os.ReadFile(filepath.Join(root, "template.yaml"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "version:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "version:")), nil
		}
	}
	return "", os.ErrNotExist
}
