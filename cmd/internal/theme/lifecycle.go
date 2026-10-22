package themecmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/cmd/internal/hostplugin"
	"github.com/zane-xin/sparq-tool/cmd/internal/hosttheme"
)

func newInstallCommand(use, action string) *cobra.Command {
	options := hosttheme.InstallOptions{}
	command := &cobra.Command{
		Use: use + " <code>", Short: "通过 Sparq 宿主" + action + "主题", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			result, err := hosttheme.ResolveAndInstall(command.Context(), args[0], options)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "主题%s操作完成: %s %s %s\n", action, result.Code, result.Status, result.Stage)
			return err
		},
	}
	hosttheme.BindInstallFlags(command, &options)
	return command
}

func newApplyCommand() *cobra.Command {
	options := hostplugin.HostOptions{}
	command := &cobra.Command{
		Use: "apply <code> <version>", Short: "应用已安装主题版本", Args: cobra.ExactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			theme, err := hosttheme.Apply(command.Context(), args[0], args[1], options)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "主题已应用: %s@%s\n", theme.Code, theme.Version)
			return err
		},
	}
	hostplugin.BindHostFlags(command, &options)
	return command
}

func newRemoveCommand() *cobra.Command {
	options := hostplugin.HostOptions{}
	command := &cobra.Command{
		Use: "remove <code> <version>", Short: "卸载未启用的主题版本", Args: cobra.ExactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			if err := hosttheme.Remove(command.Context(), args[0], args[1], options); err != nil {
				return err
			}
			_, err := fmt.Fprintf(command.OutOrStdout(), "主题已卸载: %s@%s\n", args[0], args[1])
			return err
		},
	}
	hostplugin.BindHostFlags(command, &options)
	return command
}

func newRollbackCommand() *cobra.Command {
	options := hostplugin.HostOptions{}
	command := &cobra.Command{
		Use: "rollback <code>", Short: "恢复应用该主题前的主题版本", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			result, err := hosttheme.Rollback(command.Context(), args[0], options)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "主题回滚完成: %s %s %s\n", result.Code, result.Status, result.Stage)
			return err
		},
	}
	hostplugin.BindHostFlags(command, &options)
	return command
}
