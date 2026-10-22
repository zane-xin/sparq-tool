package plugincmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/internal/plugin/buildpipeline"
	"github.com/zane-xin/sparq-tool/internal/plugin/manifest"
	"github.com/zane-xin/sparq-tool/internal/plugin/sourcepack"
	"github.com/zane-xin/sparq-tool/internal/publishstate"
	remoteauth "github.com/zane-xin/sparq-tool/internal/remote/auth"
	remotemarketplace "github.com/zane-xin/sparq-tool/internal/remote/marketplace"
)

func TestPublishCommandBuildsPackagesAndSubmitsPlugin(t *testing.T) {
	requestCount := 0
	sourceSHA256 := ""
	candidateSHA256 := ""
	candidateSize := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestCount++
		if request.Header.Get("Authorization") != "Bearer upload-token" {
			t.Fatalf("headers = %+v", request.Header)
		}
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/cloud-api/v1/developer/applications/cms/publish-preflight":
			if request.Method != http.MethodPost || !strings.HasPrefix(request.Header.Get("Idempotency-Key"), "attempt:") {
				t.Fatalf("preflight request = %s %+v", request.Method, request.Header)
			}
			_, _ = writer.Write([]byte(`{"code":0,"data":{"application_id":7,"application_code":"cms","version":"1.2.4"}}`))
		case "/cloud-api/v1/developer/applications/cms/source-uploads":
			if !strings.HasPrefix(request.Header.Get("Idempotency-Key"), "attempt:") {
				t.Fatalf("source idempotency key = %q", request.Header.Get("Idempotency-Key"))
			}
			file, _, err := request.FormFile("file")
			if err != nil {
				t.Fatal(err)
			}
			body, readErr := io.ReadAll(file)
			_ = file.Close()
			if readErr != nil || len(body) == 0 || request.FormValue("changelog") != "新增文章审核" {
				t.Fatalf("source body = %d, changelog = %q, error = %v", len(body), request.FormValue("changelog"), readErr)
			}
			sourceSHA256 = fmt.Sprintf("%x", sha256.Sum256(body))
			_, _ = fmt.Fprintf(writer, `{"code":0,"data":{"id":12,"version_id":13,"version":"1.2.4","origin":"ci","sha256":"%s","size_bytes":%d,"status":"uploading","created_at":100,"expires_at":200}}`, sourceSHA256, len(body))
		case "/cloud-api/v1/developer/application-source-uploads/12/candidate-artifacts":
			if !strings.HasPrefix(request.Header.Get("Idempotency-Key"), "sha256:") {
				t.Fatalf("candidate idempotency key = %q", request.Header.Get("Idempotency-Key"))
			}
			file, header, err := request.FormFile("file")
			if err != nil {
				t.Fatal(err)
			}
			body, readErr := io.ReadAll(file)
			_ = file.Close()
			if readErr != nil || len(body) == 0 || !strings.HasSuffix(header.Filename, ".tar.gz") ||
				request.FormValue("source_sha256") != sourceSHA256 || request.FormValue("format") != "sparq.plugin.package.v1" ||
				request.FormValue("kind") != "plugin_package" {
				t.Fatalf("candidate = %s, body = %d, form = %+v, error = %v", header.Filename, len(body), request.MultipartForm.Value, readErr)
			}
			candidateSHA256 = fmt.Sprintf("%x", sha256.Sum256(body))
			candidateSize = len(body)
			_, _ = fmt.Fprintf(writer, `{"code":0,"data":{"id":21,"source_upload_id":12,"version_id":13,"source_sha256":"%s","sha256":"%s","size_bytes":%d,"format":"sparq.plugin.package.v1","kind":"plugin_package","verification_status":"verified","trust_decision":"rebuild_required","created_at":101}}`, sourceSHA256, candidateSHA256, len(body))
		case "/cloud-api/v1/developer/application-source-uploads/12/finalize":
			if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("finalize request = %s %+v", request.Method, request.Header)
			}
			body, err := io.ReadAll(request.Body)
			if err != nil || !strings.Contains(string(body), candidateSHA256) || !strings.Contains(string(body), `"kind":"plugin_package"`) {
				t.Fatalf("finalize body = %s, error = %v", body, err)
			}
			_, _ = fmt.Fprintf(writer, `{"code":0,"data":{"upload":{"id":12,"version_id":13,"version":"1.2.4","origin":"ci","sha256":"%s","size_bytes":1,"status":"completed","created_at":100,"completed_at":102,"expires_at":200},"candidates":[{"id":21,"source_upload_id":12,"version_id":13,"source_sha256":"%s","sha256":"%s","size_bytes":%d,"format":"sparq.plugin.package.v1","kind":"plugin_package","verification_status":"verified","trust_decision":"rebuild_required","created_at":101}]}}`, sourceSHA256, sourceSHA256, candidateSHA256, candidateSize)
		default:
			t.Fatalf("path = %s", request.URL.Path)
		}
	}))
	defer server.Close()

	root := createUploadPlugin(t)
	configPath := filepath.Join(t.TempDir(), "auth.json")
	if _, err := remoteauth.SaveSession(configPath, "develop", remoteauth.Session{Endpoint: server.URL, AccessToken: "upload-token"}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	var progress bytes.Buffer
	command := newPublishCommand()
	command.SetOut(&output)
	command.SetErr(&progress)
	command.SetArgs([]string{root, "--config", configPath, "--bump", "patch", "-m", "新增文章审核"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if requestCount != 4 || !strings.Contains(output.String(), "插件发布申请已提交") || !strings.Contains(output.String(), "应用: cms") || !strings.Contains(output.String(), "版本: 1.2.4") || !strings.Contains(output.String(), "源码上传记录: 12") || !strings.Contains(output.String(), "候选 Artifact 数量: 1") || !strings.Contains(output.String(), "plugin_package: ID 21") || !strings.Contains(output.String(), "信任 rebuild_required") {
		t.Fatalf("output = %s", output.String())
	}
	progressOutput := progress.String()
	previous := -1
	for _, expected := range []string{
		"[1/10] 准备发布版本",
		"[2/10] 远程发布预检",
		"[3/10] 校验源码与候选缓存",
		"[4/10] 构建插件",
		"[5/10] 构建平台产物",
		"[6/10] 准备候选构建包",
		"[7/10] 打包源码",
		"[8/10] 创建或恢复源码会话",
		"完成：source_upload_id=12",
		"[9/10] 补传候选构建包",
		"[1/1] plugin_package",
		"完成：1/1",
		"[10/10] 完成提交并进入审核",
		"完成：提交已冻结，等待平台审核",
	} {
		current := strings.Index(progressOutput, expected)
		if current <= previous {
			t.Fatalf("progress missing or out of order at %q:\n%s", expected, progressOutput)
		}
		previous = current
	}
	if strings.Contains(output.String(), "[1/10]") {
		t.Fatalf("stdout contains progress output: %s", output.String())
	}
	assertPluginVersion(t, root, "1.2.4")
}

func TestCreatePublishPackagesCombinesFrontendAndPlatformBinaries(t *testing.T) {
	root := createUploadPlugin(t)
	frontendPath := filepath.Join(root, "web-output")
	if err := os.MkdirAll(frontendPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(frontendPath, "index.js"), []byte("export default {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	binaries := make([]buildpipeline.PlatformBinary, 0, 6)
	for _, platform := range buildpipeline.DefaultPublishPlatforms() {
		name := "cms"
		if platform.OS == "windows" {
			name += ".exe"
		}
		binaryPath := filepath.Join(root, "matrix", platform.OS, platform.Arch, name)
		if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(binaryPath, []byte(platform.OS+"/"+platform.Arch), 0o755); err != nil {
			t.Fatal(err)
		}
		binaries = append(binaries, buildpipeline.PlatformBinary{Platform: platform, Path: binaryPath})
	}
	results, err := createPublishPackages(root, buildpipeline.Result{FrontendBuilt: true, FrontendPath: frontendPath}, binaries)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 6 || results[0].Kind != "managed_remote" || results[0].TargetOS != "linux" || results[0].TargetArch != "amd64" ||
		results[5].TargetOS != "windows" || results[5].TargetArch != "arm64" {
		t.Fatalf("results = %+v", results)
	}
	for _, result := range results {
		if info, statErr := os.Stat(result.ArchivePath); statErr != nil || info.Size() == 0 {
			t.Fatalf("archive %s = %+v, %v", result.ArchivePath, info, statErr)
		}
	}
}

func TestCreatePublishPackagesKeepsFrontendBundleForFrontendOnlyPlugin(t *testing.T) {
	root := createUploadPlugin(t)
	frontendPath := filepath.Join(root, "web-output")
	if err := os.MkdirAll(frontendPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(frontendPath, "index.js"), []byte("export default {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	results, err := createPublishPackages(root, buildpipeline.Result{FrontendBuilt: true, FrontendPath: frontendPath}, nil)
	if err != nil || len(results) != 1 || results[0].Kind != "frontend_bundle" {
		t.Fatalf("results = %+v, error = %v", results, err)
	}
}

func TestResolvePluginPublishStateDoesNotRepeatVersionBump(t *testing.T) {
	root := createUploadPlugin(t)
	command := &cobra.Command{}
	command.SetErr(io.Discard)
	options := publishOptions{bump: "patch"}
	state, resumed, err := resolvePluginPublishState(command, root, "https://cloud.example.test", "develop", options, func(io.Reader) bool { return false })
	if err != nil || resumed || state.OriginalVersion != "1.2.3" || state.TargetVersion != "1.2.4" {
		t.Fatalf("first state = %+v, resumed=%v, error=%v", state, resumed, err)
	}
	if err := applyPublishVersion(command, root, state.OriginalVersion, state.TargetVersion); err != nil {
		t.Fatal(err)
	}
	recovered, resumed, err := resolvePluginPublishState(command, root, "https://cloud.example.test", "develop", options, func(io.Reader) bool { return false })
	if err != nil || !resumed || recovered.TargetVersion != "1.2.4" {
		t.Fatalf("recovered state = %+v, resumed=%v, error=%v", recovered, resumed, err)
	}
	assertPluginVersion(t, root, "1.2.4")
}

func TestMatchingPublishedCandidateSkipsOnlyIdenticalVariant(t *testing.T) {
	expected := remotemarketplace.CandidateExpectation{
		SHA256: strings.Repeat("a", 64), Kind: remotemarketplace.ArtifactKindManagedRemote,
		Runtime: "http", TargetOS: "linux", TargetArch: "amd64",
	}
	candidate := remotemarketplace.CandidateArtifact{
		ID: 21, SHA256: strings.Repeat("a", 64), Kind: expected.Kind,
		Runtime: expected.Runtime, TargetOS: expected.TargetOS, TargetArch: expected.TargetArch,
	}
	matched, found, err := matchingPublishedCandidate([]remotemarketplace.CandidateArtifact{candidate}, expected)
	if err != nil || !found || matched.ID != 21 {
		t.Fatalf("matchingPublishedCandidate() = %+v, %v, %v", matched, found, err)
	}
	candidate.SHA256 = strings.Repeat("b", 64)
	if _, _, err := matchingPublishedCandidate([]remotemarketplace.CandidateArtifact{candidate}, expected); err == nil {
		t.Fatal("candidate variant with different SHA-256 should fail")
	}
}

func TestPublishCommandKeepsSourceContextWhenCandidateUploadFails(t *testing.T) {
	sourceSHA256 := ""
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/cloud-api/v1/developer/applications/cms/publish-preflight" {
			_, _ = writer.Write([]byte(`{"code":0,"data":{"application_id":7,"application_code":"cms","version":"1.2.3"}}`))
			return
		}
		if request.URL.Path == "/cloud-api/v1/developer/applications/cms/source-uploads" {
			file, _, err := request.FormFile("file")
			if err != nil {
				t.Fatal(err)
			}
			body, readErr := io.ReadAll(file)
			_ = file.Close()
			if readErr != nil {
				t.Fatal(readErr)
			}
			sourceSHA256 = fmt.Sprintf("%x", sha256.Sum256(body))
			_, _ = fmt.Fprintf(writer, `{"code":0,"data":{"id":12,"version_id":13,"version":"1.2.3","origin":"ci","sha256":"%s","size_bytes":%d,"status":"uploading","created_at":100}}`, sourceSHA256, len(body))
			return
		}
		writer.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = writer.Write([]byte(`{"code":42200,"message":"候选构建包无效","data":null}`))
	}))
	defer server.Close()

	root := createUploadPlugin(t)
	configPath := filepath.Join(t.TempDir(), "auth.json")
	if _, err := remoteauth.SaveSession(configPath, "develop", remoteauth.Session{Endpoint: server.URL, AccessToken: "upload-token"}); err != nil {
		t.Fatal(err)
	}
	command := newPublishCommand()
	var progress bytes.Buffer
	command.SetErr(&progress)
	command.SetArgs([]string{root, "--config", configPath, "--version", "1.2.3"})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "源码上传成功，但候选构建包") || !strings.Contains(err.Error(), "上传失败") ||
		!strings.Contains(err.Error(), "source_upload_id: 12") || !strings.Contains(err.Error(), sourceSHA256) {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(progress.String(), "[9/10] 补传候选构建包") || !strings.Contains(progress.String(), "[1/1] plugin_package") || strings.Contains(progress.String(), "完成：1/1") {
		t.Fatalf("progress = %s", progress.String())
	}
}

func TestPublishCommandExplainsMissingMarketplaceApp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusNotFound)
		_, _ = writer.Write([]byte(`{"code":40400,"message":"应用不存在","data":null}`))
	}))
	defer server.Close()

	root := createUploadPlugin(t)
	configPath := filepath.Join(t.TempDir(), "auth.json")
	if _, err := remoteauth.SaveSession(configPath, "develop", remoteauth.Session{Endpoint: server.URL, AccessToken: "upload-token"}); err != nil {
		t.Fatal(err)
	}
	command := newPublishCommand()
	command.SetArgs([]string{root, "--config", configPath, "--version", "1.2.3"})
	err := command.Execute()
	if err == nil {
		t.Fatal("publish error = nil, want missing app guidance")
	}
	message := err.Error()
	for _, expected := range []string{
		`发布插件 "cms" 失败`, `metadata.code 是否为 "cms"`, server.URL,
		`认证 profile "develop"`, "平台管理员", "服务端返回: Application 发布预检失败: 应用不存在",
	} {
		if !strings.Contains(message, expected) {
			t.Fatalf("error missing %q: %s", expected, message)
		}
	}
}

func TestPublishCommandExplainsLockedVersionSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusConflict)
		_, _ = writer.Write([]byte(`{"code":40900,"message":"该版本已进入校验或审核流程，不能上传新的源码；请提升 plugin.yaml 版本号，审核驳回后可重新提交原版本","data":null}`))
	}))
	defer server.Close()

	root := createUploadPlugin(t)
	configPath := filepath.Join(t.TempDir(), "auth.json")
	if _, err := remoteauth.SaveSession(configPath, "develop", remoteauth.Session{Endpoint: server.URL, AccessToken: "upload-token"}); err != nil {
		t.Fatal(err)
	}
	command := newPublishCommand()
	command.SetArgs([]string{root, "--config", configPath, "--version", "1.2.3"})
	err := command.Execute()
	if err == nil {
		t.Fatal("publish error = nil, want locked version guidance")
	}
	for _, expected := range []string{`插件 "cms" 版本 1.2.3 发布预检失败`, "不能上传新的源码", "提升 plugin.yaml 版本号", "审核驳回后可重新提交原版本"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("error missing %q: %v", expected, err)
		}
	}
}

func TestPublishCommandRegistersPublishFlags(t *testing.T) {
	command := newPublishCommand()
	for _, name := range []string{"message", "config", "timeout", "bump", "version", "rebuild", "restart"} {
		if command.Flags().Lookup(name) == nil {
			t.Fatalf("missing flag --%s", name)
		}
	}
	for _, name := range []string{"commit-sha", "idempotency-key", "keep-version", "message-file", "pipeline-id", "profile", "source-ref"} {
		if command.Flags().Lookup(name) != nil {
			t.Fatalf("removed flag --%s should not be registered", name)
		}
	}
}

