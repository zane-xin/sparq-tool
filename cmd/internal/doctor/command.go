package doctorcmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/internal/plugin/manifest"
	"github.com/zane-xin/sparq-tool/internal/workspace/doctor"
)

// New 创建 doctor 命令。
func New() *cobra.Command {
	var manifestPath string
	command := &cobra.Command{
		Use:   "doctor",
		Short: "检查本地开发环境",
		RunE: func(command *cobra.Command, _ []string) error {
			checks := doctor.Run(manifestPath)
			for _, check := range checks {
				fmt.Fprintf(command.OutOrStdout(), "[%s] %s: %s\n", check.Status, check.Name, check.Detail)
			}
			if doctor.HasFailed(checks) {
				return errors.New("doctor 检查失败")
			}
			return nil
		},
	}
	command.Flags().StringVar(&manifestPath, "manifest", manifest.DefaultFileName, "插件清单文件路径")
	return command
}
