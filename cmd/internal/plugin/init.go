package plugincmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/internal/plugin/scaffold"
)

func newInitCommand() *cobra.Command {
	var code string
	var pathValue string
	var title string
	var module string
	var componentsValue string
	var runtimeValue string
	var withFrontend bool
	command := &cobra.Command{
		Use:   "init",
		Short: "在指定目录初始化 Sparq 插件",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			targetPath := strings.TrimSpace(pathValue)
			if targetPath == "" {
				targetPath = filepath.Join(".", code)
			}
			componentSelection := componentsValue
			if withFrontend && componentSelection == "go" {
				componentSelection = "go,frontend"
			}
			components, err := scaffold.ParseComponents(componentSelection)
			if err != nil {
				return err
			}
			runtimes, err := scaffold.ParseRuntimes(runtimeValue)
			if err != nil {
				return err
			}
			target, err := scaffold.Plugin(scaffold.PluginOptions{
				Name: code, Title: title, Dir: targetPath, Module: module, Runtimes: runtimes, Components: components,
			})
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "插件初始化完成: %s\n验证命令（在插件目录执行）:\n  go test ./...\n", target)
			if err != nil {
				return err
			}
			if _, statErr := os.Stat(filepath.Join(target, scaffold.DevExampleFileName)); statErr == nil {
				_, err = fmt.Fprintf(
					command.OutOrStdout(),
					"联调配置模板: %s\n按注释调整并另存为 sparq.dev.yaml 后执行:\n  sparq-tool dev --file sparq.dev.yaml\n",
					scaffold.DevExampleFileName,
				)
			}
			return err
		},
	}
	command.Flags().StringVar(&code, "code", "", "插件 code，必须使用小写字母、数字和连字符")
	command.Flags().StringVar(&pathValue, "path", "", "插件目录，默认使用当前目录下的 ./<code>")
	command.Flags().StringVar(&title, "name", "", "插件显示名称，默认使用 code")
	command.Flags().StringVar(&module, "module", "", "Go module path，默认使用 sparq.xin/sparq-plugins-{code}")
	command.Flags().StringVar(&componentsValue, "with", "go", "初始化组件：go 或 go,frontend")
	command.Flags().BoolVar(&withFrontend, "with-frontend", false, "同时生成插件前端工程")
	command.Flags().StringVar(&runtimeValue, "runtime", "both", "Go Runtime：local、http、local,http 或 both")
	if err := command.MarkFlagRequired("code"); err != nil {
		panic(err)
	}
	return command
}
