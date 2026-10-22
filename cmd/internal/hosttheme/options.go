package hosttheme

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/cmd/internal/hostplugin"
	remoteauth "github.com/zane-xin/sparq-tool/internal/remote/auth"
	"github.com/zane-xin/sparq-tool/internal/remote/host"
	"github.com/zane-xin/sparq-tool/internal/remote/marketplace"
)

type InstallOptions struct {
	Host       hostplugin.HostOptions
	ConfigPath string
	Profile    string
	Version    string
	Apply      bool
}

func BindInstallFlags(command *cobra.Command, options *InstallOptions) {
	hostplugin.BindHostFlags(command, &options.Host)
	command.Flags().StringVar(&options.ConfigPath, "config", "", "Cloud 认证配置路径")
	command.Flags().StringVar(&options.Profile, "profile", "", "Cloud 认证 profile")
	command.Flags().StringVar(&options.Version, "version", "latest", "主题版本")
	command.Flags().BoolVar(&options.Apply, "apply", false, "安装完成后立即应用主题")
}

func ResolveAndInstall(ctx context.Context, code string, options InstallOptions) (host.Operation, error) {
	code = strings.TrimSpace(code)
	options.Version = strings.TrimSpace(options.Version)
	if code == "" || options.Version == "" || options.Host.Timeout <= 0 {
		return host.Operation{}, fmt.Errorf("主题 code、版本和等待超时时间必须有效")
	}
	session, _, _, err := remoteauth.ResolveSession(options.ConfigPath, options.Profile)
	if err != nil {
		return host.Operation{}, err
	}
	credentials := credentials(options.Host)
	client := host.NewClient(http.DefaultClient)
	manifest, err := client.BuildManifest(ctx, credentials)
	if err != nil {
		return host.Operation{}, err
	}
	grant, err := marketplace.NewClient(http.DefaultClient).CreateTemplateInstallGrant(ctx, session, code, options.Version, manifest.InstanceID)
	if err != nil {
		return host.Operation{}, err
	}
	operation, err := client.InstallTheme(ctx, credentials, grant.Grant, options.Apply)
	if err != nil {
		return host.Operation{}, err
	}
	waitContext, cancel := context.WithTimeout(ctx, options.Host.Timeout)
	defer cancel()
	return client.WaitOperation(waitContext, credentials, operation)
}

func Apply(ctx context.Context, code, version string, options hostplugin.HostOptions) (host.Theme, error) {
	return host.NewClient(http.DefaultClient).ActivateTheme(ctx, credentials(options), code, version)
}

func Remove(ctx context.Context, code, version string, options hostplugin.HostOptions) error {
	return host.NewClient(http.DefaultClient).RemoveTheme(ctx, credentials(options), code, version)
}

func Rollback(ctx context.Context, code string, options hostplugin.HostOptions) (host.Operation, error) {
	client := host.NewClient(http.DefaultClient)
	operation, err := client.RollbackTheme(ctx, credentials(options), code)
	if err != nil {
		return host.Operation{}, err
	}
	waitContext, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	return client.WaitOperation(waitContext, credentials(options), operation)
}

func credentials(options hostplugin.HostOptions) host.Credentials {
	return host.Credentials{Endpoint: options.URL, Session: options.Session, CSRF: options.CSRF}
}
