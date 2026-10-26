package devapp

import (
	"fmt"
	"net/url"
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
	Admin            *FrontendPlan
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

// FrontendPlan 保存 Shell 或业务前端各自的进程、就绪检查与退出策略。
type FrontendPlan struct {
	Name, ConfigKey, Mode string
	Command               string
	Args                  []string
	CWD, URL, Health      string
	Env                   []string
	Timeout, Grace, Kill  time.Duration
	durationErrors        map[string]error
}

// Frontends 按 Shell、业务前端顺序返回本次会话需要启动的前端。
func (p Plan) Frontends() []FrontendPlan {
	var plans []FrontendPlan
	if p.Admin != nil && (p.Admin.Mode == appconfig.FrontendModeSource || p.Admin.Mode == appconfig.FrontendModeSourceDev) {
		plans = append(plans, *p.Admin)
	}
	if p.FrontendEnabled {
		plans = append(plans, FrontendPlan{
			Name: "应用前端", ConfigKey: "frontend", Mode: p.FrontendMode,
			Command: p.FrontendCommand, Args: p.FrontendArgs, CWD: p.FrontendCWD,
			URL: p.FrontendURL, Health: p.FrontendHealth, Env: p.FrontendEnv,
			Timeout: p.FrontendTimeout, Grace: p.FrontendGrace, Kill: p.FrontendKill,
			durationErrors: p.durationErrors,
		})
	}
	return plans
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
	plan := Plan{
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
	if config.Admin != nil {
		admin := config.Admin
		mode := admin.Mode
		if mode == "" {
			mode = appconfig.FrontendModeSource
		}
		command, args := resolveFrontendCommand(admin.Command, resolvedToolchain.Frontend)
		adminDurations := map[string]durationResult{
			"admin.timeout":            parseDuration(admin.Timeout, 30*time.Second),
			"admin.stop.grace_timeout": parseDuration(admin.Stop.GraceTimeout, 3*time.Second),
			"admin.stop.kill_timeout":  parseDuration(admin.Stop.KillTimeout, 2*time.Second),
		}
		adminErrors := map[string]error{}
		for field, result := range adminDurations {
			if result.err != nil {
				adminErrors[field] = result.err
			}
		}
		env := mapEnvironment(admin.Env)
		if _, overridden := admin.Env["VITE_SPARQ_API_PROXY_TARGET"]; !overridden {
			target := admin.APIProxyTarget
			if target == "" {
				parsed, err := url.Parse(backend.HealthURL)
				if err == nil {
					target = parsed.Scheme + "://" + parsed.Host
				}
			}
			env = append(env, "VITE_SPARQ_API_PROXY_TARGET="+target)
		}
		plan.Admin = &FrontendPlan{
			Name: "Admin Shell", ConfigKey: "admin", Mode: mode,
			Command: command, Args: args, CWD: resolvePath(baseDir, admin.CWD),
			URL: admin.URL, Health: admin.Health, Env: env,
			Timeout:        adminDurations["admin.timeout"].value,
			Grace:          adminDurations["admin.stop.grace_timeout"].value,
			Kill:           adminDurations["admin.stop.kill_timeout"].value,
			durationErrors: adminErrors,
		}
	}
	return plan
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
	if p.Admin != nil {
		switch p.Admin.Mode {
		case appconfig.FrontendModeSource, appconfig.FrontendModeSourceDev, appconfig.FrontendModeArtifact, appconfig.FrontendModeDisabled:
		default:
			return fmt.Errorf("admin.mode 不支持开发启动: %s", p.Admin.Mode)
		}
	}
	for _, frontend := range p.Frontends() {
		if err := frontend.validate(); err != nil {
			return err
		}
	}
	ports := map[string]string{}
	for _, target := range append([]FrontendPlan{{Name: "应用后端", URL: p.Backend.HealthURL}}, p.Frontends()...) {
		parsed, _ := url.Parse(target.URL)
		port := parsed.Port()
		if port == "" {
			port = map[string]string{"http": "80", "https": "443"}[parsed.Scheme]
		}
		if owner, exists := ports[port]; exists {
			return fmt.Errorf("%s 与 %s 的开发进程端口冲突: %s", target.Name, owner, port)
		}
		ports[port] = target.Name
	}
	return nil
}

func (p FrontendPlan) validate() error {
	if strings.TrimSpace(p.Command) == "" {
		return fmt.Errorf("%s.command 不能为空", p.ConfigKey)
	}
	if strings.TrimSpace(p.URL) == "" {
		return fmt.Errorf("%s.url 不能为空", p.ConfigKey)
	}
	for _, field := range []string{
		p.ConfigKey + ".timeout",
		p.ConfigKey + ".stop.grace_timeout",
		p.ConfigKey + ".stop.kill_timeout",
	} {
		if err := p.durationErrors[field]; err != nil {
			return fmt.Errorf("%s 无效: %w", field, err)
		}
	}
	info, err := os.Stat(p.CWD)
	if err != nil {
		return fmt.Errorf("读取%s工作目录 %s 失败: %w", p.Name, p.CWD, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s工作路径不是目录: %s", p.Name, p.CWD)
	}
	if err := (watch.HTTPChecker{
		URL: p.URL, Path: p.Health, Timeout: p.Timeout,
	}).Validate(); err != nil {
		return fmt.Errorf("%s就绪检查无效: %w", p.Name, err)
	}
	if p.ConfigKey == "admin" {
		for _, entry := range p.Env {
			if target, ok := strings.CutPrefix(entry, "VITE_SPARQ_API_PROXY_TARGET="); ok {
				if err := (watch.HTTPChecker{URL: target, Timeout: p.Timeout}).Validate(); err != nil {
					return fmt.Errorf("admin.api_proxy_target 无效: %w", err)
				}
			}
		}
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
