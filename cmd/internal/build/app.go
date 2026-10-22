package buildcmd

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	apprelease "github.com/zane-xin/sparq-tool/internal/release/app"
	releasebuild "github.com/zane-xin/sparq-tool/internal/release/build"
	remoteauth "github.com/zane-xin/sparq-tool/internal/remote/auth"
	remoterelease "github.com/zane-xin/sparq-tool/internal/remote/release"
	"github.com/zane-xin/sparq-tool/internal/toolchain"
)

const appUploadTimeout = 15 * time.Minute

const (
	appSigningKeyIDEnv   = "SPARQ_RELEASE_SIGNING_KEY_ID"
	appSigningKeyPathEnv = "SPARQ_RELEASE_SIGNING_PRIVATE_KEY"
)

type appPublishOptions struct {
	upload bool
}

func newAppCommand() *cobra.Command {
	options := releasebuild.AppOptions{}
	publish := appPublishOptions{}
	var configPath string
	var environment string
	var gitHash string
	var goCommand, frontendCommand string
	var goArgs, frontendArgs []string
	targetValues := []string{}
	command := &cobra.Command{
		Use:   "app [workspace-root]",
		Short: "按配置构建应用组件",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			root := "."
			if len(args) > 0 {
				root = strings.TrimSpace(args[0])
			}
			if strings.TrimSpace(configPath) == "" {
				configPath = filepath.Join(root, "build.yaml")
			}
			if len(args) > 0 && command.Flags().Changed("config") {
				return fmt.Errorf("使用 --config 构建应用时不能同时提供 workspace-root")
			}
			resolvedConfig, err := releasebuild.ResolveBuildConfigPath(configPath, environment)
			if err != nil {
				return err
			}
			config, err := releasebuild.LoadBuildConfig(resolvedConfig)
			if err != nil {
				return err
			}
			options.Config = &config
			if !command.Flags().Changed("version") {
				options.Version = ""
			}
			options.GitHash = gitHash
			targets, err := releasebuild.ParsePlatforms(targetValues)
			if err != nil {
				return err
			}
			options.Targets = targets
			options.Toolchain = toolchain.Toolchain{
				Go:       toolchain.Spec{Command: goCommand, Args: goArgs},
				Frontend: toolchain.Spec{Command: frontendCommand, Args: frontendArgs},
			}
			if options.Version == "" {
				options.Version = config.Version
			}
			if err := prepareAppPublishOptions(&options, publish); err != nil {
				return err
			}

			var releaseOutput, changelog, signingKey, signingKeyID string
			var session remoteauth.Session
			if publish.upload {
				var profile string
				session, profile, _, err = remoteauth.ResolveSession("", "")
				if err != nil {
					return err
				}
				if session.ExpiresAt > 0 && time.Now().Unix() >= session.ExpiresAt {
					return fmt.Errorf("认证 profile %q 已过期，请重新运行 sparq-tool auth login", profile)
				}
				root := config.BaseDir()
				changelog = filepath.Join(root, "CHANGELOG.md")
				if info, err := os.Stat(changelog); err != nil || !info.Mode().IsRegular() {
					if err == nil {
						err = fmt.Errorf("不是普通文件")
					}
					return fmt.Errorf("检查更新日志 %s 失败: %w", changelog, err)
				}
				releaseOutput = filepath.Join(root, "release", "updates", options.Version)
				if _, err := os.Stat(releaseOutput); err == nil {
					return fmt.Errorf("发行包目录已存在: %s", releaseOutput)
				} else if !errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("检查发行包目录 %s 失败: %w", releaseOutput, err)
				}
				signingKeyID = strings.TrimSpace(os.Getenv(appSigningKeyIDEnv))
				if signingKeyID == "" {
					return fmt.Errorf("上传官方版本必须设置环境变量 %s", appSigningKeyIDEnv)
				}
				signingKey = strings.TrimSpace(os.Getenv(appSigningKeyPathEnv))
				if signingKey == "" {
					return fmt.Errorf("上传官方版本必须设置环境变量 %s", appSigningKeyPathEnv)
				}
				if !filepath.IsAbs(signingKey) {
					signingKey = filepath.Join(root, signingKey)
				}
				if info, err := os.Stat(signingKey); err != nil || !info.Mode().IsRegular() {
					if err == nil {
						err = fmt.Errorf("不是普通文件")
					}
					return fmt.Errorf("检查发行签名私钥 %s 失败: %w", signingKey, err)
				}
			}

			result, err := releasebuild.Build(options)
			if err != nil {
				return err
			}
			if _, err = fmt.Fprintln(command.OutOrStdout(), "app 构建完成:"); err != nil {
				return err
			}
			if result.AdminPath != "" {
				if _, err = fmt.Fprintf(command.OutOrStdout(), "  Admin Shell: %s\n", result.AdminPath); err != nil {
					return err
				}
			}
			for _, runtime := range result.RuntimeBinaries {
				if _, err = fmt.Fprintf(command.OutOrStdout(), "  Runtime %s/%s: %s\n", runtime.Platform.OS, runtime.Platform.Arch, runtime.Path); err != nil {
					return err
				}
			}
			for _, updater := range result.UpdaterBinaries {
				if _, err = fmt.Fprintf(command.OutOrStdout(), "  Updater %s/%s: %s\n", updater.Platform.OS, updater.Platform.Arch, updater.Path); err != nil {
					return err
				}
			}
			if !publish.upload {
				return nil
			}

			runtimes := make([]apprelease.RuntimeInput, 0, len(result.RuntimeBinaries))
			for _, runtime := range result.RuntimeBinaries {
				runtimes = append(runtimes, apprelease.RuntimeInput{TargetOS: runtime.Platform.OS, TargetArch: runtime.Platform.Arch, BinaryPath: runtime.Path})
			}
			updaters := make([]apprelease.UpdaterInput, 0, len(result.UpdaterBinaries))
			for _, updater := range result.UpdaterBinaries {
				updaters = append(updaters, apprelease.UpdaterInput{TargetOS: updater.Platform.OS, TargetArch: updater.Platform.Arch, BinaryPath: updater.Path})
			}
			packageResult, err := apprelease.Package(apprelease.PackageOptions{
				OutputPath: releaseOutput, Version: options.Version, Runtimes: runtimes, Updaters: updaters,
				AdminPath: result.AdminArtifactDir, ChangelogPath: changelog, PublicBaseURL: session.Endpoint,
				SigningKeyID: signingKeyID, SigningKeyPath: signingKey,
			})
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(command.OutOrStdout(), "app 更新发行包已生成: %s\n", packageResult.Directory); err != nil {
				return err
			}
			uploads := make([]remoterelease.RuntimeUpload, 0, len(packageResult.Runtimes))
			for _, runtime := range packageResult.Runtimes {
				uploads = append(uploads, remoterelease.RuntimeUpload{TargetOS: runtime.TargetOS, TargetArch: runtime.TargetArch, Path: runtime.Path})
			}
			updaterUploads := make([]remoterelease.UpdaterUpload, 0, len(packageResult.Updaters))
			for _, updater := range packageResult.Updaters {
				updaterUploads = append(updaterUploads, remoterelease.UpdaterUpload{TargetOS: updater.TargetOS, TargetArch: updater.TargetArch, Path: updater.Path})
			}
			client := remoterelease.NewClient(&http.Client{Timeout: appUploadTimeout})
			uploaded, err := client.Publish(command.Context(), remoterelease.PublishInput{
				Session: session, ManifestPath: packageResult.ManifestPath, Runtimes: uploads, Updaters: updaterUploads,
				AdminPath: packageResult.AdminPath, ChangelogPath: packageResult.ChangelogPath,
			})
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "app 正式版本已上传: %s\n", uploaded.Version)
			return err
		},
	}
	command.Flags().StringSliceVar(&targetValues, "target", nil, "目标平台，可重复传入 os/arch 或使用 all；默认当前平台")
	command.Flags().StringVar(&options.Version, "version", "dev", "构建版本；dev 表示开发构建")
	command.Flags().StringVar(&gitHash, "git-hash", "", "覆盖构建 Git Hash；默认从应用路径自动读取")
	command.Flags().StringVar(&environment, "env", "", "构建环境，加载 build.<env>.yaml")
	command.Flags().BoolVar(&publish.upload, "upload", false, "构建完整正式发行矩阵并上传到 Sparq Website")
	command.Flags().BoolVar(&options.Debug, "debug", false, "保留调试信息和符号，Admin 生成 source map")
	command.Flags().StringVar(&options.Tags, "tags", "production", "Go 构建标签；默认 production，传空字符串可构建开发版本")
	command.Flags().StringVar(&configPath, "config", "", "统一应用构建配置文件路径；可配合 --env 加载环境配置")
	command.Flags().StringVar(&goCommand, "go-command", "", "临时覆盖 Go 可执行文件")
	command.Flags().StringSliceVar(&goArgs, "go-arg", nil, "临时覆盖 go build 参数，可重复传入")
	command.Flags().StringVar(&frontendCommand, "frontend-command", "", "临时覆盖前端工具可执行文件")
	command.Flags().StringSliceVar(&frontendArgs, "frontend-arg", nil, "临时覆盖前端工具参数，可重复传入")
	return command
}

