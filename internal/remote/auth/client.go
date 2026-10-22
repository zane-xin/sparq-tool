package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	DefaultEndpoint  = "https://sparq.xin"
	maxResponseBytes = 1 << 20
)

// Account 是认证接口返回的当前 Sparq Cloud 账号摘要。
type Account struct {
	ID       uint64 `json:"id"`
	Username string `json:"username,omitempty"`
	Nickname string `json:"nickname"`
	Email    string `json:"email,omitempty"`
	Mobile   string `json:"mobile,omitempty"`
	Status   string `json:"status"`
}

// APIError 描述 Sparq Cloud 返回的认证错误。
type APIError struct {
	Status  int
	Code    int
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("认证接口请求失败: HTTP %d, code %d", e.Status, e.Code)
}

// IsUnauthorized 判断认证请求是否因 Session 无效而失败。
func IsUnauthorized(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && (apiErr.Status == http.StatusUnauthorized || apiErr.Code == 40100)
}

// Client 调用 Sparq Cloud 用户认证接口。
type Client struct {
	httpClient *http.Client
}

func NewClient(httpClient *http.Client) Client {
	if httpClient == nil {
		panic("auth HTTP client is required")
	}
	return Client{httpClient: httpClient}
}

type loginRequest struct {
	IdentifierType string `json:"identifier_type"`
	Identifier     string `json:"identifier"`
	Password       string `json:"password"`
}

type loginResult struct {
	Account     Account `json:"account"`
	AccessToken string  `json:"access_token"`
	ExpiresAt   int64   `json:"expires_at"`
}

type currentAccount struct {
	Account Account `json:"account"`
}

type responseEnvelope struct {
	Code    int             `json:"code"`
	Data    json.RawMessage `json:"data"`
	Message string          `json:"message"`
}

// Login 使用 Website 用户中心创建用户 Session。
func (c Client) Login(ctx context.Context, endpoint string, identifierType string, identifier string, password []byte) (Session, error) {
	resolvedEndpoint, err := NormalizeEndpoint(endpoint)
	if err != nil {
		return Session{}, err
	}
	identifierType = strings.TrimSpace(identifierType)
	switch identifierType {
	case "email", "username", "mobile":
	default:
		return Session{}, fmt.Errorf("登录标识类型必须是 email、username 或 mobile")
	}
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return Session{}, fmt.Errorf("登录账号不能为空")
	}
	if len(password) == 0 {
		return Session{}, fmt.Errorf("登录密码不能为空")
	}

	requestBody, err := json.Marshal(loginRequest{
		IdentifierType: identifierType,
		Identifier:     identifier,
		Password:       string(password),
	})
	if err != nil {
		return Session{}, fmt.Errorf("编码登录请求失败: %w", err)
	}
	var result loginResult
	if err := c.request(ctx, http.MethodPost, resolvedEndpoint+"/cloud-api/usercenter/v1/sessions", "", requestBody, &result); err != nil {
		return Session{}, fmt.Errorf("登录 Sparq Cloud 失败: %w", err)
	}
	if result.AccessToken == "" {
		return Session{}, fmt.Errorf("登录 Sparq Cloud 失败: 服务端未返回 access token")
	}
	return Session{
		Endpoint:    resolvedEndpoint,
		AccessToken: result.AccessToken,
		ExpiresAt:   result.ExpiresAt,
		Account:     result.Account,
	}, nil
}

// CurrentAccount 验证 Session 并读取当前账号。
func (c Client) CurrentAccount(ctx context.Context, session Session) (Account, error) {
	endpoint, err := NormalizeEndpoint(session.Endpoint)
	if err != nil {
		return Account{}, err
	}
	if session.AccessToken == "" {
		return Account{}, fmt.Errorf("本地登录凭据缺少 access token")
	}
	var result currentAccount
	if err := c.request(ctx, http.MethodGet, endpoint+"/cloud-api/usercenter/v1/me", session.AccessToken, nil, &result); err != nil {
		return Account{}, fmt.Errorf("验证 Sparq Cloud 登录态失败: %w", err)
	}
	return result.Account, nil
}

// Logout 撤销当前远端用户 Session。
func (c Client) Logout(ctx context.Context, session Session) error {
	endpoint, err := NormalizeEndpoint(session.Endpoint)
	if err != nil {
		return err
	}
	if session.AccessToken == "" {
		return fmt.Errorf("本地登录凭据缺少 access token")
	}
	if err := c.request(ctx, http.MethodDelete, endpoint+"/cloud-api/usercenter/v1/session", session.AccessToken, nil, nil); err != nil {
		return fmt.Errorf("撤销 Sparq Cloud 登录态失败: %w", err)
	}
	return nil
}

func (c Client) request(ctx context.Context, method string, target string, token string, body []byte, output any) error {
	request, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "sparq-tool")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("读取认证接口响应失败: %w", err)
	}
	if len(data) > maxResponseBytes {
		return fmt.Errorf("认证接口响应超过 %d 字节限制", maxResponseBytes)
	}
	var envelope responseEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("解析认证接口响应失败: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices || envelope.Code != 0 {
		return &APIError{Status: response.StatusCode, Code: envelope.Code, Message: envelope.Message}
	}
	if output == nil || len(envelope.Data) == 0 || bytes.Equal(envelope.Data, []byte("null")) {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, output); err != nil {
		return fmt.Errorf("解析认证接口数据失败: %w", err)
	}
	return nil
}

// NormalizeEndpoint 校验并规范化 Sparq Cloud 基础地址。
func NormalizeEndpoint(endpoint string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", fmt.Errorf("Sparq Cloud endpoint 不能为空")
	}
	parsed, err := url.ParseRequestURI(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("Sparq Cloud endpoint 必须是有效的 HTTP(S) 地址")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("Sparq Cloud endpoint 不能包含用户信息、查询参数或 fragment")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}
