package dev

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type LifecycleSessionRequest struct {
	Actor          string `json:"actor"`
	Plugin         string `json:"plugin"`
	Component      string `json:"component"`
	SourceDigest   string `json:"source_digest"`
	ManifestDigest string `json:"manifest_digest"`
	// 空数组表示插件明确没有可选生命周期阶段，不能省略为旧客户端语义。
	Operations []string `json:"operations"`
	TTLSeconds int      `json:"ttl_seconds,omitempty"`
}

type LifecycleSession struct {
	SessionID      string   `json:"session_id"`
	Plugin         string   `json:"plugin"`
	Component      string   `json:"component"`
	SourceDigest   string   `json:"source_digest"`
	ManifestDigest string   `json:"manifest_digest"`
	Permissions    []string `json:"permissions"`
	ExpiresAt      string   `json:"expires_at"`
}

type LifecycleTestRequest struct {
	SessionID      string `json:"session_id"`
	OperationID    string `json:"operation_id"`
	IdempotencyKey string `json:"idempotency_key"`
	Plugin         string `json:"plugin"`
	Component      string `json:"component"`
	Phase          string `json:"phase"`
	SourceDigest   string `json:"source_digest"`
	ManifestDigest string `json:"manifest_digest"`
}

type LifecycleTestResult struct {
	OperationID string `json:"operation_id"`
	Phase       string `json:"phase"`
	Status      string `json:"status"`
	Message     string `json:"message,omitempty"`
}

type LifecycleRevokeRequest struct {
	SessionID string `json:"session_id"`
}

type LifecycleRenewRequest struct {
	SessionID  string `json:"session_id"`
	TTLSeconds int    `json:"ttl_seconds,omitempty"`
}

// RequestLifecycleSession asks the development host for a transient backend session.
func RequestLifecycleSession(ctx context.Context, endpoint, devSecret string, request LifecycleSessionRequest) (LifecycleSession, error) {
	var response LifecycleSession
	if err := postJSON(ctx, endpoint, "/dev/lifecycle/session", devSecret, request, &response, http.StatusOK); err != nil {
		return response, err
	}
	if response.SessionID == "" || response.ExpiresAt == "" {
		return LifecycleSession{}, fmt.Errorf("开发生命周期 session 响应缺少 session_id 或 expires_at")
	}
	return response, nil
}

// ExecuteLifecycleTest asks the host to execute one lifecycle phase.
func ExecuteLifecycleTest(ctx context.Context, endpoint string, request LifecycleTestRequest) (LifecycleTestResult, error) {
	var response LifecycleTestResult
	if err := postJSON(ctx, endpoint, "/dev/lifecycle/test", "", request, &response, http.StatusOK); err != nil {
		return response, err
	}
	return response, nil
}

// RevokeLifecycleSession invalidates a transient development session.
func RevokeLifecycleSession(ctx context.Context, endpoint string, request LifecycleRevokeRequest) error {
	return postJSON(ctx, endpoint, "/dev/lifecycle/session/revoke", "", request, nil, http.StatusNoContent)
}

// RenewLifecycleSession extends a transient development session.
func RenewLifecycleSession(ctx context.Context, endpoint string, request LifecycleRenewRequest) (LifecycleSession, error) {
	var response LifecycleSession
	if err := postJSON(ctx, endpoint, "/dev/lifecycle/session/renew", "", request, &response, http.StatusOK); err != nil {
		return response, err
	}
	return response, nil
}

func lifecycleSessionHeartbeatInterval(session LifecycleSession, now time.Time) (time.Duration, error) {
	expiresAt, err := time.Parse(time.RFC3339, session.ExpiresAt)
	if err != nil {
		return 0, fmt.Errorf("解析开发生命周期 session 过期时间失败: %w", err)
	}
	remaining := expiresAt.Sub(now)
	if remaining <= 0 {
		return 0, errors.New("开发生命周期 session 已过期")
	}
	return remaining / 2, nil
}

