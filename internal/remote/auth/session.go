package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/zane-xin/sparq-tool/internal/userpath"
)

const (
	DefaultProfile           = "default"
	DefaultConfigPathDisplay = userpath.DefaultToolDirDisplay + "/auth.json"
)

var profilePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// Session 是一个 endpoint 下的 Sparq Cloud 用户登录态。
type Session struct {
	Endpoint    string  `json:"endpoint"`
	AccessToken string  `json:"access_token"`
	ExpiresAt   int64   `json:"expires_at"`
	Account     Account `json:"account"`
	LoggedAt    string  `json:"logged_at"`
}

// Config 保存用户级认证 profile，不属于任何插件目录。
type Config struct {
	CurrentProfile string             `json:"current_profile"`
	Profiles       map[string]Session `json:"profiles"`
}

// SaveSession 保存并选中指定 profile。
func SaveSession(configPath string, profile string, session Session) (string, error) {
	profile, err := validateProfile(profile)
	if err != nil {
		return "", err
	}
	config, path, err := loadForUpdate(configPath)
	if err != nil {
		return "", err
	}
	if config.Profiles == nil {
		config.Profiles = make(map[string]Session)
	}
	session.LoggedAt = time.Now().UTC().Format(time.RFC3339)
	config.Profiles[profile] = session
	config.CurrentProfile = profile
	if err := writeConfig(path, config); err != nil {
		return "", err
	}
	return path, nil
}

// ResolveSession 读取显式 profile，未指定时读取当前 profile。
func ResolveSession(configPath string, profile string) (Session, string, string, error) {
	config, path, err := Load(configPath)
	if err != nil {
		return Session{}, "", path, err
	}
	selected := strings.TrimSpace(profile)
	if selected == "" {
		selected = config.CurrentProfile
	}
	selected, err = validateProfile(selected)
	if err != nil {
		return Session{}, "", path, err
	}
	session, exists := config.Profiles[selected]
	if !exists {
		return Session{}, selected, path, fmt.Errorf("认证 profile %q 尚未登录", selected)
	}
	return session, selected, path, nil
}

// RemoveSession 删除指定 profile；未指定时删除当前 profile。
func RemoveSession(configPath string, profile string) (string, string, error) {
	config, path, err := Load(configPath)
	if err != nil {
		return "", path, err
	}
	selected := strings.TrimSpace(profile)
	if selected == "" {
		selected = config.CurrentProfile
	}
	selected, err = validateProfile(selected)
	if err != nil {
		return "", path, err
	}
	if _, exists := config.Profiles[selected]; !exists {
		return "", path, fmt.Errorf("认证 profile %q 尚未登录", selected)
	}
	delete(config.Profiles, selected)
	if len(config.Profiles) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return "", path, fmt.Errorf("删除认证配置 %s 失败: %w", path, err)
		}
		return selected, path, nil
	}
	if config.CurrentProfile == selected {
		config.CurrentProfile = firstProfile(config.Profiles)
	}
	if err := writeConfig(path, config); err != nil {
		return "", path, err
	}
	return selected, path, nil
}

// Load 读取认证配置。
func Load(configPath string) (Config, string, error) {
	path, err := resolveConfigPath(configPath)
	if err != nil {
		return Config{}, "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, path, fmt.Errorf("尚未登录 Sparq Cloud，请先运行 sparq-tool auth login")
		}
		return Config{}, path, fmt.Errorf("读取认证配置 %s 失败: %w", path, err)
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, path, fmt.Errorf("解析认证配置 %s 失败: %w", path, err)
	}
	if config.Profiles == nil {
		config.Profiles = make(map[string]Session)
	}
	return config, path, nil
}

func loadForUpdate(configPath string) (Config, string, error) {
	config, path, err := Load(configPath)
	if err == nil {
		return config, path, nil
	}
	resolved, resolveErr := resolveConfigPath(configPath)
	if resolveErr != nil {
		return Config{}, "", resolveErr
	}
	if _, statErr := os.Stat(resolved); errors.Is(statErr, os.ErrNotExist) {
		return Config{Profiles: make(map[string]Session)}, resolved, nil
	}
	return Config{}, path, err
}

func resolveConfigPath(configPath string) (string, error) {
	configPath = strings.TrimSpace(configPath)
	if configPath != "" {
		return filepath.Abs(configPath)
	}
	toolDir, err := userpath.ToolDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(toolDir, "auth.json"), nil
}

func writeConfig(path string, config Config) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("编码认证配置失败: %w", err)
	}
	directory := filepath.Dir(path)
	directoryCreated := false
	if _, err := os.Stat(directory); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("创建认证配置目录 %s 失败: %w", directory, err)
		}
		directoryCreated = true
	} else if err != nil {
		return fmt.Errorf("检查认证配置目录 %s 失败: %w", directory, err)
	}
	if directoryCreated {
		if err := os.Chmod(directory, 0o700); err != nil {
			return fmt.Errorf("设置认证配置目录权限失败: %w", err)
		}
	}
	temporary, err := os.CreateTemp(directory, ".auth-*.tmp")
	if err != nil {
		return fmt.Errorf("创建认证配置临时文件失败: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("设置认证配置文件权限失败: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("写入认证配置失败: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("同步认证配置失败: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("关闭认证配置失败: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("替换认证配置 %s 失败: %w", path, err)
	}
	return nil
}

func validateProfile(profile string) (string, error) {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		profile = DefaultProfile
	}
	if !profilePattern.MatchString(profile) {
		return "", fmt.Errorf("认证 profile 只能包含字母、数字、点、下划线和连字符，且不能超过 64 个字符")
	}
	return profile, nil
}

func firstProfile(profiles map[string]Session) string {
	var selected string
	for profile := range profiles {
		if selected == "" || profile < selected {
			selected = profile
		}
	}
	return selected
}