func prepareAppPublishOptions(options *releasebuild.AppOptions, publish appPublishOptions) error {
	if !publish.upload {
		return nil
	}
	version := strings.TrimSpace(strings.TrimPrefix(options.Version, "v"))
	if version == "" || version == "dev" {
		return fmt.Errorf("上传官方版本必须通过 --version 指定正式版本")
	}
	if options.Debug {
		return fmt.Errorf("上传官方版本不能使用 --debug")
	}
	if len(options.Targets) == 0 {
		options.Targets = releasebuild.DefaultReleasePlatforms()
	}
	if !hasCompleteReleasePlatforms(options.Targets) {
		return fmt.Errorf("上传官方版本必须通过 --target all 构建完整的六平台矩阵")
	}
	options.Version = version
	return nil
}

func hasCompleteReleasePlatforms(platforms []releasebuild.Platform) bool {
	want := make(map[releasebuild.Platform]bool, len(releasebuild.DefaultReleasePlatforms()))
	for _, platform := range releasebuild.DefaultReleasePlatforms() {
		want[platform] = false
	}
	if len(platforms) != len(want) {
		return false
	}
	for _, platform := range platforms {
		if _, ok := want[platform]; !ok || want[platform] {
			return false
		}
		want[platform] = true
	}
	return true
}
