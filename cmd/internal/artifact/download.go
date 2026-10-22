package artifactcmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/internal/artifact"
)

func newDownloadCommand() *cobra.Command {
	var output string
	var token string
	command := &cobra.Command{
		Use:   "download <source>",
		Short: "下载远程产物或复制本地产物",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			result, err := artifact.Download(artifact.DownloadOptions{
				Source: args[0],
				Output: output,
				Token:  token,
			})
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "产物已下载: %s\n", result.Output)
			return err
		},
	}
	command.Flags().StringVar(&output, "output", "", "输出文件路径，未设置时使用来源文件名")
	command.Flags().StringVar(&token, "token", "", "HTTP Bearer token")
	return command
}
