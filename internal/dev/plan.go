package dev

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zane-xin/sparq-tool/internal/plugin/manifest"
)

// Options 描述 dev 命令参数。
type Options struct {
	Manifest        string
	AppCommand      string
	PluginCommand   string
	FrontendCommand string
}

// Plan 描述本地开发联调执行计划。
type Plan struct {
	BaseDir               string
	AppEndpoint           string
	AppHealthPath         string
	AppCommand            string
	AppCWD                string
	AppDevSecret          string
	PluginID              string
	PluginComponent       string
	ManifestPath          string
	LifecycleSessionID    string
	LifecycleSourceDigest string
	PluginEndpoint        string
	PluginHealthPath      string
	PluginCommand         string
	PluginCWD             string
	PluginSecret          string
	PluginEnv             map[string]string
	ReloadEnabled         bool
	ReloadPaths           []string
	ReloadExtensions      []string
	ReloadFiles           []string
	ReloadExclude         []string
	ReloadBuild           string
	ReloadDebounce        durationValue
	FrontendEnabled       bool
	FrontendCommand       string
	FrontendCWD           string
	FrontendURL           string
	FrontendHealthPath    string
	FrontendEnv           map[string]string
	AppTimeout            durationValue
	PluginTimeout         durationValue
	FrontendTimeout       durationValue
}

// NewPlan 合并配置文件和命令行覆盖参数。
func NewPlan(config Config, options Options) Plan {
	plan := Plan{
		BaseDir:            config.baseDir,
		AppEndpoint:        config.App.Endpoint,
		AppHealthPath:      defaultString(config.App.Health, "/__healthz__"),
		AppCommand:         config.App.Command,
		AppCWD:             config.App.CWD,
		AppDevSecret:       config.App.DevSecret,
		PluginID:           config.Plugin.ID,
		PluginComponent:    config.Plugin.Component,
		ManifestPath:       config.Plugin.Manifest,
		PluginEndpoint:     config.Plugin.Endpoint,
		PluginHealthPath:   defaultString(config.Plugin.Health, "/healthz"),
		PluginCommand:      config.Plugin.Command,
		PluginCWD:          config.Plugin.CWD,
		PluginSecret:       config.Plugin.Secret,
		PluginEnv:          cloneEnv(config.Plugin.Env),
		ReloadEnabled:      config.Plugin.Reload.Enabled,
		ReloadExtensions:   cloneStrings(config.Plugin.Reload.Extensions),
		ReloadFiles:        cloneStrings(config.Plugin.Reload.Files),
		ReloadExclude:      cloneStrings(config.Plugin.Reload.Exclude),
		ReloadBuild:        config.Plugin.Reload.Build,
		ReloadDebounce:     newDuration(config.Plugin.Reload.Debounce, 300*time.Millisecond),
		FrontendEnabled:    config.Frontend.Enabled || config.Frontend.Command != "",
		FrontendCommand:    config.Frontend.Command,
		FrontendCWD:        defaultString(config.Frontend.CWD, "frontend"),
		FrontendURL:        config.Frontend.URL,
		FrontendHealthPath: config.Frontend.Health,
		FrontendEnv:        cloneEnv(config.Frontend.Env),
		AppTimeout:         newDuration(config.App.Timeout, 30*time.Second),
		PluginTimeout:      newDuration(config.Plugin.Timeout, 30*time.Second),
		FrontendTimeout:    newDuration(config.Frontend.Timeout, 15*time.Second),
	}
	if strings.TrimSpace(options.Manifest) != "" {
		plan.ManifestPath = strings.TrimSpace(options.Manifest)
	}
	if strings.TrimSpace(options.AppCommand) != "" {
		plan.AppCommand = strings.TrimSpace(options.AppCommand)
	}
	if strings.TrimSpace(options.PluginCommand) != "" {
		plan.PluginCommand = strings.TrimSpace(options.PluginCommand)
	}
	if strings.TrimSpace(options.FrontendCommand) != "" {
		plan.FrontendEnabled = true
		plan.FrontendCommand = strings.TrimSpace(options.FrontendCommand)
	}
	plan.BaseDir = defaultString(plan.BaseDir, ".")
	plan.ManifestPath = defaultString(plan.ManifestPath, "plugin.yaml")
	plan.ManifestPath = resolveOptionalPath(plan.BaseDir, plan.ManifestPath)
	plan.AppCWD = resolvePath(plan.BaseDir, plan.AppCWD)
	plan.PluginCWD = resolvePath(plan.BaseDir, plan.PluginCWD)
	plan.FrontendCWD = resolvePath(plan.BaseDir, plan.FrontendCWD)
	plan.discoverConventionalFrontendRoot()
	if plan.ReloadEnabled {
		plan.ReloadPaths = resolveReloadPaths(plan.PluginCWD, config.Plugin.Reload.Paths)
		plan.ReloadExtensions = normalizeExtensions(defaultStrings(plan.ReloadExtensions, []string{".go"}))
		plan.ReloadFiles = defaultStrings(plan.ReloadFiles, []string{"go.mod", "go.sum"})
		plan.ReloadExclude = defaultStrings(plan.ReloadExclude, []string{".git", "frontend", "storage"})
		plan.ReloadBuild = defaultString(plan.ReloadBuild, "go build ./...")
	}
	return plan
}

