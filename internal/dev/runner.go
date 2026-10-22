package dev

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zane-xin/sparq-protocol/plugin"
	"github.com/zane-xin/sparq-protocol/protocol"
	"github.com/zane-xin/sparq-protocol/remoteauth"
	"github.com/zane-xin/sparq-tool/internal/plugin/buildpipeline"
	"github.com/zane-xin/sparq-tool/internal/watch"
)

type durationValue struct {
	value time.Duration
	err   error
}

func newDuration(raw string, fallback time.Duration) durationValue {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return durationValue{value: fallback}
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		if err == nil {
			err = fmt.Errorf("必须是正 duration")
		}
		return durationValue{value: fallback, err: err}
	}
	return durationValue{value: value}
}

func (d durationValue) Duration() time.Duration {
	return d.value
}

// LifecycleTestOptions 描述一次开发生命周期验证。
type LifecycleTestOptions struct {
	Phase            plugin.LifecyclePhase
	AllowDestructive bool
}

// LifecycleTestReport 描述一次完整装配或单阶段诊断的结果。
type LifecycleTestReport struct {
	Plugin         string                  `json:"plugin"`
	Mode           string                  `json:"mode"`
	Phase          plugin.LifecyclePhase   `json:"phase,omitempty"`
	Status         string                  `json:"status"`
	ManifestDigest string                  `json:"manifest_digest"`
	Operations     []plugin.LifecyclePhase `json:"operations,omitempty"`
	OperationID    string                  `json:"operation_id,omitempty"`
}

type lifecycleBinding struct {
	session        LifecycleSession
	manifest       plugin.Manifest
	manifestDigest string
	sourceDigest   string
}

// Run 启动 dev 执行计划中的本地进程。
func Run(ctx context.Context, plan Plan, streams Streams) error {
	return run(ctx, plan, streams, nil)
}

// RunLifecycleTest 使用完整开发编排验证首次装配，并可追加一个显式阶段诊断。
func RunLifecycleTest(ctx context.Context, plan Plan, options LifecycleTestOptions, streams Streams) (LifecycleTestReport, error) {
	if len(plan.AppDevSecret) < 32 {
		return LifecycleTestReport{}, fmt.Errorf("dev lifecycle test 要求 app.dev_secret 至少 32 字节")
	}
	if options.Phase != "" && !isLifecycleTestPhase(options.Phase) {
		return LifecycleTestReport{}, fmt.Errorf("不支持开发生命周期阶段 %q", options.Phase)
	}
	if isDestructiveLifecycleTestPhase(options.Phase) && !options.AllowDestructive {
		return LifecycleTestReport{}, fmt.Errorf("诊断生命周期阶段 %s 必须显式设置 --allow-destructive", options.Phase)
	}
	plan.FrontendEnabled = false
	plan.FrontendCommand = ""
	var report LifecycleTestReport
	err := run(ctx, plan, streams, func(ctx context.Context, binding lifecycleBinding) error {
		report = LifecycleTestReport{
			Plugin: plan.PluginID, Mode: "first_assembly", Status: "succeeded",
			ManifestDigest: binding.manifestDigest,
			Operations:     append([]plugin.LifecyclePhase(nil), binding.manifest.Lifecycle.Operations...),
		}
		if options.Phase == "" {
			return nil
		}
		if !binding.manifest.Lifecycle.Supports(options.Phase) {
			return fmt.Errorf("插件 %s Manifest 未声明生命周期阶段 %s", plan.PluginID, options.Phase)
		}
		operationID := fmt.Sprintf("dev-lifecycle-%d", time.Now().UnixNano())
		result, err := ExecuteLifecycleTest(ctx, plan.AppEndpoint, LifecycleTestRequest{
			SessionID: binding.session.SessionID, OperationID: operationID, IdempotencyKey: operationID,
			Plugin: plan.PluginID, Component: binding.session.Component, Phase: string(options.Phase),
			SourceDigest: binding.sourceDigest, ManifestDigest: binding.manifestDigest,
		})
		if err != nil {
			return fmt.Errorf("诊断插件 %s 生命周期阶段 %s 失败: %w", plan.PluginID, options.Phase, err)
		}
		report.Mode = "phase"
		report.Phase = options.Phase
		report.Status = result.Status
		report.OperationID = result.OperationID
		return nil
	})
	return report, err
}

type backendReadyFunc func(context.Context, lifecycleBinding) error

