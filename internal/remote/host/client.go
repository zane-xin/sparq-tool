package host

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxResponseBytes = 1 << 20

type Credentials struct {
	Endpoint string
	Session  string
	CSRF     string
}

type Artifact struct {
	Kind      string `json:"kind"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	SizeBytes uint64 `json:"size_bytes"`
	ExpiresAt int64  `json:"expires_at,omitempty"`
	Format    string `json:"format,omitempty"`
}

type BuildManifest struct {
	InstanceID string `json:"instance_id"`
	TargetOS   string `json:"target_os"`
	TargetArch string `json:"target_arch"`
}

type Theme struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
	Active      bool   `json:"active"`
	Protected   bool   `json:"protected"`
}

type Operation struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	Code       string         `json:"code"`
	Action     string         `json:"action"`
	Status     string         `json:"status"`
	Stage      string         `json:"stage"`
	Error      string         `json:"error"`
	Result     map[string]any `json:"result"`
	CreatedAt  int64          `json:"created_at"`
	StartedAt  int64          `json:"started_at"`
	FinishedAt int64          `json:"finished_at"`
}

type Client struct {
	httpClient *http.Client
}

func NewClient(httpClient *http.Client) Client {
	if httpClient == nil {
		panic("host HTTP client is required")
	}
	return Client{httpClient: httpClient}
}

func (client Client) InstallTheme(ctx context.Context, credentials Credentials, grant string, activate bool) (Operation, error) {
	var result Operation
	err := client.request(ctx, credentials, http.MethodPost, "/admin-api/site/themes/install-artifact", map[string]any{
		"grant": grant, "activate": activate,
	}, &result)
	return result, err
}

func (client Client) ActivateTheme(ctx context.Context, credentials Credentials, code, version string) (Theme, error) {
	var result Theme
	err := client.request(ctx, credentials, http.MethodPost, "/admin-api/site/themes/"+url.PathEscape(code)+"/"+url.PathEscape(version)+"/activate", map[string]any{}, &result)
	return result, err
}

func (client Client) RemoveTheme(ctx context.Context, credentials Credentials, code, version string) error {
	var result struct {
		Uninstalled bool `json:"uninstalled"`
	}
	if err := client.request(ctx, credentials, http.MethodDelete, "/admin-api/site/themes/"+url.PathEscape(code)+"/"+url.PathEscape(version), map[string]any{}, &result); err != nil {
		return err
	}
	if !result.Uninstalled {
		return fmt.Errorf("宿主未确认主题卸载完成")
	}
	return nil
}

func (client Client) RollbackTheme(ctx context.Context, credentials Credentials, code string) (Operation, error) {
	var result Operation
	err := client.request(ctx, credentials, http.MethodPost, "/admin-api/site/themes/"+url.PathEscape(code)+"/rollback", map[string]any{}, &result)
	return result, err
}

func (client Client) BuildManifest(ctx context.Context, credentials Credentials) (BuildManifest, error) {
	var result BuildManifest
	err := client.request(ctx, credentials, http.MethodGet, "/admin-api/build-manifest", nil, &result)
	if err == nil && result.InstanceID == "" {
		err = fmt.Errorf("宿主 Build Manifest 未返回 instance_id")
	}
	return result, err
}

func (client Client) Operation(ctx context.Context, credentials Credentials, id string) (Operation, error) {
	var result Operation
	err := client.request(ctx, credentials, http.MethodGet, "/admin-api/installation-operations/"+url.PathEscape(id), nil, &result)
	return result, err
}

func (client Client) WaitOperation(ctx context.Context, credentials Credentials, operation Operation) (Operation, error) {
	if operation.ID == "" {
		return Operation{}, fmt.Errorf("宿主未返回安装 operation ID")
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, err := client.Operation(ctx, credentials, operation.ID)
		if err != nil {
			return Operation{}, err
		}
		switch current.Status {
		case "succeeded":
			return current, nil
		case "failed":
			if current.Error == "" {
				current.Error = "宿主安装操作失败"
			}
			return current, fmt.Errorf("%s", current.Error)
		}
		select {
		case <-ctx.Done():
			return current, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (client Client) request(ctx context.Context, credentials Credentials, method string, path string, input any, output any) error {
	endpoint := strings.TrimRight(strings.TrimSpace(credentials.Endpoint), "/")
	if _, err := url.ParseRequestURI(endpoint); err != nil || endpoint == "" {
		return fmt.Errorf("宿主 URL 无效")
	}
	if credentials.Session == "" {
		return fmt.Errorf("宿主 admin session 不能为空")
	}
	var body io.Reader
	if input != nil {
		payload, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "sparq-tool")
	request.AddCookie(&http.Cookie{Name: "sparq_admin_session", Value: credentials.Session})
	if input != nil {
		if credentials.CSRF == "" {
			return fmt.Errorf("宿主 CSRF token 不能为空")
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Sparq-CSRF-Token", credentials.CSRF)
		request.AddCookie(&http.Cookie{Name: "sparq_csrf_token", Value: credentials.CSRF})
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("请求 Sparq 宿主管理 API 失败: %w", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(payload) > maxResponseBytes {
		return fmt.Errorf("宿主管理 API 响应过大")
	}
	var envelope struct {
		Code    int             `json:"code"`
		Data    json.RawMessage `json:"data"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return fmt.Errorf("解析宿主管理 API 响应失败: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || envelope.Code != 0 {
		return fmt.Errorf("宿主管理 API 请求失败: HTTP %d, code %d, %s", response.StatusCode, envelope.Code, envelope.Message)
	}
	if output != nil {
		if err := json.Unmarshal(envelope.Data, output); err != nil {
			return fmt.Errorf("解析宿主管理 API 数据失败: %w", err)
		}
	}
	return nil
}