func keepLifecycleSessionAlive(ctx context.Context, endpoint string, session LifecycleSession, interval time.Duration) <-chan error {
	errors := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				refreshed, err := RenewLifecycleSession(ctx, endpoint, LifecycleRenewRequest{SessionID: session.SessionID})
				if err != nil {
					errors <- fmt.Errorf("续租开发生命周期 session 失败: %w", err)
					return
				}
				session = refreshed
				if interval, err = lifecycleSessionHeartbeatInterval(session, time.Now()); err != nil {
					errors <- err
					return
				}
				ticker.Reset(interval)
			}
		}
	}()
	return errors
}

// SourceDigest 计算本次开发编排实际监听源码集合的确定性摘要。
func SourceDigest(plan Plan) (string, error) {
	paths := plan.ReloadPaths
	extensions := plan.ReloadExtensions
	files := plan.ReloadFiles
	excludes := plan.ReloadExclude
	if !plan.ReloadEnabled {
		paths = []string{plan.PluginCWD}
		extensions = []string{".go", ".json", ".yaml", ".yml"}
		files = []string{"go.mod", "go.sum"}
		excludes = []string{".git", "frontend", "node_modules", "release", "storage", "vendor"}
	}
	extensionSet := stringSet(extensions)
	fileSet := stringSet(files)
	seen := map[string]struct{}{}
	entries := make([]sourceDigestEntry, 0)
	for _, root := range paths {
		root, err := filepath.Abs(root)
		if err != nil {
			return "", fmt.Errorf("解析源码摘要目录 %s 失败: %w", root, err)
		}
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if relative == "." {
				return nil
			}
			if sourceDigestExcluded(relative, excludes) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			if _, ok := fileSet[entry.Name()]; !ok {
				if _, ok := extensionSet[filepath.Ext(entry.Name())]; !ok {
					return nil
				}
			}
			absolute := filepath.Clean(path)
			if _, ok := seen[absolute]; ok {
				return nil
			}
			seen[absolute] = struct{}{}
			key, err := filepath.Rel(plan.BaseDir, absolute)
			if err != nil || strings.HasPrefix(key, ".."+string(filepath.Separator)) {
				key = absolute
			}
			entries = append(entries, sourceDigestEntry{key: filepath.ToSlash(key), path: absolute})
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("读取源码摘要目录 %s 失败: %w", root, err)
		}
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].key < entries[right].key })
	hash := sha256.New()
	for _, entry := range entries {
		_, _ = io.WriteString(hash, entry.key)
		_, _ = hash.Write([]byte{0})
		file, err := os.Open(entry.path)
		if err != nil {
			return "", fmt.Errorf("读取源码摘要文件 %s 失败: %w", entry.path, err)
		}
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return "", fmt.Errorf("计算源码摘要文件 %s 失败: %w", entry.path, copyErr)
		}
		if closeErr != nil {
			return "", fmt.Errorf("关闭源码摘要文件 %s 失败: %w", entry.path, closeErr)
		}
		_, _ = hash.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

type sourceDigestEntry struct {
	key  string
	path string
}

func sourceDigestExcluded(relative string, excludes []string) bool {
	relative = filepath.Clean(relative)
	for _, excluded := range excludes {
		excluded = filepath.Clean(excluded)
		if relative == excluded || strings.HasPrefix(relative, excluded+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func postJSON(ctx context.Context, endpoint, path, devSecret string, input, output any, expectedStatus int) error {
	target, err := endpointURL(endpoint, path)
	if err != nil {
		return err
	}
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if devSecret != "" {
		request.Header.Set("Authorization", "Bearer "+devSecret)
	}
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != expectedStatus {
		return fmt.Errorf("开发生命周期接口返回 HTTP %d", response.StatusCode)
	}
	if output != nil {
		if err := json.NewDecoder(response.Body).Decode(output); err != nil {
			return err
		}
	}
	return nil
}