func TestValidatePublishOptionsRejectsConflictingVersionStrategies(t *testing.T) {
	err := validatePublishOptions(publishOptions{timeout: defaultPublishTimeout, bump: "patch", version: "1.2.3"}, "")
	if err == nil || !strings.Contains(err.Error(), "--bump 和 --version 不能同时使用") {
		t.Fatalf("error = %v", err)
	}
}

func TestRestartApplicationPublishCancelsOldSessionAndUsesNewAttemptKey(t *testing.T) {
	t.Setenv("SPARQ_HOME", t.TempDir())
	oldKey := "attempt:old"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/cloud-api/v1/developer/application-source-uploads/8/cancel":
			_, _ = writer.Write([]byte(`{"code":0,"data":{"id":8,"version_id":9,"version":"1.2.3","status":"canceled"}}`))
		case "/cloud-api/v1/developer/applications/cms/publish-preflight":
			if request.Header.Get("Idempotency-Key") == oldKey {
				t.Fatal("restart reused the canceled session idempotency key")
			}
			_, _ = writer.Write([]byte(`{"code":0,"data":{"application_id":7,"application_code":"cms","version":"1.2.3"}}`))
		default:
			t.Fatalf("path = %s", request.URL.Path)
		}
	}))
	defer server.Close()
	state := publishstate.State{
		Kind: "plugin", SourceDir: t.TempDir(), Endpoint: server.URL, Profile: "develop", Code: "cms",
		OriginalVersion: "1.2.3", TargetVersion: "1.2.3", IdempotencyKey: oldKey,
		SourceFingerprint: strings.Repeat("a", 64), SourceUploadID: 8,
	}
	command := &cobra.Command{}
	command.SetContext(context.Background())
	result, preflight, err := restartApplicationPublish(
		command, remotemarketplace.NewClient(server.Client()),
		remoteauth.Session{Endpoint: server.URL, AccessToken: "token"}, "develop", state.SourceDir,
		state, remotemarketplace.PublishPreflight{ApplicationID: 7, ApplicationCode: "cms", Version: "1.2.3", ResumableUpload: &remotemarketplace.SourceUpload{ID: 8}},
		"cms", "版本说明",
	)
	if err != nil || result.IdempotencyKey == oldKey || result.SourceUploadID != 0 || result.SourceFingerprint != "" || preflight.ResumableUpload != nil {
		t.Fatalf("restartApplicationPublish() = %+v, %+v, %v", result, preflight, err)
	}
}

