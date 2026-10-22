package dev

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/zane-xin/sparq-protocol/remoteauth"
	"github.com/zane-xin/sparq-tool/internal/watch"
)

func waitHealthWithExits(
	ctx context.Context,
	endpoint string,
	healthPath string,
	timeout time.Duration,
	exits <-chan processExit,
	signer *remoteauth.Signer,
) error {
	if exits == nil {
		return waitHealth(ctx, endpoint, healthPath, timeout, signer)
	}
	healthContext, cancel := context.WithCancel(ctx)
	defer cancel()
	healthResult := make(chan error, 1)
	go func() {
		healthResult <- waitHealth(healthContext, endpoint, healthPath, timeout, signer)
	}()
	select {
	case err := <-healthResult:
		return err
	case result := <-exits:
		return formatProcessExit(result)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func waitHealthWithProcess(
	ctx context.Context,
	endpoint string,
	healthPath string,
	timeout time.Duration,
	process *watch.Process,
	signer *remoteauth.Signer,
) error {
	healthContext, cancel := context.WithCancel(ctx)
	defer cancel()
	healthResult := make(chan error, 1)
	go func() {
		healthResult <- waitHealth(healthContext, endpoint, healthPath, timeout, signer)
	}()
	select {
	case err := <-healthResult:
		return err
	case <-process.Done():
		return formatProcessExit(processExit{process: process, err: process.Err()})
	case <-ctx.Done():
		return ctx.Err()
	}
}

func waitHealth(ctx context.Context, endpoint string, healthPath string, timeout time.Duration, signer *remoteauth.Signer) error {
	target, err := endpointURL(endpoint, healthPath)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		if err := probeHealth(ctx, client, target, signer); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return lastErr
		case <-ticker.C:
		}
	}
}

func probeHealth(ctx context.Context, client *http.Client, target string, signer *remoteauth.Signer) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "*/*")
	if signer != nil {
		if err := signer.Sign(request, nil); err != nil {
			return fmt.Errorf("签署插件健康检查请求失败: %w", err)
		}
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("健康检查 %s 返回 HTTP %d", response.Request.URL.String(), response.StatusCode)
	}
	return nil
}
