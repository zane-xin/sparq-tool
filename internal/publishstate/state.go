package publishstate

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zane-xin/sparq-tool/internal/userpath"
)

const directoryName = "publish-state"

// State 只保存恢复一次发布所需的元数据，不保存登录凭据或候选包内容。
type State struct {
	Kind              string `json:"kind"`
	SourceDir         string `json:"source_dir"`
	Endpoint          string `json:"endpoint"`
	Profile           string `json:"profile"`
	Code              string `json:"code,omitempty"`
	OriginalVersion   string `json:"original_version"`
	TargetVersion     string `json:"target_version"`
	IdempotencyKey    string `json:"idempotency_key"`
	SourceFingerprint string `json:"source_fingerprint,omitempty"`
	SourceSHA256      string `json:"source_sha256,omitempty"`
	SourceUploadID    uint64 `json:"source_upload_id,omitempty"`
	UpdatedAt         string `json:"updated_at"`
}

// NewAttemptKey 生成独立于包内容的发布尝试标识，源码变化时仍能明确识别旧会话。
func NewAttemptKey() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("生成发布幂等键失败: %w", err)
	}
	return "attempt:" + hex.EncodeToString(value), nil
}

// Load 按资源类型、Cloud profile 和源码目录读取未完成发布状态。
func Load(kind, endpoint, profile, sourceDir string) (State, bool, error) {
	identity, err := normalizeIdentity(kind, endpoint, profile, sourceDir)
	if err != nil {
		return State{}, false, err
	}
	path, err := statePath(identity)
	if err != nil {
		return State{}, false, err
	}
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return State{}, false, nil
	}
	if err != nil {
		return State{}, false, fmt.Errorf("读取发布恢复状态失败: %w", err)
	}
	var state State
	if err := json.Unmarshal(body, &state); err != nil {
		return State{}, false, fmt.Errorf("解析发布恢复状态失败: %w", err)
	}
	if err := validate(state); err != nil {
		return State{}, false, err
	}
	if state.Kind != identity.Kind || state.Endpoint != identity.Endpoint || state.Profile != identity.Profile || state.SourceDir != identity.SourceDir {
		return State{}, false, fmt.Errorf("发布恢复状态与当前发布目标不一致")
	}
	return state, true, nil
}

// Save 使用私有文件权限原子保存发布状态。
func Save(state State) error {
	identity, err := normalizeIdentity(state.Kind, state.Endpoint, state.Profile, state.SourceDir)
	if err != nil {
		return err
	}
	state.Kind = identity.Kind
	state.Endpoint = identity.Endpoint
	state.Profile = identity.Profile
	state.SourceDir = identity.SourceDir
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := validate(state); err != nil {
		return err
	}
	directory, err := stateDirectory()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("创建发布恢复状态目录失败: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("设置发布恢复状态目录权限失败: %w", err)
	}
	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("编码发布恢复状态失败: %w", err)
	}
	body = append(body, '\n')
	temporary, err := os.CreateTemp(directory, ".publish-state-*")
	if err != nil {
		return fmt.Errorf("创建发布恢复状态临时文件失败: %w", err)
	}
	temporaryPath := temporary.Name()
	remove := true
	defer func() {
		_ = temporary.Close()
		if remove {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("设置发布恢复状态文件权限失败: %w", err)
	}
	if _, err := temporary.Write(body); err != nil {
		return fmt.Errorf("写入发布恢复状态失败: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("同步发布恢复状态失败: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("关闭发布恢复状态失败: %w", err)
	}
	target, err := statePath(identity)
	if err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		return fmt.Errorf("提交发布恢复状态失败: %w", err)
	}
	remove = false
	return nil
}

// Delete 在 finalize 成功后删除本地恢复状态。
func Delete(kind, endpoint, profile, sourceDir string) error {
	identity, err := normalizeIdentity(kind, endpoint, profile, sourceDir)
	if err != nil {
		return err
	}
	path, err := statePath(identity)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("清理发布恢复状态失败: %w", err)
	}
	return nil
}

func stateDirectory() (string, error) {
	toolDir, err := userpath.ToolDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(toolDir, directoryName), nil
}

func statePath(identity State) (string, error) {
	directory, err := stateDirectory()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{identity.Kind, identity.Endpoint, identity.Profile, identity.SourceDir}, "\x00")))
	return filepath.Join(directory, identity.Kind+"-"+hex.EncodeToString(digest[:])+".json"), nil
}

func normalizeIdentity(kind, endpoint, profile, sourceDir string) (State, error) {
	kind = strings.TrimSpace(kind)
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	profile = strings.TrimSpace(profile)
	if kind != "plugin" && kind != "theme" {
		return State{}, fmt.Errorf("发布恢复状态类型无效")
	}
	if endpoint == "" || profile == "" || strings.TrimSpace(sourceDir) == "" {
		return State{}, fmt.Errorf("发布恢复状态标识不完整")
	}
	absolute, err := filepath.Abs(sourceDir)
	if err != nil {
		return State{}, fmt.Errorf("解析发布源码目录失败: %w", err)
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return State{}, fmt.Errorf("解析发布源码目录失败: %w", err)
	}
	return State{Kind: kind, Endpoint: endpoint, Profile: profile, SourceDir: filepath.Clean(absolute)}, nil
}

func validate(state State) error {
	if state.Kind == "" || state.Endpoint == "" || state.Profile == "" || state.SourceDir == "" ||
		state.OriginalVersion == "" || state.TargetVersion == "" || state.IdempotencyKey == "" {
		return fmt.Errorf("发布恢复状态内容不完整")
	}
	if len(state.IdempotencyKey) > 128 {
		return fmt.Errorf("发布恢复状态幂等键无效")
	}
	return nil
}
