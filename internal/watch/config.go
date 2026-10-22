package watch

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 描述 sparq.watch.yaml 中独立文件监听会话。
type Config struct {
	CWD     string        `yaml:"cwd"`
	Build   CommandConfig `yaml:"build"`
	Run     CommandConfig `yaml:"run"`
	Watch   WatchConfig   `yaml:"watch"`
	Stop    StopConfig    `yaml:"stop"`
	Restart RestartConfig `yaml:"restart"`
	Health  HealthConfig  `yaml:"health"`
	baseDir string
}

// CommandConfig 描述构建或运行命令及其附加环境变量。
type CommandConfig struct {
	Command string            `yaml:"command"`
	Env     map[string]string `yaml:"env"`
}

// WatchConfig 描述独立命令的文件匹配规则和监听驱动。
type WatchConfig struct {
	Paths        []string `yaml:"paths"`
	Extensions   []string `yaml:"extensions"`
	Files        []string `yaml:"files"`
	Include      []string `yaml:"include"`
	Exclude      []string `yaml:"exclude"`
	ExcludeGlobs []string `yaml:"exclude_globs"`
	Mode         string   `yaml:"mode"`
	PollInterval string   `yaml:"poll_interval"`
	Debounce     string   `yaml:"debounce"`
}

// StopConfig 控制运行进程优雅退出和强制终止的等待时间。
type StopConfig struct {
	GraceTimeout string `yaml:"grace_timeout"`
	KillTimeout  string `yaml:"kill_timeout"`
}

// RestartConfig 控制运行进程异常退出后的重启和退避。
type RestartConfig struct {
	Policy       string `yaml:"policy"`
	InitialDelay string `yaml:"initial_delay"`
	MaxDelay     string `yaml:"max_delay"`
	ResetAfter   string `yaml:"reset_after"`
}

// HealthConfig 描述可选的普通 HTTP 就绪检查。
type HealthConfig struct {
	URL     string `yaml:"url"`
	Path    string `yaml:"path"`
	Timeout string `yaml:"timeout"`
}

// Plan 是完成默认值和路径解析后的 watch 执行计划。
type Plan struct {
	CWD                 string
	BuildCommand        string
	BuildEnv            []string
	RunCommand          string
	RunEnv              []string
	WatchPaths          []string
	WatchExtensions     []string
	WatchFiles          []string
	WatchInclude        []string
	WatchExclude        []string
	WatchExcludeGlobs   []string
	WatchMode           string
	PollInterval        time.Duration
	Debounce            time.Duration
	GraceTimeout        time.Duration
	KillTimeout         time.Duration
	RestartPolicy       string
	RestartInitialDelay time.Duration
	RestartMaxDelay     time.Duration
	RestartResetAfter   time.Duration
	HealthURL           string
	HealthPath          string
	HealthTimeout       time.Duration
	durationErrors      map[string]error
	healthConfigured    bool
}

// LoadConfig 读取并严格解析 sparq.watch.yaml。
func LoadConfig(path string) (Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	config, err := parseConfig(content)
	if err != nil {
		return Config{}, fmt.Errorf("解析 watch 配置 %s 失败: %w", path, err)
	}
	config.baseDir, err = filepath.Abs(filepath.Dir(path))
	if err != nil {
		return Config{}, fmt.Errorf("解析 watch 配置目录失败: %w", err)
	}
	return config, nil
}

func parseConfig(content []byte) (Config, error) {
	var config Config
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return Config{}, err
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); err == nil {
		return Config{}, fmt.Errorf("只能包含一个 YAML 文档")
	} else if err != io.EOF {
		return Config{}, err
	}
	return config, nil
}

