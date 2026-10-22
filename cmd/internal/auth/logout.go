package authcmd

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
	remoteauth "github.com/zane-xin/sparq-tool/internal/remote/auth"
)

func newLogoutCommand() *cobra.Command {
	options := credentialOptions{timeout: defaultRequestTimeout}
	var localOnly bool
	command := &cobra.Command{
		Use:   "logout",
		Short: "退出 Sparq Cloud 账号",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			session, profile, _, err := remoteauth.ResolveSession(options.configPath, options.profile)
			if err != nil {
				return err
			}
			if !localOnly {
				client := remoteauth.NewClient(&http.Client{Timeout: options.timeout})
				if err := client.Logout(command.Context(), session); err != nil && !remoteauth.IsUnauthorized(err) {
					return err
				}
			}
			removedProfile, path, err := remoteauth.RemoveSession(options.configPath, profile)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "已退出 Sparq Cloud\nprofile: %s\nconfig: %s\n", removedProfile, path)
			return err
		},
	}
	options.bindFlags(command.Flags())
	command.Flags().BoolVar(&localOnly, "local", false, "仅删除本地登录凭据，不请求远端撤销 Session")
	return command
}
