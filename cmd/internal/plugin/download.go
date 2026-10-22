package plugincmd

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/internal/artifact"
	remoteauth "github.com/zane-xin/sparq-tool/internal/remote/auth"
	remotemarketplace "github.com/zane-xin/sparq-tool/internal/remote/marketplace"
)

const defaultDownloadTimeout = 15 * time.Minute

type downloadOptions struct {
	configPath string
	profile    string
	version    string
	mode       string
	targetOS   string
	targetArch string
	output     string
	timeout    time.Duration
}

func newDownloadCommand() *cobra.Command {
	options := downloadOptions{version: "latest", mode: "local", targetOS: runtime.GOOS, targetArch: runtime.GOARCH, timeout: defaultDownloadTimeout}
	command := &cobra.Command{
		Use:   "download <code>",
		Short: "下载已授权插件的正式交付包",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if err := validateDownloadOptions(&options); err != nil {
				return err
			}
			session, profile, _, err := remoteauth.ResolveSession(options.configPath, options.profile)
			if err != nil {
				return err
			}
			if session.ExpiresAt > 0 && time.Now().Unix() >= session.ExpiresAt {
				return fmt.Errorf("认证 profile %q 已过期，请重新运行 sparq-tool auth login", profile)
			}
			kind := remotemarketplace.ArtifactKindLocalSourceCapsule
			target := remotemarketplace.DownloadTarget{}
			switch options.mode {
			case "remote":
				kind = remotemarketplace.ArtifactKindManagedRemote
				target.OS = options.targetOS
				target.Arch = options.targetArch
			case "frontend":
				kind = remotemarketplace.ArtifactKindFrontendBundle
			}
			client := &http.Client{Timeout: options.timeout}
			resolution, err := remotemarketplace.NewClient(client).ResolveApplicationDownload(command.Context(), remotemarketplace.ResolveApplicationDownloadInput{
				Session: session, ApplicationCode: strings.TrimSpace(args[0]), Version: options.version,
				ArtifactKind: kind, Target: target,
			})
			if err != nil {
				if remotemarketplace.IsUnauthorized(err) {
					return fmt.Errorf("Sparq Cloud 登录态无效，请重新运行 sparq-tool auth login: %w", err)
				}
				return err
			}
			if !resolution.Authorization.Allowed {
				return fmt.Errorf("Application %s 尚未获得有效授权: %s", resolution.Application.Code, resolution.Authorization.Reason)
			}
			if !resolution.Artifact.Available {
				return fmt.Errorf("Application %s %s 暂无可下载的 %s Artifact: %s", resolution.Application.Code, resolution.Application.Version, options.mode, resolution.Artifact.Reason)
			}
			output, err := downloadOutput(options.output, resolution.Artifact.Filename)
			if err != nil {
				return err
			}
			downloaded, err := artifact.DownloadVerified(artifact.VerifiedDownloadOptions{
				Source: resolution.Artifact.DownloadURL, Output: output,
				ExpectedSHA256: resolution.Artifact.SHA256, ExpectedSize: resolution.Artifact.SizeBytes,
				Context: command.Context(), Client: client,
			})
			if err != nil {
				return fmt.Errorf("下载 Application %s %s 失败: %w", resolution.Application.Code, resolution.Application.Version, err)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Application 交付包下载完成\nApplication: %s\n版本: %s\n模式: %s\n文件: %s\nSHA-256: %s\n签名公钥: %s\n", resolution.Application.Code, resolution.Application.Version, options.mode, downloaded.Output, resolution.Artifact.SHA256, resolution.Artifact.PublicKeyID)
			return err
		},
	}
	command.Flags().StringVar(&options.configPath, "config", "", "认证配置路径，默认使用 "+remoteauth.DefaultConfigPathDisplay)
	command.Flags().StringVar(&options.profile, "profile", "", "认证 profile，默认使用当前 profile")
	command.Flags().StringVar(&options.version, "version", "latest", "插件版本，默认下载当前发布版本")
	command.Flags().StringVar(&options.mode, "mode", "local", "交付模式：local、remote 或 frontend")
	command.Flags().StringVar(&options.targetOS, "os", runtime.GOOS, "Remote 二进制目标操作系统")
	command.Flags().StringVar(&options.targetArch, "arch", runtime.GOARCH, "Remote 二进制目标架构")
	command.Flags().StringVarP(&options.output, "output", "o", "", "输出文件或目录，默认使用服务端文件名")
	command.Flags().DurationVar(&options.timeout, "timeout", defaultDownloadTimeout, "下载请求超时时间")
	return command
}

func validateDownloadOptions(options *downloadOptions) error {
	options.version = strings.TrimSpace(options.version)
	options.mode = strings.ToLower(strings.TrimSpace(options.mode))
	options.targetOS = strings.ToLower(strings.TrimSpace(options.targetOS))
	options.targetArch = strings.ToLower(strings.TrimSpace(options.targetArch))
	if options.version == "" || options.timeout <= 0 || options.mode != "local" && options.mode != "remote" && options.mode != "frontend" {
		return fmt.Errorf("版本、交付模式或下载超时时间无效")
	}
	if options.mode == "remote" && (options.targetOS == "" || options.targetArch == "") {
		return fmt.Errorf("Remote 下载必须指定目标 OS 和 Arch")
	}
	return nil
}

func downloadOutput(value, filename string) (string, error) {
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "" || filename == "." || filename == string(filepath.Separator) {
		return "", fmt.Errorf("Marketplace 未返回有效的 Artifact 文件名")
	}
	output := strings.TrimSpace(value)
	if output == "" {
		return filename, nil
	}
	info, err := os.Stat(output)
	if err == nil && info.IsDir() {
		return filepath.Join(output, filename), nil
	}
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("检查输出路径 %s 失败: %w", output, err)
	}
	return output, nil
}
