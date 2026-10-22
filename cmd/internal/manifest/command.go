package manifestcmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/internal/plugin/manifest"
)

// New 创建 manifest 命令。
func New() *cobra.Command {
	command := &cobra.Command{
		Use:   "manifest",
		Short: "管理插件清单",
	}
	command.AddCommand(newGenerateCommand())
	command.AddCommand(newValidateCommand())
	return command
}

func newGenerateCommand() *cobra.Command {
	var file string
	var output string
	var packageName string
	var artifact string
	var check bool
	command := &cobra.Command{
		Use:   "generate [plugin.yaml]",
		Short: "由插件清单生成 Go Manifest 入口",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			path, err := resolveManifestPath(file, args)
			if err != nil {
				return err
			}
			result, err := manifest.GenerateGo(manifest.GoGenerateOptions{
				ManifestPath: path,
				PackageName:  packageName,
				Output:       output,
				ArtifactPath: artifact,
			})
			if err != nil {
				return err
			}
			if check {
				if err := manifest.CheckGo(result); err != nil {
					return err
				}
				_, err = fmt.Fprintf(command.OutOrStdout(), "插件 Manifest 生成文件有效: %s\n", result.Output)
				return err
			}
			if err := manifest.WriteGo(result); err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "插件 Manifest 已生成: %s\n", result.Output)
			return err
		},
	}
	command.Flags().StringVarP(&file, "file", "f", "", "插件清单文件路径")
	command.Flags().StringVarP(&output, "output", "o", "", "生成文件路径，默认写入清单目录的 manifest_gen.go")
	command.Flags().StringVar(&packageName, "package", "", "生成文件的 Go package，默认从插件根目录检测")
	command.Flags().StringVar(&artifact, "artifact", "", "前端 artifact.json 路径，默认从插件根目录或 frontend 目录查找")
	command.Flags().BoolVar(&check, "check", false, "只检查现有生成文件是否与 plugin.yaml 一致")
	return command
}

func newValidateCommand() *cobra.Command {
	var file string
	command := &cobra.Command{
		Use:   "validate [plugin.yaml]",
		Short: "校验插件清单",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			path, err := resolveManifestPath(file, args)
			if err != nil {
				return err
			}
			item, err := manifest.Load(path)
			if err != nil {
				return fmt.Errorf("校验插件清单 %s 失败: %w", path, err)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "插件清单有效: %s (%s %s)\n", path, item.Metadata.Code, item.Metadata.Version)
			return err
		},
	}
	command.Flags().StringVarP(&file, "file", "f", "", "插件清单文件路径")
	return command
}

func resolveManifestPath(file string, args []string) (string, error) {
	if file != "" && len(args) > 0 {
		return "", fmt.Errorf("不能同时通过位置参数和 --file 指定插件清单")
	}
	if file != "" {
		return file, nil
	}
	if len(args) > 0 {
		return args[0], nil
	}
	return manifest.DefaultFileName, nil
}
