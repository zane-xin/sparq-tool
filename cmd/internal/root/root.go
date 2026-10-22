package rootcmd

import (
	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/cmd/internal/artifact"
	"github.com/zane-xin/sparq-tool/cmd/internal/auth"
	"github.com/zane-xin/sparq-tool/cmd/internal/build"
	"github.com/zane-xin/sparq-tool/cmd/internal/builder"
	"github.com/zane-xin/sparq-tool/cmd/internal/checksum"
	"github.com/zane-xin/sparq-tool/cmd/internal/dev"
	"github.com/zane-xin/sparq-tool/cmd/internal/doctor"
	"github.com/zane-xin/sparq-tool/cmd/internal/local"
	"github.com/zane-xin/sparq-tool/cmd/internal/manifest"
	"github.com/zane-xin/sparq-tool/cmd/internal/package"
	"github.com/zane-xin/sparq-tool/cmd/internal/plugin"
	"github.com/zane-xin/sparq-tool/cmd/internal/restart"
	"github.com/zane-xin/sparq-tool/cmd/internal/signature"
	"github.com/zane-xin/sparq-tool/cmd/internal/theme"
	"github.com/zane-xin/sparq-tool/cmd/internal/watch"
)

// New 创建 sparq-tool 根命令。
func New() *cobra.Command {
	command := &cobra.Command{
		Use:   "sparq-tool",
		Short: "Sparq 本地开发辅助工具",
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	command.SetHelpCommand(newHelpCommand(command))
	command.AddCommand(artifactcmd.New())
	command.AddCommand(authcmd.New())
	command.AddCommand(buildcmd.New())
	command.AddCommand(buildercmd.New())
	command.AddCommand(checksumcmd.New())
	command.AddCommand(devcmd.New())
	command.AddCommand(doctorcmd.New())
	command.AddCommand(localcmd.New())
	command.AddCommand(manifestcmd.New())
	command.AddCommand(packagecmd.New())
	command.AddCommand(plugincmd.New())
	command.AddCommand(restartcmd.New())
	command.AddCommand(signaturecmd.New())
	command.AddCommand(themecmd.New())
	command.AddCommand(watchcmd.New())
	return command
}

func newHelpCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "help [command]",
		Short: "查看命令帮助",
		Args:  cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, args []string) error {
			target, _, err := root.Find(args)
			if err != nil {
				return err
			}
			return target.Help()
		},
	}
}
