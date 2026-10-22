package themecmd

import (
	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/cmd/internal/cloudcatalog"
)

// New 创建 theme 命令。
func New() *cobra.Command {
	command := &cobra.Command{
		Use:   "theme",
		Short: "发布、获取、安装和管理主题",
	}
	command.AddCommand(newPublishCommand())
	command.AddCommand(newDownloadCommand())
	command.AddCommand(newInstallCommand("install", "安装"))
	command.AddCommand(newInstallCommand("update", "更新"))
	command.AddCommand(newRemoveCommand())
	command.AddCommand(newApplyCommand())
	command.AddCommand(newRollbackCommand())
	resource := cloudcatalog.Resource{Kind: cloudcatalog.KindTemplate, DisplayName: "主题", ArgumentName: "theme"}
	command.AddCommand(cloudcatalog.NewListCommand(resource))
	command.AddCommand(cloudcatalog.NewSearchCommand(resource))
	command.AddCommand(cloudcatalog.NewInfoCommand(resource))
	return command
}
