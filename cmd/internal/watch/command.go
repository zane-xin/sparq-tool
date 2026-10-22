package watchcmd

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/internal/watch"
)

// New 创建独立 watch 命令。
func New() *cobra.Command {
	var file string
	var dryRun bool
	var initialize bool
	command := &cobra.Command{
		Use:   "watch",
		Short: "监听文件变化并重新运行开发命令",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if initialize {
				return watch.InitConfig(file, command.OutOrStdout())
			}
			config, err := watch.LoadConfig(file)
			if err != nil {
				return err
			}
			plan := watch.NewPlan(config)
			if err := plan.Validate(); err != nil {
				return err
			}
			if dryRun {
				PrintPlan(command.OutOrStdout(), plan)
				return nil
			}
			ctx, stop := signal.NotifyContext(command.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			PrintPlan(command.OutOrStdout(), plan)
			fmt.Fprintln(command.OutOrStdout(), "watch 进程启动中，按 Ctrl+C 停止")
			return watch.RunPlan(ctx, plan, command.OutOrStdout())
		},
	}
	command.Flags().StringVar(&file, "file", "sparq.watch.yaml", "watch 配置文件路径")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "只打印执行计划，不启动进程")
	command.Flags().BoolVar(&initialize, "init", false, "生成标准 watch 配置，不覆盖已有文件")
	command.MarkFlagsMutuallyExclusive("dry-run", "init")
	return command
}

// PrintPlan 打印独立 watch 执行计划。
func PrintPlan(writer io.Writer, plan watch.Plan) {
	fmt.Fprintf(writer, "工作目录: %s\n", plan.CWD)
	fmt.Fprintf(writer, "运行命令: %s\n", plan.RunCommand)
	if plan.BuildCommand == "" {
		fmt.Fprintln(writer, "构建命令: 未配置，文件变化后直接重新运行")
	} else {
		fmt.Fprintf(writer, "构建命令: %s\n", plan.BuildCommand)
	}
	fmt.Fprintf(writer, "监听目录: %s\n", strings.Join(plan.WatchPaths, ", "))
	fmt.Fprintf(writer, "监听模式: %s\n", plan.WatchMode)
	if plan.WatchMode == watch.WatchModePoll {
		fmt.Fprintf(writer, "轮询间隔: %s\n", plan.PollInterval)
	}
	if len(plan.WatchInclude) > 0 {
		fmt.Fprintf(writer, "包含规则: %s\n", strings.Join(plan.WatchInclude, ", "))
	} else {
		fmt.Fprintf(writer, "监听扩展名: %s\n", strings.Join(plan.WatchExtensions, ", "))
		fmt.Fprintf(writer, "监听文件名: %s\n", strings.Join(plan.WatchFiles, ", "))
	}
	fmt.Fprintf(writer, "排除目录: %s\n", strings.Join(plan.WatchExclude, ", "))
	if len(plan.WatchExcludeGlobs) > 0 {
		fmt.Fprintf(writer, "排除规则: %s\n", strings.Join(plan.WatchExcludeGlobs, ", "))
	}
	fmt.Fprintf(writer, "监听防抖: %s\n", plan.Debounce)
	fmt.Fprintf(writer, "退出等待: 优雅 %s，强制 %s\n", plan.GraceTimeout, plan.KillTimeout)
	fmt.Fprintf(writer, "失败重启: %s\n", plan.RestartPolicy)
	if plan.RestartPolicy == watch.RestartOnFailure {
		fmt.Fprintf(
			writer,
			"重启退避: 初始 %s，最大 %s，稳定运行 %s 后重置\n",
			plan.RestartInitialDelay,
			plan.RestartMaxDelay,
			plan.RestartResetAfter,
		)
	}
	if plan.HealthURL != "" {
		target := strings.TrimRight(plan.HealthURL, "/")
		if plan.HealthPath != "" {
			target += "/" + strings.TrimLeft(plan.HealthPath, "/")
		}
		fmt.Fprintf(writer, "健康检查: %s\n", target)
	}
}
