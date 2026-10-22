package themecmd

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/internal/artifact"
	remoteauth "github.com/zane-xin/sparq-tool/internal/remote/auth"
	"github.com/zane-xin/sparq-tool/internal/remote/marketplace"
)

func newDownloadCommand() *cobra.Command {
	var configPath, profile, version, output string
	var timeout time.Duration
	command := &cobra.Command{
		Use: "download <code>", Short: "下载已授权主题包", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if strings.TrimSpace(version) == "" || timeout <= 0 {
				return fmt.Errorf("主题版本或下载超时时间无效")
			}
			session, resolvedProfile, _, err := remoteauth.ResolveSession(configPath, profile)
			if err != nil {
				return err
			}
			if session.ExpiresAt > 0 && time.Now().Unix() >= session.ExpiresAt {
				return fmt.Errorf("认证 profile %q 已过期，请重新运行 sparq-tool auth login", resolvedProfile)
			}
			client := &http.Client{Timeout: timeout}
			resolution, err := marketplace.NewClient(client).ResolveTemplateDownload(command.Context(), marketplace.ResolveTemplateDownloadInput{
				Session: session, TemplateCode: strings.TrimSpace(args[0]), Version: version,
			})
			if err != nil {
				return err
			}
			if !resolution.Authorization.Allowed {
				return fmt.Errorf("Template %s 尚未获得有效授权: %s", resolution.Template.Code, resolution.Authorization.Reason)
			}
			if !resolution.Artifact.Available {
				return fmt.Errorf("Template %s %s 暂无可下载包: %s", resolution.Template.Code, resolution.Template.Version, resolution.Artifact.Reason)
			}
			target, err := themeDownloadOutput(output, resolution.Artifact.Filename)
			if err != nil {
				return err
			}
			downloaded, err := artifact.DownloadVerified(artifact.VerifiedDownloadOptions{
				Source: resolution.Artifact.DownloadURL, Output: target, ExpectedSHA256: resolution.Artifact.SHA256,
				ExpectedSize: resolution.Artifact.SizeBytes, Context: command.Context(), Client: client,
			})
			if err != nil {
				return fmt.Errorf("下载 Template 失败: %w", err)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Template 下载完成\nTemplate: %s\n版本: %s\n文件: %s\nSHA-256: %s\n", resolution.Template.Code, resolution.Template.Version, downloaded.Output, resolution.Artifact.SHA256)
			return err
		},
	}
	command.Flags().StringVar(&configPath, "config", "", "认证配置路径")
	command.Flags().StringVar(&profile, "profile", "", "认证 profile")
	command.Flags().StringVar(&version, "version", "latest", "主题版本")
	command.Flags().StringVarP(&output, "output", "o", "", "输出文件或目录")
	command.Flags().DurationVar(&timeout, "timeout", 15*time.Minute, "下载请求超时时间")
	return command
}

func themeDownloadOutput(value, filename string) (string, error) {
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "" || filename == "." {
		return "", fmt.Errorf("Marketplace 未返回有效的主题包文件名")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return filename, nil
	}
	if info, err := os.Stat(value); err == nil && info.IsDir() {
		return filepath.Join(value, filename), nil
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return value, nil
}