func run(ctx context.Context, plan Plan, streams Streams, backendReady backendReadyFunc) (runErr error) {
	if err := plan.Validate(); err != nil {
		return err
	}
	if err := ensureManagedEndpointsAvailable(plan); err != nil {
		return err
	}
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	streams = streams.normalized()

	exits := make(chan processExit, 3)
	var appProcess *watch.Process
	var pluginProcess *watch.Process
	var frontendProcess *watch.Process
	var supervisor *watch.Supervisor
	var token string
	var binding lifecycleBinding
	var lifecycleErrorChannels []<-chan error
	defer func() {
		var cleanupErr error
		if token != "" {
			revokeContext, revokeCancel := context.WithTimeout(context.Background(), 5*time.Second)
			cleanupErr = errors.Join(cleanupErr, revokeDevToken(revokeContext, plan.AppEndpoint, token))
			revokeCancel()
		}
		if err := stopNamedProcess("插件前端", frontendProcess); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
		if supervisor != nil {
			if err := supervisor.Close(); err != nil {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("停止插件热更新 supervisor 失败: %w", err))
			}
		} else if err := stopNamedProcess("插件后端", pluginProcess); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
		// Remote 插件必须先完成 Stop -> Deactivate -> Unregister，再释放宿主中的开发占用。
		if binding.session.SessionID != "" {
			revokeContext, revokeCancel := context.WithTimeout(context.Background(), 5*time.Second)
			cleanupErr = errors.Join(cleanupErr, RevokeLifecycleSession(revokeContext, plan.AppEndpoint, LifecycleRevokeRequest{SessionID: binding.session.SessionID}))
			revokeCancel()
		}
		if err := stopNamedProcess("宿主", appProcess); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
		if cleanupErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("清理开发环境失败: %w", cleanupErr))
		}
	}()

	if plan.AppCommand != "" {
		process, err := watch.StartProcess(watch.Command{
			Name: plan.AppCommand, Command: plan.AppCommand, CWD: plan.AppCWD,
			Stdin: streams.Stdin, Stdout: streams.Stdout, Stderr: streams.Stderr,
		})
		if err != nil {
			return err
		}
		appProcess = process
		monitorProcess(exits, process)
	}
	if err := waitHealthWithExits(runContext, plan.AppEndpoint, plan.AppHealthPath, plan.AppTimeout.Duration(), exits, nil); err != nil {
		return fmt.Errorf("等待宿主健康检查失败: %w", err)
	}
	initialBuildComplete := false
	if plan.AppDevSecret != "" && plan.ReloadEnabled {
		if err := buildPluginOnce(runContext, plan, streams); err != nil {
			return err
		}
		initialBuildComplete = true
	}
	if plan.AppDevSecret != "" {
		manifest, err := buildpipeline.ResolveManifest(runContext, plan.ManifestPath)
		if err != nil {
			return fmt.Errorf("解析插件完整生命周期 Manifest 失败: %w", err)
		}
		manifestDigest, err := plugin.ManifestDigest(manifest)
		if err != nil {
			return fmt.Errorf("计算插件 Manifest 摘要失败: %w", err)
		}
		sourceDigest, err := SourceDigest(plan)
		if err != nil {
			return fmt.Errorf("计算插件源码摘要失败: %w", err)
		}
		operations := make([]string, 0, len(manifest.Lifecycle.Operations))
		for _, operation := range manifest.Lifecycle.Operations {
			operations = append(operations, string(operation))
		}
		component, err := resolvePluginComponent(manifest, plan.PluginComponent)
		if err != nil {
			return fmt.Errorf("解析插件开发组件失败: %w", err)
		}
		plan.PluginComponent = string(component)
		session, err := RequestLifecycleSession(runContext, plan.AppEndpoint, plan.AppDevSecret, LifecycleSessionRequest{
			Actor: "sparq-tool", Plugin: plan.PluginID, Component: plan.PluginComponent,
			SourceDigest: sourceDigest, ManifestDigest: manifestDigest, Operations: operations,
		})
		if err != nil {
			return fmt.Errorf("申请开发生命周期 session 失败: %w", err)
		}
		binding = lifecycleBinding{session: session, manifest: manifest, manifestDigest: manifestDigest, sourceDigest: sourceDigest}
		plan.LifecycleSessionID = session.SessionID
		plan.LifecycleSourceDigest = sourceDigest
		heartbeatInterval, err := lifecycleSessionHeartbeatInterval(session, time.Now())
		if err != nil {
			return err
		}
		lifecycleErrorChannels = append(lifecycleErrorChannels, keepLifecycleSessionAlive(runContext, plan.AppEndpoint, session, heartbeatInterval))
		fmt.Fprintf(streams.Stdout, "插件开发生命周期 session 已建立: %s\n", session.SessionID)
	}

	pluginSigner, err := remoteauth.NewSigner(plugin.Identity{PluginID: protocol.PluginID(plan.PluginID)}, plan.PluginSecret)
	if err != nil {
		return fmt.Errorf("创建插件健康检查签名器失败: %w", err)
	}
	if plan.ReloadEnabled {
		supervisor, err = startPluginSupervisor(runContext, plan, pluginSigner, streams, initialBuildComplete)
		if err != nil {
			return err
		}
	} else {
		pluginProcess, err = watch.StartProcess(watch.Command{
			Name: plan.PluginCommand, Command: plan.PluginCommand, CWD: plan.PluginCWD, Env: pluginEnvironment(plan),
			Stdin: streams.Stdin, Stdout: streams.Stdout, Stderr: streams.Stderr,
		})
		if err != nil {
			return err
		}
		monitorProcess(exits, pluginProcess)
		if err := waitHealthWithExits(runContext, plan.PluginEndpoint, plan.PluginHealthPath, plan.PluginTimeout.Duration(), exits, pluginSigner); err != nil {
			return fmt.Errorf("等待插件健康检查失败: %w", err)
		}
	}
	if backendReady != nil {
		if binding.session.SessionID == "" {
			return fmt.Errorf("开发生命周期 session 未建立")
		}
		return backendReady(runContext, binding)
	}
	if plan.FrontendEnabled {
		devToken, err := requestDevToken(runContext, plan.AppEndpoint, plan.AppDevSecret, plan.PluginID)
		if err != nil {
			return fmt.Errorf("获取开发态 token 失败: %w", err)
		}
		token = devToken.Token
		rootURL := plan.frontendRootURL()
		if err := registerDevFrontend(runContext, plan.AppEndpoint, token, plan.PluginID, rootURL); err != nil {
			return fmt.Errorf("注册插件开发前端入口失败: %w", err)
		}
		heartbeatInterval, err := devFrontendHeartbeatInterval(devToken, time.Now())
		if err != nil {
			return err
		}
		frontendProcess, err = watch.StartProcess(watch.Command{
			Name: plan.FrontendCommand, Command: plan.FrontendCommand, CWD: plan.FrontendCWD,
			Env: frontendEnvironment(plan, token), Stdin: streams.Stdin, Stdout: streams.Stdout, Stderr: streams.Stderr,
		})
		if err != nil {
			return err
		}
		monitorProcess(exits, frontendProcess)
		readiness := plan.frontendReadinessTargets()
		for _, target := range readiness {
			if err := waitHealthWithExits(runContext, target.URL, target.HealthPath, plan.FrontendTimeout.Duration(), exits, nil); err != nil {
				return fmt.Errorf("等待%s失败: %w", target.Name, err)
			}
		}
		lifecycleErrorChannels = append(lifecycleErrorChannels, keepDevFrontendRegistered(
			runContext,
			plan.AppEndpoint,
			token,
			plan.PluginID,
			rootURL,
			heartbeatInterval,
		))
		fmt.Fprintf(streams.Stdout, "插件前端已就绪: %s\n", readiness[0].URL)
	}

	return waitProcesses(runContext, exits, mergeLifecycleErrors(runContext, lifecycleErrorChannels...), cancel)
}

