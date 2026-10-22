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
	var frontend *watch.Process
	defer func() {
		var cleanupErr error
		if frontend != nil {
			if err := frontend.Stop(); err != nil {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("停止应用前端失败: %w", err))
			}
		}
		if err := backend.Close(); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("停止应用后端失败: %w", err))
		}
		if cleanupErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("清理应用开发环境失败: %w", cleanupErr))
		}
	}()

	if !plan.FrontendEnabled {
		select {
		case <-ctx.Done():
			return nil
		case <-backend.Done():
			return fmt.Errorf("应用后端开发进程已退出: %w", backend.Err())
		}
	}

	frontend, err = watch.StartProcess(watch.Command{
		Name: "应用前端", Command: plan.FrontendCommand, Args: plan.FrontendArgs, CWD: plan.FrontendCWD, Env: plan.FrontendEnv,
		GraceTimeout: plan.FrontendGrace, KillTimeout: plan.FrontendKill,
		KeepStdinOpen: true, Stdout: streams.Stdout, Stderr: streams.Stderr,
	})
	if err != nil {
		return err
	}
	checker := watch.HTTPChecker{URL: plan.FrontendURL, Path: plan.FrontendHealth, Timeout: plan.FrontendTimeout}
	if err := waitFrontendReady(runContext, checker, frontend, backend); err != nil {
		select {
		case <-ctx.Done():
			return nil
		case <-backend.Done():
			return fmt.Errorf("等待应用前端就绪失败: 应用后端开发进程已退出: %w", backend.Err())
		case <-frontend.Done():
			return fmt.Errorf("等待应用前端就绪失败: %w", watch.ExitError(frontend))
		default:
			fmt.Fprintf(streams.Stderr, "[health] 应用前端在 %s 内未就绪，开发进程仍在运行: %v\n", plan.FrontendTimeout, err)
			fmt.Fprintln(streams.Stdout, "应用联调已启动；应用前端仍在编译或等待请求，请查看前端输出")
		}
	} else {
		fmt.Fprintf(streams.Stdout, "应用联调已就绪: 后端 %s，前端 %s\n", plan.Backend.HealthURL, plan.FrontendURL)
	}

	select {
	case <-ctx.Done():
		return nil
	case <-backend.Done():
		return fmt.Errorf("应用后端开发进程已退出: %w", backend.Err())
	case <-frontend.Done():
		return fmt.Errorf("应用前端开发进程已退出: %w", watch.ExitError(frontend))
	}
}

func waitFrontendReady(
	ctx context.Context,
	checker watch.HTTPChecker,
	frontend *watch.Process,
	backend *watch.Supervisor,
) error {
	result := make(chan error, 1)
	go func() {
		result <- checker.Wait(ctx, frontend)
	}()
	select {
	case err := <-result:
		return err
	case <-backend.Done():
		return fmt.Errorf("应用后端开发进程已退出: %w", backend.Err())
	case <-ctx.Done():
		return ctx.Err()
	}
}

func ensureEndpointsAvailable(plan Plan) error {
	targets := []struct {
		name     string
		endpoint string
	}{
		{name: "应用后端", endpoint: plan.Backend.HealthURL},
	}
	if plan.FrontendEnabled {
		targets = append(targets, struct {
			name     string
			endpoint string
		}{name: "应用前端", endpoint: plan.FrontendURL})
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
		listener, err := net.Listen("tcp", net.JoinHostPort("", port))
		if err != nil {
			return fmt.Errorf("%s地址不可用，无法启动本次开发进程 %s: %w", target.name, target.endpoint, err)
		}
		if err := listener.Close(); err != nil {
			return fmt.Errorf("释放%s地址预检监听失败 %s: %w", target.name, target.endpoint, err)
		}
	}
	return nil
}
