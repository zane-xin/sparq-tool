package devapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"

	"github.com/zane-xin/sparq-tool/internal/watch"
)

// Streams 描述应用开发会话启动的子进程输入输出。
type Streams struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Run 启动应用后端 watch 会话和前端开发进程。
func Run(ctx context.Context, plan Plan, streams Streams) (runErr error) {
	if err := plan.Validate(); err != nil {
		return err
	}
	if err := ensureEndpointsAvailable(plan); err != nil {
		return err
	}
	if streams.Stdout == nil {
		streams.Stdout = io.Discard
	}
	if streams.Stderr == nil {
		streams.Stderr = io.Discard
	}
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()

	backend, err := watch.StartPlan(runContext, plan.Backend, streams.Stdout, watch.SessionOptions{
		Name:              "应用后端",
		ExitOnProcessExit: true,
	})
	if err != nil {
		return fmt.Errorf("启动应用后端失败: %w", err)
	}
	var frontends []runningFrontend
	defer func() {
		cancel()
		var cleanupErr error
		for i := len(frontends) - 1; i >= 0; i-- {
			frontend := frontends[i]
			if err := frontend.process.Stop(); err != nil {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("停止%s失败: %w", frontend.plan.Name, err))
			}
		}
		if err := backend.Close(); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("停止应用后端失败: %w", err))
		}
		if cleanupErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("清理应用开发环境失败: %w", cleanupErr))
		}
	}()

	plans := plan.Frontends()
	exits := make(chan error, len(plans))
	for _, frontendPlan := range plans {
		process, err := watch.StartProcess(watch.Command{
			Name: frontendPlan.Name, Command: frontendPlan.Command, Args: frontendPlan.Args,
			CWD: frontendPlan.CWD, Env: frontendPlan.Env,
			GraceTimeout: frontendPlan.Grace, KillTimeout: frontendPlan.Kill,
			KeepStdinOpen: true, Stdout: streams.Stdout, Stderr: streams.Stderr,
		})
		if err != nil {
			return fmt.Errorf("启动%s失败: %w", frontendPlan.Name, err)
		}
		frontends = append(frontends, runningFrontend{plan: frontendPlan, process: process})
		go func() {
			<-process.Done()
			exits <- fmt.Errorf("%s开发进程已退出: %w", frontendPlan.Name, watch.ExitError(process))
		}()
	}

	// 先启动全部前端，避免 Shell 首次请求业务模块时该进程尚未启动。
	pending := false
	for _, frontend := range frontends {
		readyContext, stopReady := context.WithCancel(runContext)
		result := make(chan error, 1)
		go func() {
			p := frontend.plan
			result <- (watch.HTTPChecker{URL: p.URL, Path: p.Health, Timeout: p.Timeout}).Wait(readyContext, frontend.process)
		}()
		select {
		case <-ctx.Done():
			stopReady()
			return nil
		case <-backend.Done():
			stopReady()
			return fmt.Errorf("应用后端开发进程已退出: %w", backend.Err())
		case err := <-exits:
			stopReady()
			return err
		case err := <-result:
			stopReady()
			if ctx.Err() != nil {
				return nil
			}
			select {
			case <-frontend.process.Done():
				return fmt.Errorf("%s开发进程已退出: %w", frontend.plan.Name, watch.ExitError(frontend.process))
			default:
			}
			if err != nil {
				pending = true
				fmt.Fprintf(streams.Stderr, "[health] %s在 %s 内未就绪，开发进程仍在运行: %v\n", frontend.plan.Name, frontend.plan.Timeout, err)
			} else {
				fmt.Fprintf(streams.Stdout, "%s已就绪: %s\n", frontend.plan.Name, frontend.plan.URL)
			}
		}
	}
	if pending {
		fmt.Fprintln(streams.Stdout, "应用联调已启动；前端仍在编译或等待请求，请查看前端输出")
	} else {
		fmt.Fprintf(streams.Stdout, "应用联调已就绪: 后端 %s\n", plan.Backend.HealthURL)
	}
	select {
	case <-ctx.Done():
		return nil
	case <-backend.Done():
		return fmt.Errorf("应用后端开发进程已退出: %w", backend.Err())
	case err := <-exits:
		return err
	}
}

type runningFrontend struct {
	plan    FrontendPlan
	process *watch.Process
}

func ensureEndpointsAvailable(plan Plan) error {
	targets := []struct {
		name     string
		endpoint string
	}{
		{name: "应用后端", endpoint: plan.Backend.HealthURL},
	}
	for _, frontend := range plan.Frontends() {
		targets = append(targets, struct {
			name     string
			endpoint string
		}{name: frontend.Name, endpoint: frontend.URL})
	}
	checked := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		parsed, err := url.Parse(target.endpoint)
		if err != nil {
			return fmt.Errorf("解析%s地址失败 %s: %w", target.name, target.endpoint, err)
		}
		port := parsed.Port()
		if port == "" {
			port = map[string]string{"http": "80", "https": "443"}[parsed.Scheme]
		}
		address := net.JoinHostPort(parsed.Hostname(), port)
		if _, exists := checked[address]; exists {
			continue
		}
		checked[address] = struct{}{}
		// 部分系统的通配监听只覆盖 IPv6，必须同时检查配置中的具体地址。
		for _, bindAddress := range []string{address, net.JoinHostPort("", port)} {
			listener, err := net.Listen("tcp", bindAddress)
			if err != nil {
				return fmt.Errorf("%s地址不可用，无法启动本次开发进程 %s: %w", target.name, target.endpoint, err)
			}
			if err := listener.Close(); err != nil {
				return fmt.Errorf("释放%s地址预检监听失败 %s: %w", target.name, target.endpoint, err)
			}
		}
	}
	return nil
}
