package release

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	remoteauth "github.com/zane-xin/sparq-tool/internal/remote/auth"
)

const maxResponseBytes = 1 << 20

type Client struct{ httpClient *http.Client }

type PublishInput struct {
	Session       remoteauth.Session
	ManifestPath  string
	Runtimes      []RuntimeUpload
	Updaters      []UpdaterUpload
	AdminPath     string
	ChangelogPath string
}

type RuntimeUpload struct {
	TargetOS   string
	TargetArch string
	Path       string
}

type UpdaterUpload = RuntimeUpload

type PublishResult struct {
	Version string `json:"version"`
}

type responseEnvelope struct {
	Code    int             `json:"code"`
	Data    json.RawMessage `json:"data"`
	Message string          `json:"message"`
}

func NewClient(httpClient *http.Client) Client {
	if httpClient == nil {
		panic("release HTTP client is required")
	}
	return Client{httpClient: httpClient}
}

func (client Client) Publish(ctx context.Context, input PublishInput) (PublishResult, error) {
	endpoint, err := remoteauth.NormalizeEndpoint(input.Session.Endpoint)
	if err != nil {
		return PublishResult{}, err
	}
	if err := validateUploadEndpoint(endpoint); err != nil {
		return PublishResult{}, err
	}
	if strings.TrimSpace(input.Session.AccessToken) == "" {
		return PublishResult{}, fmt.Errorf("认证 profile 缺少 access token")
	}
	files := []uploadFile{{field: "manifest", path: input.ManifestPath}, {field: "admin", path: input.AdminPath}, {field: "changelog", path: input.ChangelogPath}}
	for _, runtime := range input.Runtimes {
		files = append(files, uploadFile{field: RuntimeField(runtime.TargetOS, runtime.TargetArch), path: runtime.Path})
	}
	for _, updater := range input.Updaters {
		files = append(files, uploadFile{field: UpdaterField(updater.TargetOS, updater.TargetArch), path: updater.Path})
	}
	body, contentType, cleanup, err := multipartBody(files)
	if err != nil {
		return PublishResult{}, err
	}
	defer cleanup()
	var published PublishResult
	if err := client.request(ctx, http.MethodPost, endpoint+"/cloud-api/v1/releases/sparq", contentType, body, input.Session.AccessToken, &published); err != nil {
		return PublishResult{}, fmt.Errorf("上传 Sparq 官方发行包失败: %w", err)
	}
	return published, nil
}

func validateUploadEndpoint(endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	// TODO: Require HTTPS once production deployments have complete HTTPS support.
	if parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("Sparq Cloud 发布地址必须使用 HTTP 或 HTTPS")
	}
	return nil
}

// RuntimeField 返回目标平台 Runtime 在 multipart 请求中的字段名。
func RuntimeField(targetOS, targetArch string) string {
	return "runtime_" + targetOS + "_" + targetArch
}

// UpdaterField 返回目标平台 updater 在 multipart 请求中的字段名。
func UpdaterField(targetOS, targetArch string) string {
	return "updater_" + targetOS + "_" + targetArch
}

func (client Client) request(ctx context.Context, method, target, contentType string, body io.Reader, accessToken string, output any) error {
	request, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "sparq-tool")
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxResponseBytes {
		return fmt.Errorf("发行接口响应超过 %d 字节限制", maxResponseBytes)
	}
	var envelope responseEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("解析发行接口响应失败: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices || envelope.Code != 0 {
		if envelope.Message != "" {
			return errors.New(envelope.Message)
		}
		return fmt.Errorf("发行接口请求失败: HTTP %d, code %d", response.StatusCode, envelope.Code)
	}
	if output != nil && len(envelope.Data) > 0 && !bytes.Equal(envelope.Data, []byte("null")) {
		if err := json.Unmarshal(envelope.Data, output); err != nil {
			return fmt.Errorf("解析发行接口数据失败: %w", err)
		}
	}
	return nil
}

type uploadFile struct {
	field string
	path  string
}

func multipartBody(files []uploadFile) (*os.File, string, func(), error) {
	temporary, err := os.CreateTemp("", "sparq-app-release-upload-*.multipart")
	if err != nil {
		return nil, "", func() {}, err
	}
	cleanup := func() { _ = temporary.Close(); _ = os.Remove(temporary.Name()) }
	writer := multipart.NewWriter(temporary)
	for _, file := range files {
		input, err := os.Open(file.path)
		if err != nil {
			cleanup()
			return nil, "", func() {}, fmt.Errorf("打开上传文件 %s 失败: %w", file.path, err)
		}
		part, err := writer.CreateFormFile(file.field, filepath.Base(file.path))
		if err == nil {
			_, err = io.Copy(part, input)
		}
		closeErr := input.Close()
		if err = errors.Join(err, closeErr); err != nil {
			cleanup()
			return nil, "", func() {}, err
		}
	}
	if err := writer.Close(); err != nil {
		cleanup()
		return nil, "", func() {}, err
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, "", func() {}, err
	}
	return temporary, writer.FormDataContentType(), cleanup, nil
}