// NewPlan 应用 watch 默认值并解析相对路径。
func NewPlan(config Config) Plan {
	cwd := resolvePath(defaultString(config.baseDir, "."), defaultString(config.CWD, "."))
	durations := map[string]durationResult{
		"watch.poll_interval":   parseDuration(config.Watch.PollInterval, 500*time.Millisecond),
		"watch.debounce":        parseDuration(config.Watch.Debounce, 300*time.Millisecond),
		"stop.grace_timeout":    parseDuration(config.Stop.GraceTimeout, 3*time.Second),
		"stop.kill_timeout":     parseDuration(config.Stop.KillTimeout, 2*time.Second),
		"restart.initial_delay": parseDuration(config.Restart.InitialDelay, 500*time.Millisecond),
		"restart.max_delay":     parseDuration(config.Restart.MaxDelay, 10*time.Second),
		"restart.reset_after":   parseDuration(config.Restart.ResetAfter, 30*time.Second),
		"health.timeout":        parseDuration(config.Health.Timeout, 30*time.Second),
	}
	durationErrors := make(map[string]error)
	for field, result := range durations {
		if result.err != nil {
			durationErrors[field] = result.err
		}
	}
	extensions := normalizeExtensions(defaultStrings(config.Watch.Extensions, []string{".go"}))
	files := defaultStrings(config.Watch.Files, []string{"go.mod", "go.sum"})
	if config.Watch.Include != nil {
		extensions = nil
		files = nil
	}
	return Plan{
		CWD:                 cwd,
		BuildCommand:        config.Build.Command,
		BuildEnv:            mapEnvironment(config.Build.Env),
		RunCommand:          config.Run.Command,
		RunEnv:              mapEnvironment(config.Run.Env),
		WatchPaths:          resolvePaths(cwd, defaultStrings(config.Watch.Paths, []string{"."})),
		WatchExtensions:     extensions,
		WatchFiles:          files,
		WatchInclude:        cloneStrings(config.Watch.Include),
		WatchExclude:        defaultStrings(config.Watch.Exclude, []string{".git", "tmp", "vendor", "storage"}),
		WatchExcludeGlobs:   cloneStrings(config.Watch.ExcludeGlobs),
		WatchMode:           defaultString(config.Watch.Mode, WatchModeNotify),
		PollInterval:        durations["watch.poll_interval"].value,
		Debounce:            durations["watch.debounce"].value,
		GraceTimeout:        durations["stop.grace_timeout"].value,
		KillTimeout:         durations["stop.kill_timeout"].value,
		RestartPolicy:       defaultString(config.Restart.Policy, RestartNever),
		RestartInitialDelay: durations["restart.initial_delay"].value,
		RestartMaxDelay:     durations["restart.max_delay"].value,
		RestartResetAfter:   durations["restart.reset_after"].value,
		HealthURL:           config.Health.URL,
		HealthPath:          config.Health.Path,
		HealthTimeout:       durations["health.timeout"].value,
		durationErrors:      durationErrors,
		healthConfigured:    config.Health.Path != "" || config.Health.Timeout != "",
	}
}

// Validate 校验命令、监听规则、进程策略和可选健康检查。
func (p Plan) Validate() error {
	if strings.TrimSpace(p.RunCommand) == "" {
		return fmt.Errorf("run.command 不能为空")
	}
	fields := []string{
		"watch.poll_interval",
		"watch.debounce",
		"stop.grace_timeout",
		"stop.kill_timeout",
		"restart.initial_delay",
		"restart.max_delay",
		"restart.reset_after",
	}
	for _, field := range fields {
		if err := p.durationErrors[field]; err != nil {
			return fmt.Errorf("%s 无效: %w", field, err)
		}
	}
	if err := p.watchOptions().Validate(); err != nil {
		return err
	}
	if err := p.restartOptions().Validate(); err != nil {
		return err
	}
	if p.HealthURL != "" {
		if err := p.durationErrors["health.timeout"]; err != nil {
			return fmt.Errorf("health.timeout 无效: %w", err)
		}
		if err := (HTTPChecker{URL: p.HealthURL, Path: p.HealthPath, Timeout: p.HealthTimeout}).Validate(); err != nil {
			return err
		}
	} else if p.healthConfigured {
		return fmt.Errorf("配置 health.path 或 health.timeout 时 health.url 不能为空")
	}
	return nil
}

func (p Plan) watchOptions() WatchOptions {
	return WatchOptions{
		Paths:        p.WatchPaths,
		Extensions:   p.WatchExtensions,
		Files:        p.WatchFiles,
		Include:      p.WatchInclude,
		Exclude:      p.WatchExclude,
		ExcludeGlobs: p.WatchExcludeGlobs,
		Mode:         p.WatchMode,
		PollInterval: p.PollInterval,
		Debounce:     p.Debounce,
	}
}

func (p Plan) restartOptions() RestartOptions {
	return RestartOptions{
		Policy:       p.RestartPolicy,
		InitialDelay: p.RestartInitialDelay,
		MaxDelay:     p.RestartMaxDelay,
		ResetAfter:   p.RestartResetAfter,
	}
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

func resolvePaths(cwd string, values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, filepath.Clean(resolvePath(cwd, value)))
	}
	return result
}

func resolvePath(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}

func normalizeExtensions(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if !strings.HasPrefix(value, ".") {
			value = "." + value
		}
		result = append(result, value)
	}
	return result
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

func defaultStrings(values, fallback []string) []string {
	if values == nil {
		return cloneStrings(fallback)
	}
	return cloneStrings(values)
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
