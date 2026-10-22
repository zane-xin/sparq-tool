package devapp

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/zane-xin/sparq-tool/internal/appconfig"
	"github.com/zane-xin/sparq-tool/internal/toolchain"
	"github.com/zane-xin/sparq-tool/internal/watch"
)

// Plan 是完成默认值和路径解析后的应用联调执行计划。
type Plan struct {
	BackendWatchFile string
	Backend          watch.Plan
	FrontendCommand  string
	FrontendArgs     []string
	FrontendCWD      string
	FrontendURL      string
	FrontendHealth   string
	FrontendEnv      []string
	FrontendTimeout  time.Duration
	FrontendGrace    time.Duration
	FrontendKill     time.Duration
	FrontendEnabled  bool
	FrontendMode     string
	durationErrors   map[string]error
}

// LoadPlan 读取应用联调配置及其引用的后端 watch 配置。
func LoadPlan(path string) (Plan, error) {
	config, err := LoadConfig(path)
	if err != nil {
		return Plan{}, err
	}
	if err := config.ValidateCommon(); err != nil {
		return Plan{}, err
	}
	if config.FrontendMode() == appconfig.FrontendModeSourceBuild {
		return Plan{}, fmt.Errorf("frontend.mode=source-build 仅适用于构建命令；需要同一配置同时支持构建和开发时请使用 source")
	}
	if strings.TrimSpace(config.Backend.WatchFile) == "" {
		return Plan{}, fmt.Errorf("backend.watch_file 不能为空")
	}
	watchFile := resolvePath(config.BaseDir(), config.Backend.WatchFile)
	watchConfig, err := watch.LoadConfig(watchFile)
	if err != nil {
		return Plan{}, fmt.Errorf("读取应用后端 watch 配置 %s 失败: %w", watchFile, err)
	}
	userToolchain, _, err := toolchain.LoadUser()
	if err != nil {
		return Plan{}, err
	}
	config.Toolchain = toolchain.Merge(userToolchain, config.Toolchain)
	return NewPlan(config, watch.NewPlan(watchConfig)), nil
}

// NewPlan 合并应用配置、后端 watch 计划和开发默认值。
func NewPlan(config Config, backend watch.Plan) Plan {
	resolvedToolchain := toolchain.Merge(toolchain.Defaults(), config.Toolchain)
	frontendCommand, frontendArgs := resolveFrontendCommand(config.Frontend.Command, resolvedToolchain.Frontend)
	frontendMode := config.FrontendMode()
	frontendEnabled := frontendMode == appconfig.FrontendModeSource || frontendMode == appconfig.FrontendModeSourceDev
	if frontendEnabled && config.Frontend.URL != "" {
		backend.RunEnv = append(backend.RunEnv, "SPARQ_DEV_FRONTEND_URL="+config.Frontend.URL)
	}
	durations := map[string]durationResult{
		"frontend.timeout":            parseDuration(config.Frontend.Timeout, 30*time.Second),
		"frontend.stop.grace_timeout": parseDuration(config.Frontend.Stop.GraceTimeout, 3*time.Second),
		"frontend.stop.kill_timeout":  parseDuration(config.Frontend.Stop.KillTimeout, 2*time.Second),
	}
	durationErrors := make(map[string]error)
	for field, result := range durations {
		if result.err != nil {
			durationErrors[field] = result.err
		}
	}
	baseDir := config.BaseDir()
	return Plan{
		BackendWatchFile: resolvePath(baseDir, config.Backend.WatchFile),
		Backend:          backend,
		FrontendCommand:  frontendCommand,
		FrontendArgs:     frontendArgs,
		FrontendCWD:      resolvePath(baseDir, config.Frontend.CWD),
		FrontendURL:      config.Frontend.URL,
		FrontendHealth:   config.Frontend.Health,
		FrontendEnv:      mapEnvironment(config.Frontend.Env),
		FrontendTimeout:  durations["frontend.timeout"].value,
		FrontendGrace:    durations["frontend.stop.grace_timeout"].value,
		FrontendKill:     durations["frontend.stop.kill_timeout"].value,
		FrontendEnabled:  frontendEnabled,
		FrontendMode:     frontendMode,
		durationErrors:   durationErrors,
	}
}

func resolveFrontendCommand(raw string, spec toolchain.Spec) (string, []string) {
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return "", nil
	}
	if spec.Command == "" {
		return parts[0], parts[1:]
	}
	args := append([]string{}, spec.Args...)
	if parts[0] == spec.Command || parts[0] == filepath.Base(spec.Command) {
		parts = parts[1:]
		// 完整包装命令已经写出固定参数时，只保留工具链中的一份。
		if len(spec.Args) > 0 && len(parts) >= len(spec.Args) && slices.Equal(parts[:len(spec.Args)], spec.Args) {
			parts = parts[len(spec.Args):]
		}
	} else if parts[0] == "pnpm" || (len(spec.Args) > 0 && parts[0] == spec.Args[len(spec.Args)-1]) {
		parts = parts[1:]
	}
	args = append(args, parts...)
	return spec.Command, args
}

// Validate 校验应用前后端命令、路径和就绪检查。
func (p Plan) Validate() error {
	if p.FrontendMode != appconfig.FrontendModeSource && p.FrontendMode != appconfig.FrontendModeSourceDev && p.FrontendMode != appconfig.FrontendModeArtifact && p.FrontendMode != appconfig.FrontendModeDisabled {
		return fmt.Errorf("frontend.mode 不支持: %s", p.FrontendMode)
	}
	if strings.TrimSpace(p.BackendWatchFile) == "" {
		return fmt.Errorf("backend.watch_file 不能为空")
	}
	if err := p.Backend.Validate(); err != nil {
		return fmt.Errorf("应用后端 watch 配置无效: %w", err)
	}
	if p.Backend.HealthURL == "" {
		return fmt.Errorf("应用后端 watch 配置必须提供 health.url")
	}
	if !p.FrontendEnabled {
		return nil
	}
	if strings.TrimSpace(p.FrontendCommand) == "" {
		return fmt.Errorf("frontend.command 不能为空")
	}
	if strings.TrimSpace(p.FrontendURL) == "" {
		return fmt.Errorf("frontend.url 不能为空")
	}
	for _, field := range []string{
		"frontend.timeout",
		"frontend.stop.grace_timeout",
		"frontend.stop.kill_timeout",
	} {
		if err := p.durationErrors[field]; err != nil {
			return fmt.Errorf("%s 无效: %w", field, err)
		}
	}
	info, err := os.Stat(p.FrontendCWD)
	if err != nil {
		return fmt.Errorf("读取应用前端工作目录 %s 失败: %w", p.FrontendCWD, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("应用前端工作路径不是目录: %s", p.FrontendCWD)
	}
	if err := (watch.HTTPChecker{
		URL: p.FrontendURL, Path: p.FrontendHealth, Timeout: p.FrontendTimeout,
	}).Validate(); err != nil {
		return fmt.Errorf("应用前端就绪检查无效: %w", err)
	}
	return nil
}

type durationResult struct {
	value time.Duration
	err   error
}

func parseDuration(raw string, fallback time.Duration) durationResult {
	if strings.TrimSpace(raw) == "" {
		return durationResult{value: fallback}
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		if err == nil {
			err = fmt.Errorf("必须是正 duration")
		}
		return durationResult{value: fallback, err: err}
	}
	return durationResult{value: value}
}

func resolvePath(baseDir, path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(baseDir, path)
}

func mapEnvironment(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}
