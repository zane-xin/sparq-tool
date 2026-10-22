package dev

import (
	"context"
	"errors"
	"fmt"

	"github.com/zane-xin/sparq-protocol/remoteauth"
	"github.com/zane-xin/sparq-tool/internal/watch"
)

func buildPluginOnce(ctx context.Context, plan Plan, streams Streams) error {
	process, err := watch.StartProcess(watch.Command{
		Name: "插件后端构建", Command: plan.ReloadBuild, CWD: plan.PluginCWD,
		Env: pluginEnvironment(plan), Stdin: streams.Stdin, Stdout: streams.Stdout, Stderr: streams.Stderr,
	})
	if err != nil {
		return err
	}
	select {
	case <-process.Done():
		if process.Err() != nil {
			return fmt.Errorf("插件后端首次构建失败: %w", process.Err())
		}
		return nil
	case <-ctx.Done():
		return errors.Join(ctx.Err(), process.Stop())
	}
}

type pluginReadyChecker struct {
	plan   Plan
	signer *remoteauth.Signer
}

func (c pluginReadyChecker) Wait(ctx context.Context, process *watch.Process) error {
	return waitHealthWithProcess(
		ctx,
		c.plan.PluginEndpoint,
		c.plan.PluginHealthPath,
		c.plan.PluginTimeout.Duration(),
		process,
		c.signer,
	)
}

func startPluginSupervisor(
	ctx context.Context,
	plan Plan,
	signer *remoteauth.Signer,
	streams Streams,
	skipInitialBuild bool,
) (*watch.Supervisor, error) {
	environment := pluginEnvironment(plan)
	return watch.Start(ctx, watch.Options{
		Name: "插件后端",
		Watch: watch.WatchOptions{
			Paths:      plan.ReloadPaths,
			Extensions: plan.ReloadExtensions,
			Files:      plan.ReloadFiles,
			Exclude:    plan.ReloadExclude,
			Debounce:   plan.ReloadDebounce.Duration(),
		},
		Build: watch.Command{
			Name: "插件后端构建", Command: plan.ReloadBuild, CWD: plan.PluginCWD, Env: environment,
			Stdin: streams.Stdin, Stdout: streams.Stdout, Stderr: streams.Stderr,
		},
		Run: watch.Command{
			Name: "插件后端", Command: plan.PluginCommand, CWD: plan.PluginCWD, Env: environment,
			Stdin: streams.Stdin, Stdout: streams.Stdout, Stderr: streams.Stderr,
		},
		Checker:          pluginReadyChecker{plan: plan, signer: signer},
		Writer:           streams.Stdout,
		SkipInitialBuild: skipInitialBuild,
	})
}
