package dev

import (
	"context"
	"fmt"
	"net"
	"net/url"

	"github.com/zane-xin/sparq-tool/internal/watch"
)

type processExit struct {
	process *watch.Process
	err     error
}

func ensureManagedEndpointsAvailable(plan Plan) error {
	endpoints := []struct {
		name     string
		endpoint string
		enabled  bool
	}{
		{name: "宿主", endpoint: plan.AppEndpoint, enabled: plan.AppCommand != ""},
		{name: "插件后端", endpoint: plan.PluginEndpoint, enabled: true},
	}
	if plan.FrontendEnabled {
		for _, target := range plan.frontendProcessTargets() {
			endpoints = append(endpoints, struct {
				name     string
				endpoint string
				enabled  bool
			}{name: target.Name, endpoint: target.URL, enabled: true})
		}
	}
	checkedAddresses := map[string]struct{}{}
	for _, item := range endpoints {
		if !item.enabled {
			continue
		}
		parsed, err := url.Parse(item.endpoint)
		if err != nil {
			return fmt.Errorf("解析%s地址失败 %s: %w", item.name, item.endpoint, err)
		}
		port := parsed.Port()
		if port == "" {
			port = map[string]string{"http": "80", "https": "443"}[parsed.Scheme]
		}
		address := net.JoinHostPort(parsed.Hostname(), port)
		if _, checked := checkedAddresses[address]; checked {
			continue
		}
		checkedAddresses[address] = struct{}{}
		listener, err := net.Listen("tcp", net.JoinHostPort("", port))
		if err != nil {
			return fmt.Errorf("%s地址不可用，无法启动本次开发进程 %s: %w", item.name, item.endpoint, err)
		}
		if err := listener.Close(); err != nil {
			return fmt.Errorf("释放%s地址预检监听失败 %s: %w", item.name, item.endpoint, err)
		}
	}
	return nil
}

type frontendTarget struct {
	Name       string
	URL        string
	HealthPath string
}

func (p Plan) frontendProcessTargets() []frontendTarget {
	targets := make([]frontendTarget, 0, 1)
	if p.FrontendURL != "" {
		targets = append(targets, frontendTarget{Name: "插件前端", URL: p.FrontendURL})
	}
	return uniqueFrontendTargets(targets)
}

func (p Plan) frontendReadinessTargets() []frontendTarget {
	targets := make([]frontendTarget, 0, 1)
	if p.FrontendURL != "" {
		targets = append(targets, frontendTarget{Name: "插件前端", URL: p.FrontendURL, HealthPath: p.FrontendHealthPath})
	}
	return uniqueFrontendTargets(targets)
}

func uniqueFrontendTargets(targets []frontendTarget) []frontendTarget {
	output := make([]frontendTarget, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		key := target.URL + "\n" + target.HealthPath
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		output = append(output, target)
	}
	return output
}

func waitProcesses(ctx context.Context, exits <-chan processExit, lifecycleErrors <-chan error, cancel context.CancelFunc) error {
	select {
	case <-ctx.Done():
		return nil
	case result := <-exits:
		cancel()
		return formatProcessExit(result)
	case err := <-lifecycleErrors:
		cancel()
		return err
	}
}

func mergeLifecycleErrors(ctx context.Context, channels ...<-chan error) <-chan error {
	merged := make(chan error, 1)
	for _, channel := range channels {
		if channel == nil {
			continue
		}
		go func(errors <-chan error) {
			select {
			case <-ctx.Done():
				return
			case err, ok := <-errors:
				if !ok || err == nil {
					return
				}
				select {
				case merged <- err:
				default:
				}
			}
		}(channel)
	}
	return merged
}

func formatProcessExit(result processExit) error {
	if result.err != nil {
		return fmt.Errorf("开发进程 %s 退出异常: %w", result.process.Name(), result.err)
	}
	return fmt.Errorf("开发进程 %s 已退出", result.process.Name())
}

func monitorProcess(exits chan<- processExit, process *watch.Process) {
	go func() {
		<-process.Done()
		exits <- processExit{process: process, err: process.Err()}
	}()
}

func stopNamedProcess(name string, process *watch.Process) error {
	if process == nil {
		return nil
	}
	if err := process.Stop(); err != nil {
		return fmt.Errorf("停止开发进程%s失败: %w", name, err)
	}
	return nil
}