func resolvePluginComponent(manifest plugin.Manifest, configured string) (protocol.ComponentID, error) {
	if configured = strings.TrimSpace(configured); configured != "" {
		componentID := protocol.ComponentID(configured)
		component, exists := manifest.Component(componentID)
		if !exists {
			return "", fmt.Errorf("plugin.component %q 未在完整 Manifest 中声明", configured)
		}
		if !isRemoteDevelopmentComponent(component) {
			return "", fmt.Errorf("plugin.component %q 不是支持 http runtime 的 backend 或 worker 组件", configured)
		}
		return componentID, nil
	}
	if len(manifest.Components) == 0 {
		component, _ := manifest.Component(plugin.DefaultComponentID)
		if !isRemoteDevelopmentComponent(component) {
			return "", fmt.Errorf("旧版 default 组件未声明 http runtime")
		}
		return plugin.DefaultComponentID, nil
	}
	candidates := make([]protocol.ComponentID, 0, len(manifest.Components))
	for _, component := range manifest.Components {
		if isRemoteDevelopmentComponent(component) {
			candidates = append(candidates, component.ID)
		}
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("完整 Manifest 没有支持 http runtime 的 backend 或 worker 组件")
	}
	values := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		values = append(values, string(candidate))
	}
	return "", fmt.Errorf("完整 Manifest 有多个可开发组件 %s，请设置 plugin.component", strings.Join(values, ", "))
}

func isRemoteDevelopmentComponent(component plugin.ComponentManifest) bool {
	if component.Kind != plugin.ComponentKindBackend && component.Kind != plugin.ComponentKindWorker {
		return false
	}
	for _, runtime := range component.Runtime {
		if runtime == plugin.RuntimeHTTP {
			return true
		}
	}
	return false
}

func isLifecycleTestPhase(phase plugin.LifecyclePhase) bool {
	switch phase {
	case plugin.PhaseInstall, plugin.PhaseMigrate, plugin.PhaseActivate, plugin.PhaseDeactivate, plugin.PhaseUninstall, plugin.PhaseRollback:
		return true
	default:
		return false
	}
}

func isDestructiveLifecycleTestPhase(phase plugin.LifecyclePhase) bool {
	switch phase {
	case plugin.PhaseDeactivate, plugin.PhaseUninstall, plugin.PhaseRollback:
		return true
	default:
		return false
	}
}