func TestChoosePublishVersionRequiresStrategyOutsideTerminal(t *testing.T) {
	current, err := manifest.ParseVersion("1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	command := &cobra.Command{}
	command.SetIn(strings.NewReader(""))
	_, err = choosePublishVersion(command, current, publishOptions{}, func(io.Reader) bool { return false })
	if err == nil || !strings.Contains(err.Error(), "--bump patch") || !strings.Contains(err.Error(), "--version <version>") {
		t.Fatalf("error = %v", err)
	}
}

func TestChoosePublishVersionInteractivelyDefaultsToPatch(t *testing.T) {
	current, err := manifest.ParseVersion("1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	command := &cobra.Command{}
	command.SetIn(strings.NewReader("\n"))
	var prompts bytes.Buffer
	command.SetErr(&prompts)
	target, err := choosePublishVersion(command, current, publishOptions{}, func(io.Reader) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if target.String() != "1.2.4" || !strings.Contains(prompts.String(), "Patch 1.2.4（默认）") {
		t.Fatalf("target = %s, prompts = %s", target, prompts.String())
	}
}

func TestChoosePublishVersionAcceptsInteractiveCustomVersion(t *testing.T) {
	current, err := manifest.ParseVersion("1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	command := &cobra.Command{}
	command.SetIn(strings.NewReader("5\n2.0.0-beta.1\n"))
	command.SetErr(io.Discard)
	target, err := choosePublishVersion(command, current, publishOptions{}, func(io.Reader) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if target.String() != "2.0.0-beta.1" {
		t.Fatalf("target = %s", target)
	}
}

func TestPreparePublishVersionKeepsManifestBytesAndInvalidatesBuiltManifest(t *testing.T) {
	root := createUploadPlugin(t)
	manifestPath := filepath.Join(root, manifest.DefaultFileName)
	original, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	releaseDir := filepath.Join(root, "release")
	if err := os.MkdirAll(releaseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	builtManifest := filepath.Join(releaseDir, "manifest.json")
	if err := os.WriteFile(builtManifest, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	command := &cobra.Command{}
	command.SetErr(io.Discard)
	version, err := preparePublishVersion(command, root, publishOptions{version: "1.2.3"}, func(io.Reader) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if version != "1.2.3" {
		t.Fatalf("version = %s", version)
	}
	current, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current, original) {
		t.Fatalf("plugin.yaml changed when publishing its current version:\n%s", current)
	}
	if _, err := os.Stat(builtManifest); !os.IsNotExist(err) {
		t.Fatalf("release/manifest.json error = %v, want not exist", err)
	}
}

func TestPublishCommandDoesNotChangeVersionWhenPreflightFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusConflict)
		_, _ = writer.Write([]byte(`{"code":40900,"message":"版本已锁定","data":null}`))
	}))
	defer server.Close()

	root := createUploadPlugin(t)
	configPath := filepath.Join(t.TempDir(), "auth.json")
	if _, err := remoteauth.SaveSession(configPath, "develop", remoteauth.Session{Endpoint: server.URL, AccessToken: "upload-token"}); err != nil {
		t.Fatal(err)
	}
	command := newPublishCommand()
	command.SetArgs([]string{root, "--config", configPath, "--bump", "minor"})
	if err := command.Execute(); err == nil {
		t.Fatal("publish error = nil")
	}
	assertPluginVersion(t, root, "1.2.3")
	generated, err := os.ReadFile(filepath.Join(root, manifest.DefaultGeneratedFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), `\"version\":\"1.2.3\"`) {
		t.Fatalf("manifest_gen.go changed before preflight passed:\n%s", generated)
	}
}

func TestPublishCommandChecksAuthenticationBeforeChangingVersion(t *testing.T) {
	root := createUploadPlugin(t)
	configPath := filepath.Join(t.TempDir(), "auth.json")
	if _, err := remoteauth.SaveSession(configPath, "expired", remoteauth.Session{
		Endpoint: "https://sparq.example.test", AccessToken: "expired-token", ExpiresAt: time.Now().Unix() - 1,
	}); err != nil {
		t.Fatal(err)
	}
	command := newPublishCommand()
	command.SetArgs([]string{root, "--config", configPath, "--bump", "major"})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "已过期") {
		t.Fatalf("error = %v", err)
	}
	assertPluginVersion(t, root, "1.2.3")
}

func TestPublishIntegrityChecksRejectMismatchedPlatformResults(t *testing.T) {
	packed := sourcepack.Result{Version: "1.2.3", SHA256: strings.Repeat("a", 64)}
	upload := remotemarketplace.SourceUpload{ID: 12, VersionID: 13, Version: "1.2.4", SHA256: strings.Repeat("b", 64)}
	if err := validatePublishedSource(packed, upload); err == nil || !strings.Contains(err.Error(), "插件源码包不一致") {
		t.Fatalf("source validation error = %v", err)
	}

	upload.Version = packed.Version
	upload.SHA256 = packed.SHA256
	result := pluginPackageResult{Kind: remotemarketplace.ArtifactKindManagedRemote, Runtime: "http", TargetOS: "linux", TargetArch: "amd64"}
	candidate := remotemarketplace.CandidateArtifact{
		SourceUploadID: upload.ID, VersionID: upload.VersionID, SourceSHA256: upload.SHA256,
		SHA256: strings.Repeat("c", 64), Format: "sparq.plugin.package.v1", Kind: result.Kind,
		Runtime: result.Runtime, TargetOS: result.TargetOS, TargetArch: result.TargetArch,
	}
	if err := validatePublishedCandidate(result, strings.Repeat("d", 64), upload, candidate); err == nil || !strings.Contains(err.Error(), "候选构建包与本地产物不一致") {
		t.Fatalf("candidate validation error = %v", err)
	}
}

func assertPluginVersion(t *testing.T, root string, want string) {
	t.Helper()
	item, err := manifest.Read(filepath.Join(root, manifest.DefaultFileName))
	if err != nil {
		t.Fatal(err)
	}
	if item.Metadata.Version != want {
		t.Fatalf("plugin version = %s, want %s", item.Metadata.Version, want)
	}
}

func createUploadPlugin(t *testing.T) string {
	t.Helper()
	t.Setenv("SPARQ_HOME", t.TempDir())
	root := t.TempDir()
	protocolOutput, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/zane-xin/sparq-protocol").Output()
	if err != nil {
		t.Fatal(err)
	}
	protocolDir := strings.TrimSpace(string(protocolOutput))
	files := map[string]string{
		"plugin.yaml": `apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: cms
  code: cms
  name: CMS
  version: 1.2.3
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec:
  runtime:
    supported: [local]
`,
		"go.mod": "module example.com/cms\n\ngo 1.26.1\n\nrequire (\n\tgithub.com/Masterminds/semver/v3 v3.4.0 // indirect\n\tgithub.com/zane-xin/sparq-protocol v0.0.0\n)\n\nreplace github.com/zane-xin/sparq-protocol v0.0.0 => " + filepath.ToSlash(protocolDir) + "\n",
		"go.sum": "github.com/Masterminds/semver/v3 v3.4.0 h1:Zog+i5UMtVoCU8oKka5P7i9q9HgrJeGzI9SA1Xbatp0=\n" +
			"github.com/Masterminds/semver/v3 v3.4.0/go.mod h1:4V+yj/TJE1HU9XfppCwVMZq3I84lprf4nC11bSS5beM=\n",
		"bootstrap.go":     "package cms\n",
		"sparq.build.yaml": "version: 1\ngo:\n  enabled: false\n",
	}
	for relative, body := range files {
		if err := os.WriteFile(filepath.Join(root, relative), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	generated, err := manifest.GenerateGo(manifest.GoGenerateOptions{ManifestPath: filepath.Join(root, "plugin.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.WriteGo(generated); err != nil {
		t.Fatal(err)
	}
	return root
}
