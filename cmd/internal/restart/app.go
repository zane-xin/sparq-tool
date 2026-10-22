package restartcmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/cmd/internal/placeholder"
	"github.com/zane-xin/sparq-tool/internal/process"
)

func newAppCommand() *cobra.Command {
	options := process.Options{}
	command := &cobra.Command{
		Use:   "app",
		Short: "重启宿主 app",
		RunE: func(command *cobra.Command, args []string) error {
			if process.IsReservedDriver(options.Driver) {
				return placeholder.Print(command.OutOrStdout(), "restart app --driver "+options.Driver)
			}
			ctx, cancel := context.WithTimeout(context.Background(), options.Timeout)
			defer cancel()
			options.Stdin = command.InOrStdin()
			options.Stdout = command.OutOrStdout()
			options.Stderr = command.ErrOrStderr()
			if err := process.Run(ctx, options); err != nil {
				return err
			}
			_, err := fmt.Fprintln(command.OutOrStdout(), "app 重启命令执行完成")
			return err
		},
	}
	bindFlags(command, &options)
	return command
}

func bindFlags(command *cobra.Command, options *process.Options) {
	command.Flags().StringVar(&options.Driver, "driver", "command", "重启驱动：command、systemd、docker-compose")
	command.Flags().StringVar(&options.Command, "command", "", "重启命令")
	command.Flags().DurationVar(&options.Timeout, "timeout", 30*time.Second, "重启命令超时时间")
}
