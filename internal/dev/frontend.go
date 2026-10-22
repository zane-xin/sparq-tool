package dev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

type devTokenRequest struct {
	Actor       string   `json:"actor"`
	Permissions []string `json:"permissions"`
	Plugin      string   `json:"plugin"`
}

type devTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

type devTokenRevokeRequest struct {
	Token string `json:"token"`
}

type devFrontendRequest struct {
	Token   string `json:"token"`
	Plugin  string `json:"plugin"`
	RootURL string `json:"root_url"`
}

func requestDevToken(ctx context.Context, endpoint, devSecret, pluginID string) (devTokenResponse, error) {
	target, err := endpointURL(endpoint, "/dev/auth/token")
	if err != nil {
		return devTokenResponse{}, err
	}
	body, err := json.Marshal(devTokenRequest{Actor: "admin", Permissions: []string{"dev.frontend"}, Plugin: pluginID})
	if err != nil {
		return devTokenResponse{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return devTokenResponse{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+devSecret)
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		return devTokenResponse{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return devTokenResponse{}, fmt.Errorf("开发态 token 接口返回 HTTP %d", response.StatusCode)
	}
	var token devTokenResponse
	if err := json.NewDecoder(response.Body).Decode(&token); err != nil {
		return devTokenResponse{}, err
	}
	if token.Token == "" || token.ExpiresAt == "" {
		return devTokenResponse{}, errors.New("开发态 token 响应缺少 token 或 expires_at")
	}
	return token, nil
}

func registerDevFrontend(ctx context.Context, endpoint, token, pluginID, rootURL string) error {
	target, err := endpointURL(endpoint, "/dev/frontend")
	if err != nil {
		return err
	}
	body, err := json.Marshal(devFrontendRequest{Token: token, Plugin: pluginID, RootURL: rootURL})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("开发前端覆盖接口返回 HTTP %d", response.StatusCode)
	}
	return nil
}

func devFrontendHeartbeatInterval(token devTokenResponse, now time.Time) (time.Duration, error) {
	expiresAt, err := time.Parse(time.RFC3339, token.ExpiresAt)
	if err != nil {
		return 0, fmt.Errorf("解析开发态 token 过期时间失败: %w", err)
	}
	remaining := expiresAt.Sub(now)
	if remaining <= 0 {
		return 0, errors.New("开发态 token 已过期")
	}
	return remaining / 2, nil
}

func keepDevFrontendRegistered(
	ctx context.Context,
	endpoint string,
	token string,
	pluginID string,
	rootURL string,
	interval time.Duration,
) <-chan error {
	errors := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := registerDevFrontend(ctx, endpoint, token, pluginID, rootURL); err != nil {
					errors <- fmt.Errorf("续租插件开发前端入口失败: %w", err)
					return
				}
			}
		}
	}()
	return errors
}

func (p Plan) frontendRootURL() string {
	endpoint := strings.TrimRight(p.FrontendURL, "/")
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || strings.HasSuffix(parsed.Path, ".ts") || strings.HasSuffix(parsed.Path, ".mjs") {
		return endpoint
	}
	parsed.Path = path.Join(parsed.Path, "src/frontend.ts")
	return parsed.String()
}

func revokeDevToken(ctx context.Context, endpoint, token string) error {
	target, err := endpointURL(endpoint, "/dev/auth/revoke")
	if err != nil {
		return err
	}
	body, err := json.Marshal(devTokenRevokeRequest{Token: token})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("开发态 token 撤销接口返回 HTTP %d", response.StatusCode)
	}
	return nil
}
