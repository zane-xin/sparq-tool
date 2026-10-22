package plugincmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/internal/plugin/buildpipeline"
)

func newBuildCommand() *cobra.Command {
	var target string
	command := &cobra.Command{
		Use:   "build [path]",
		Short: "构建并验证插件 Go 与前端产物",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}
			result, err := buildpipeline.Build(command.Context(), buildpipeline.Options{Dir: dir, Target: buildpipeline.Target(target)})
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintln(command.OutOrStdout(), "插件构建验证完成"); err != nil {
				return err
			}
			if result.GoTested {
				if _, err := fmt.Fprintln(command.OutOrStdout(), "Go 测试: 通过"); err != nil {
					return err
				}
				if result.BinaryPath == "" {
					_, err = fmt.Fprintln(command.OutOrStdout(), "Remote Runner: 未声明，已跳过")
				} else {
					_, err = fmt.Fprintf(command.OutOrStdout(), "Remote Runner: %s\n", result.BinaryPath)
				}
				if err != nil {
					return err
				}
			}
			if result.FrontendBuilt {
				if _, err = fmt.Fprintf(command.OutOrStdout(), "前端产物: %s（%s）\n", result.FrontendPath, result.FrontendPackageManager); err != nil {
					return err
				}
			}
			if result.ManifestPath != "" {
				_, err = fmt.Fprintf(command.OutOrStdout(), "完整 Manifest: %s\n", result.ManifestPath)
			}
			return nil
		},
	}
	command.Flags().StringVar(&target, "target", string(buildpipeline.TargetAll), "构建目标：all、go 或 frontend")
	return command
}
