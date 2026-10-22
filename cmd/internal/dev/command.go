package devcmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-protocol/plugin"
	"github.com/zane-xin/sparq-tool/internal/dev"
	"github.com/zane-xin/sparq-tool/internal/devapp"
)

type options struct {
	file            string
	manifest        string
	appCommand      string
	pluginCommand   string
	frontendCommand string
	dryRun          bool
}

// New 创建 dev 命令。
func New() *cobra.Command {
	command := newPluginCommand("dev", "启动插件本地开发联调")
	command.AddCommand(newPluginCommand("plugin", "启动插件本地开发联调"), newAppCommand(), newLifecycleCommand())
	return command
}

func newLifecycleCommand() *cobra.Command {
	command := &cobra.Command{Use: "lifecycle", Short: "验证插件开发生命周期"}
	command.AddCommand(newLifecycleTestCommand())
	return command
}

func newLifecycleTestCommand() *cobra.Command {
	options := options{}
	var phase string
	var jsonOutput bool
	var allowDestructive bool
	command := &cobra.Command{
		Use:   "test",
		Short: "启动开发编排并验证插件首次装配生命周期",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			config, err := dev.LoadConfig(options.file)
			if err != nil {
				return err
			}
			plan := dev.NewPlan(config, dev.Options{
				Manifest: options.manifest, AppCommand: options.appCommand, PluginCommand: options.pluginCommand,
			})
			ctx, stop := signal.NotifyContext(command.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			streams := dev.Streams{Stdin: command.InOrStdin(), Stdout: command.OutOrStdout(), Stderr: command.ErrOrStderr()}
			if jsonOutput {
				streams.Stdout = command.ErrOrStderr()
			}
			report, err := dev.RunLifecycleTest(ctx, plan, dev.LifecycleTestOptions{
				Phase: plugin.LifecyclePhase(phase), AllowDestructive: allowDestructive,
			}, streams)
			if err != nil {
				return err
			}
			if jsonOutput {
				payload, err := json.Marshal(report)
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(command.OutOrStdout(), string(payload))
				return err
			}
			printLifecycleTestReport(command.OutOrStdout(), report)
			return nil
		},
	}
	command.Flags().StringVar(&options.file, "file", "sparq.dev.yaml", "开发配置文件路径")
	command.Flags().StringVar(&options.manifest, "manifest", "", "插件清单路径覆盖")
	command.Flags().StringVar(&options.appCommand, "app-command", "", "宿主 app 启动命令覆盖")
	command.Flags().StringVar(&options.pluginCommand, "plugin-command", "", "插件后端启动命令覆盖")
	command.Flags().StringVar(&phase, "phase", "", "Ready 后追加诊断的生命周期阶段")
	command.Flags().BoolVar(&allowDestructive, "allow-destructive", false, "允许诊断 deactivate、rollback 或 uninstall")
	command.Flags().BoolVar(&jsonOutput, "json", false, "输出机器可读 JSON")
	return command
}

func printLifecycleTestReport(writer io.Writer, report dev.LifecycleTestReport) {
	fmt.Fprintf(writer, "插件生命周期验证通过: %s\n", report.Plugin)
	if report.Mode == "phase" {
		fmt.Fprintf(writer, "诊断阶段: %s\n", report.Phase)
		fmt.Fprintf(writer, "操作 ID: %s\n", report.OperationID)
	} else {
		fmt.Fprintln(writer, "验证模式: 首次装配")
	}
	if len(report.Operations) > 0 {
		operations := make([]string, 0, len(report.Operations))
		for _, operation := range report.Operations {
			operations = append(operations, string(operation))
		}
		fmt.Fprintf(writer, "声明阶段: %s\n", strings.Join(operations, ", "))
	}
	fmt.Fprintf(writer, "Manifest 摘要: %s\n", report.ManifestDigest)
}

func newPluginCommand(use, short string) *cobra.Command {
	options := options{}
	command := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			config, err := dev.LoadConfig(options.file)
			if err != nil {
				return err
			}
			plan := dev.NewPlan(config, dev.Options{
				Manifest:        options.manifest,
				AppCommand:      options.appCommand,
				PluginCommand:   options.pluginCommand,
				FrontendCommand: options.frontendCommand,
			})
			if err := plan.Validate(); err != nil {
				return err
			}
			if options.dryRun {
				printPlan(command.OutOrStdout(), plan)
				return nil
			}
			ctx, stop := signal.NotifyContext(command.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			fmt.Fprintln(command.OutOrStdout(), "开发进程启动中，按 Ctrl+C 停止")
			return dev.Run(ctx, plan, dev.Streams{
				Stdin: command.InOrStdin(), Stdout: command.OutOrStdout(), Stderr: command.ErrOrStderr(),
			})
		},
	}
	command.Flags().StringVar(&options.file, "file", "sparq.dev.yaml", "开发配置文件路径")
	command.Flags().StringVar(&options.manifest, "manifest", "", "插件清单路径覆盖")
	command.Flags().StringVar(&options.appCommand, "app-command", "", "宿主 app 启动命令覆盖")
	command.Flags().StringVar(&options.pluginCommand, "plugin-command", "", "插件后端启动命令覆盖")
	command.Flags().StringVar(&options.frontendCommand, "frontend-command", "", "插件前端启动命令覆盖")
	command.Flags().BoolVar(&options.dryRun, "dry-run", false, "只打印执行计划，不启动进程")
	return command
}

