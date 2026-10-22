package packagecmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	releasebuild "github.com/zane-xin/sparq-tool/internal/release/build"
	"github.com/zane-xin/sparq-tool/internal/release/bundle"
)

func newAppCommand() *cobra.Command {
	options := bundle.Options{}
	targetValues := []string{}
	command := &cobra.Command{
		Use:   "app [workspace-root]",
		Short: "组装可直接安装的 app bundle",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			options.Root = "."
			if len(args) > 0 {
				options.Root = strings.TrimSpace(args[0])
			}
			targets, err := releasebuild.ParsePlatforms(targetValues)
			if err != nil {
				return err
			}
			options.Targets = targets
			result, err := bundle.Package(options)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(command.OutOrStdout(), "app bundle 已生成: %s\n", result.Directory); err != nil {
				return err
			}
			for _, artifact := range result.Artifacts {
				if _, err := fmt.Fprintf(command.OutOrStdout(), "  %s/%s: %s\n", artifact.Platform.OS, artifact.Platform.Arch, artifact.Path); err != nil {
					return err
				}
			}
			return nil
		},
	}
	command.Flags().StringVar(&options.Input, "input", "release/bin", "build app 二进制产物目录")
	command.Flags().StringVar(&options.Output, "output", "", "bundle 输出目录，默认 release/packages/<version>")
	command.Flags().StringVar(&options.Version, "version", "dev", "bundle 版本")
	command.Flags().StringSliceVar(&targetValues, "target", nil, "目标平台，可重复传入 os/arch 或使用 all；默认当前平台")
	command.Flags().StringVar(&options.Archive, "archive", bundle.ArchiveAuto, "归档格式：auto、none、tar.gz 或 zip")
	return command
}