func (p *Plan) discoverConventionalFrontendRoot() {
	if !isRegularFile(filepath.Join(p.FrontendCWD, "src", "frontend.ts")) {
		return
	}
	p.FrontendEnabled = true
	p.FrontendCommand = defaultString(p.FrontendCommand, "pnpm dev")
	p.FrontendURL = defaultString(p.FrontendURL, "http://127.0.0.1:5175/plugins/"+p.PluginID)
	p.FrontendHealthPath = defaultString(p.FrontendHealthPath, "/src/frontend.ts")
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// Validate 校验执行计划及其开发边界。
func (p Plan) Validate() error {
	if strings.TrimSpace(p.PluginID) == "" {
		return fmt.Errorf("plugin.id 不能为空")
	}
	if strings.TrimSpace(p.AppEndpoint) == "" {
		return fmt.Errorf("app.endpoint 不能为空")
	}
	if strings.TrimSpace(p.PluginEndpoint) == "" {
		return fmt.Errorf("plugin.endpoint 不能为空")
	}
	if strings.TrimSpace(p.PluginCommand) == "" {
		return fmt.Errorf("plugin.command 不能为空")
	}
	if strings.TrimSpace(p.PluginSecret) == "" {
		return fmt.Errorf("plugin.secret 不能为空")
	}
	if len(p.PluginSecret) < 32 {
		return fmt.Errorf("plugin.secret 至少需要 32 字节")
	}
	if p.FrontendEnabled && len(p.AppDevSecret) < 32 {
		return fmt.Errorf("frontend.enabled=true 时 app.dev_secret 至少需要 32 字节")
	}
	for name, timeout := range map[string]durationValue{
		"app.timeout":      p.AppTimeout,
		"plugin.timeout":   p.PluginTimeout,
		"frontend.timeout": p.FrontendTimeout,
	} {
		if timeout.err != nil {
			return fmt.Errorf("%s 无效: %w", name, timeout.err)
		}
	}
	if p.ReloadEnabled {
		if p.ReloadDebounce.err != nil {
			return fmt.Errorf("plugin.reload.debounce 无效: %w", p.ReloadDebounce.err)
		}
		if strings.TrimSpace(p.ReloadBuild) == "" {
			return fmt.Errorf("plugin.reload.enabled=true 时 plugin.reload.build 不能为空")
		}
		if len(p.ReloadPaths) == 0 {
			return fmt.Errorf("plugin.reload.enabled=true 时至少需要一个监听目录")
		}
		if len(p.ReloadExtensions) == 0 && len(p.ReloadFiles) == 0 {
			return fmt.Errorf("plugin.reload.enabled=true 时 extensions 和 files 不能同时为空")
		}
		for _, path := range p.ReloadPaths {
			info, err := os.Stat(path)
			if err != nil {
				return fmt.Errorf("读取插件热更新监听目录 %s 失败: %w", path, err)
			}
			if !info.IsDir() {
				return fmt.Errorf("插件热更新监听路径不是目录: %s", path)
			}
		}
	}
	if p.FrontendEnabled && strings.TrimSpace(p.FrontendCommand) == "" {
		return fmt.Errorf("frontend.enabled=true 时 frontend.command 不能为空")
	}
	if p.FrontendEnabled && !isRegularFile(filepath.Join(p.FrontendCWD, "src", "frontend.ts")) {
		return fmt.Errorf("frontend.enabled=true 时必须存在唯一入口 %s", filepath.Join(p.FrontendCWD, "src", "frontend.ts"))
	}
	if _, err := endpointURL(p.AppEndpoint, p.AppHealthPath); err != nil {
		return fmt.Errorf("app.endpoint 无效: %w", err)
	}
	if _, err := endpointURL(p.PluginEndpoint, p.PluginHealthPath); err != nil {
		return fmt.Errorf("plugin.endpoint 无效: %w", err)
	}
	if _, err := os.Stat(p.ManifestPath); err != nil {
		return fmt.Errorf("插件清单不存在 %s: %w", p.ManifestPath, err)
	}
	item, err := manifest.Load(p.ManifestPath)
	if err != nil {
		return fmt.Errorf("校验插件清单失败: %w", err)
	}
	if item.Metadata.Code != p.PluginID {
		return fmt.Errorf("插件清单 code %q 与 plugin.id %q 不一致", item.Metadata.Code, p.PluginID)
	}
	if p.FrontendEnabled {
		usesManifestModule, err := manifest.UsesManifestModule(p.ManifestPath)
		if err != nil {
			return fmt.Errorf("检查插件 Manifest 组合模块失败: %w", err)
		}
		if item.Spec.Frontend == nil && !usesManifestModule {
			return fmt.Errorf("frontend.enabled=true 时插件清单必须声明 trusted frontend root")
		}
		if p.FrontendURL == "" {
			return fmt.Errorf("frontend.enabled=true 时没有可登记的 frontend root URL")
		}
		if p.FrontendURL != "" {
			if _, err := httpEndpointURL(p.FrontendURL, ""); err != nil {
				return fmt.Errorf("frontend.url 无效: %w", err)
			}
			if _, err := httpEndpointURL(p.FrontendURL, p.FrontendHealthPath); err != nil {
				return fmt.Errorf("frontend.health 无效: %w", err)
			}
		} else if p.FrontendHealthPath != "" {
			return fmt.Errorf("frontend.health 依赖 frontend.url")
		}
	}
	return nil
}