func newAppCommand() *cobra.Command {
	var file string
	var dryRun bool
	command := &cobra.Command{
		Use:   "app",
		Short: "启动二次开发应用本地联调",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			plan, err := devapp.LoadPlan(file)
			if err != nil {
				return err
			}
			if err := plan.Validate(); err != nil {
				return err
			}
			if dryRun {
				printAppPlan(command.OutOrStdout(), plan)
				return nil
			}
			ctx, stop := signal.NotifyContext(command.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			fmt.Fprintln(command.OutOrStdout(), "应用开发进程启动中，按 Ctrl+C 停止")
			return devapp.Run(ctx, plan, devapp.Streams{
				Stdin: command.InOrStdin(), Stdout: command.OutOrStdout(), Stderr: command.ErrOrStderr(),
			})
		},
	}
	command.Flags().StringVar(&file, "file", "sparq.dev.yaml", "应用开发配置文件路径")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "只打印执行计划，不启动进程")
	return command
}

func printPlan(writer io.Writer, plan dev.Plan) {
	fmt.Fprintf(writer, "宿主地址: %s\n", fallback(plan.AppEndpoint, "未配置"))
	fmt.Fprintf(writer, "宿主健康检查: %s\n", plan.AppHealthPath)
	fmt.Fprintf(writer, "插件 ID: %s\n", fallback(plan.PluginID, "未配置"))
	fmt.Fprintf(writer, "插件组件: %s\n", fallback(plan.PluginComponent, "从完整 Manifest 自动选择"))
	fmt.Fprintf(writer, "插件地址: %s\n", fallback(plan.PluginEndpoint, "未配置"))
	fmt.Fprintf(writer, "插件健康检查: %s\n", plan.PluginHealthPath)
	if plan.AppCommand != "" {
		fmt.Fprintf(writer, "宿主命令: %s\n", plan.AppCommand)
	}
	fmt.Fprintf(writer, "插件后端命令: %s\n", plan.PluginCommand)
	if plan.ReloadEnabled {
		fmt.Fprintf(writer, "插件热更新构建命令: %s\n", plan.ReloadBuild)
		fmt.Fprintf(writer, "插件热更新监听目录: %s\n", strings.Join(plan.ReloadPaths, ", "))
		fmt.Fprintf(writer, "插件热更新防抖: %s\n", plan.ReloadDebounce.Duration())
	}
	if plan.FrontendEnabled {
		fmt.Fprintf(writer, "插件前端命令: %s\n", fallback(plan.FrontendCommand, "未配置"))
		if plan.FrontendURL != "" {
			fmt.Fprintf(writer, "插件前端根地址: %s\n", plan.FrontendURL)
			fmt.Fprintf(writer, "插件前端根就绪检查: %s\n", fallback(plan.FrontendHealthPath, "/"))
		}
	}
	if plan.ReloadEnabled {
		fmt.Fprintln(writer, "启动顺序: 宿主 -> 插件构建检查 -> 开发生命周期 session -> 插件后端 -> 开发态 token -> 前端覆盖登记 -> 插件前端")
	} else {
		fmt.Fprintln(writer, "启动顺序: 宿主 -> 开发生命周期 session -> 插件后端 -> 开发态 token -> 前端覆盖登记 -> 插件前端")
	}
	fmt.Fprintln(writer, "退出行为: 注销开发态 token，并停止本次启动的进程")
}

func printAppPlan(writer io.Writer, plan devapp.Plan) {
	fmt.Fprintf(writer, "应用后端 watch 配置: %s\n", plan.BackendWatchFile)
	fmt.Fprintf(writer, "应用后端工作目录: %s\n", plan.Backend.CWD)
	fmt.Fprintf(writer, "应用后端命令: %s\n", plan.Backend.RunCommand)
	fmt.Fprintf(writer, "应用后端健康检查: %s%s\n", plan.Backend.HealthURL, plan.Backend.HealthPath)
	if plan.FrontendEnabled {
		fmt.Fprintf(writer, "应用前端工作目录: %s\n", plan.FrontendCWD)
		fmt.Fprintf(writer, "应用前端命令: %s %s\n", plan.FrontendCommand, strings.Join(plan.FrontendArgs, " "))
		fmt.Fprintf(writer, "应用前端地址: %s\n", plan.FrontendURL)
		fmt.Fprintf(writer, "应用前端就绪检查: %s\n", fallback(plan.FrontendURL+plan.FrontendHealth, "/"))
		fmt.Fprintln(writer, "启动顺序: 应用后端 -> 应用后端健康检查 -> 应用前端 -> 应用前端就绪检查")
	} else {
		fmt.Fprintln(writer, "应用前端: 已按配置跳过")
		fmt.Fprintln(writer, "启动顺序: 应用后端 -> 应用后端健康检查")
	}
	fmt.Fprintln(writer, "退出行为: 任一开发进程退出或收到 Ctrl+C 时停止另一进程")
}

func fallback(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
