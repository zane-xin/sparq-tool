package watch

import (
	"context"
	"io"
)

// SessionOptions 控制一次已解析 watch 计划的会话行为。
type SessionOptions struct {
	Name              string
	ExitOnProcessExit bool
}

// StartPlan 启动已校验的文件监听和进程替换会话。
// 调用方负责在会话结束后调用 Supervisor.Close 清理监听器和运行进程。
func StartPlan(ctx context.Context, plan Plan, writer io.Writer, sessionOptions SessionOptions) (*Supervisor, error) {
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	var checker ReadyChecker
	if plan.HealthURL != "" {
		checker = HTTPChecker{URL: plan.HealthURL, Path: plan.HealthPath, Timeout: plan.HealthTimeout}
	}
	return Start(ctx, Options{
		Name:  sessionOptions.Name,
		Watch: plan.watchOptions(),
		Build: Command{
			Name: "watch 构建", Command: plan.BuildCommand, CWD: plan.CWD, Env: plan.BuildEnv,
			GraceTimeout: plan.GraceTimeout, KillTimeout: plan.KillTimeout,
		},
		Run: Command{
			Name: sessionOptions.Name, Command: plan.RunCommand, CWD: plan.CWD, Env: plan.RunEnv,
			GraceTimeout: plan.GraceTimeout, KillTimeout: plan.KillTimeout,
		},
		Restart:           plan.restartOptions(),
		Checker:           checker,
		ExitOnProcessExit: sessionOptions.ExitOnProcessExit,
		Writer:            writer,
	})
}

// RunPlan 启动独立文件监听和进程替换会话。
func RunPlan(ctx context.Context, plan Plan, writer io.Writer) error {
	supervisor, err := StartPlan(ctx, plan, writer, SessionOptions{Name: "watch 进程"})
	if err != nil {
		return err
	}
	<-ctx.Done()
	return supervisor.Close()
}
