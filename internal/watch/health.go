package watch

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPChecker 等待普通 HTTP 接口返回 2xx，适用于不需要 Sparq 签名的独立 watch 会话。
type HTTPChecker struct {
	URL     string
	Path    string
	Timeout time.Duration
}

// Validate 校验 HTTP 就绪检查地址和超时。
func (c HTTPChecker) Validate() error {
	if _, err := healthURL(c.URL, c.Path); err != nil {
		return err
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("health.timeout 必须大于 0")
	}
	return nil
}

// Wait 重试 HTTP 就绪检查，同时在运行进程提前退出时立即返回。
func (c HTTPChecker) Wait(ctx context.Context, process *Process) error {
	target, err := healthURL(c.URL, c.Path)
	if err != nil {
		return err
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	healthContext, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- waitHTTP(healthContext, target, timeout)
	}()
	select {
	case err := <-result:
		if err != nil {
			return err
		}
		select {
		case <-process.Done():
			return ExitError(process)
		default:
			return nil
		}
	case <-process.Done():
		return ExitError(process)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func waitHTTP(ctx context.Context, target string, timeout time.Duration) error {
	client := &http.Client{Timeout: 2 * time.Second}
	timeoutContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		request, err := http.NewRequestWithContext(timeoutContext, http.MethodGet, target, nil)
		if err != nil {
			return err
		}
		request.Header.Set("Accept", "*/*")
		response, err := client.Do(request)
		if err == nil {
			response.Body.Close()
			if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
				return nil
			}
			err = fmt.Errorf("就绪检查 %s 返回 HTTP %d", target, response.StatusCode)
		}
		lastErr = err
		select {
		case <-timeoutContext.Done():
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return lastErr
		case <-ticker.C:
		}
	}
}

func healthURL(endpoint, path string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(endpoint, "/"))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("health.url 必须是有效 HTTP(S) 地址: %s", endpoint)
	}
	if path != "" {
		parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + strings.TrimLeft(path, "/")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}
