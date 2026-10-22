package checksumcmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/internal/artifact/checksum"
)

func newCreateCommand() *cobra.Command {
	var output string
	command := &cobra.Command{
		Use:   "create <file|dir>...",
		Short: "生成 sha256 checksum 清单",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			entries, err := checksum.Create(args)
			if err != nil {
				return err
			}
			output = strings.TrimSpace(output)
			if output == "" {
				for _, entry := range entries {
					if _, err := fmt.Fprintf(command.OutOrStdout(), "%s  %s\n", entry.Sum, filepath.ToSlash(entry.Path)); err != nil {
						return err
					}
				}
				return nil
			}
			entries, err = checksum.RelativeTo(filepath.Dir(output), entries)
			if err != nil {
				return err
			}
			if err := checksum.Write(output, entries); err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "checksum 已生成: %s\n", output)
			return err
		},
	}
	command.Flags().StringVar(&output, "output", "", "输出文件路径，未设置时输出到终端")
	return command
}
