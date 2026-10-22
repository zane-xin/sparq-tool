package plugincmd

import (
	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/cmd/internal/cloudcatalog"
)

// New 创建 plugin 命令。
func New() *cobra.Command {
	command := &cobra.Command{
		Use:   "plugin",
		Short: "创建、构建、发布和管理插件",
	}
	command.AddCommand(newInitCommand())
	command.AddCommand(newAddCommand())
	command.AddCommand(newBuildCommand())
	command.AddCommand(newPackageCommand())
	command.AddCommand(newPublishCommand())
	command.AddCommand(newDownloadCommand())
	resource := cloudcatalog.Resource{Kind: cloudcatalog.KindApplication, DisplayName: "插件", ArgumentName: "plugin"}
	command.AddCommand(cloudcatalog.NewListCommand(resource))
	command.AddCommand(cloudcatalog.NewSearchCommand(resource))
	command.AddCommand(cloudcatalog.NewInfoCommand(resource))
	return command
}
