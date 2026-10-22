package themecmd

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	pluginmanifest "github.com/zane-xin/sparq-tool/internal/plugin/manifest"
	"github.com/zane-xin/sparq-tool/internal/publishstate"
	remoteauth "github.com/zane-xin/sparq-tool/internal/remote/auth"
	"github.com/zane-xin/sparq-tool/internal/remote/marketplace"
	"github.com/zane-xin/sparq-tool/internal/theme/sourcepack"
	"golang.org/x/term"
)

const themePublishProgressSteps = 4

type publishOptions struct {
	configPath, profile, message, messageFile string
	sourceRef, commitSHA, pipelineID          string
	idempotencyKey                            string
	bump, version                             string
	keepVersion                               bool
	timeout                                   time.Duration
}

func newPublishCommand() *cobra.Command {
	return newPublishCommandWithTerminalCheck(themePublishInputIsTerminal)
}

func newPublishCommandWithTerminalCheck(isTerminal func(io.Reader) bool) *cobra.Command {
	options := publishOptions{timeout: 5 * time.Minute}
	command := &cobra.Command{
		Use: "publish [path]", Short: "打包主题并提交 Sparq Cloud 审核", Args: cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			progress := themePublishProgress{writer: command.ErrOrStderr()}
			sourceDir := "."
			if len(args) > 0 {
				sourceDir = args[0]
			}
			message, err := themeVersionMessage(options.message, options.messageFile)
			if err != nil {
				return err
			}
			if err := validateThemePublishOptions(options, message); err != nil {
				return err
			}
			if !options.keepVersion && strings.TrimSpace(options.version) == "" && strings.TrimSpace(options.bump) == "" && !isTerminal(command.InOrStdin()) {
				return fmt.Errorf("当前环境无法交互选择发布版本，请使用 --bump patch、--version <version> 或 --keep-version")
			}
			session, profile, _, err := remoteauth.ResolveSession(options.configPath, options.profile)
			if err != nil {
				return err
			}
			if session.ExpiresAt > 0 && time.Now().Unix() >= session.ExpiresAt {
				return fmt.Errorf("认证 profile %q 已过期，请重新运行 sparq-tool auth login", profile)
			}
			if err := progress.start(1, "准备发布版本"); err != nil {
				return err
			}
			state, resumed, err := resolveThemePublishState(command, sourceDir, session.Endpoint, profile, options, isTerminal)
			if err != nil {
				return err
			}
			if err := applyThemePublishVersion(command, sourceDir, state.OriginalVersion, state.TargetVersion); err != nil {
				return err
			}
			versionDetail := state.TargetVersion
			if resumed {
				versionDetail += "（恢复发布）"
			}
			if err := progress.done(versionDetail); err != nil {
				return err
			}
			temporary, err := os.MkdirTemp("", "sparq-theme-publish-*")
			if err != nil {
				return err
			}
			defer os.RemoveAll(temporary)
			if err := progress.start(2, "校验并打包主题"); err != nil {
				return err
			}
			packed, err := sourcepack.Create(sourcepack.Options{SourceDir: sourceDir, OutputDir: temporary})
			if err != nil {
				return err
			}
			if packed.Version != state.TargetVersion {
				return fmt.Errorf("主题包版本 %s 与发布版本 %s 不一致", packed.Version, state.TargetVersion)
			}
			if err := progress.done(fmt.Sprintf("%s（%d 个文件，%d bytes）", filepath.Base(packed.ArchivePath), packed.FileCount, packed.SizeBytes)); err != nil {
				return err
			}
			if state.Code != "" && state.Code != packed.Code {
				return fmt.Errorf("发布恢复状态属于主题 %q，当前主题包为 %q", state.Code, packed.Code)
			}
			if state.SourceSHA256 != "" && !strings.EqualFold(state.SourceSHA256, packed.SHA256) {
				return fmt.Errorf("本地主题内容已变化，不能继续 source_upload_id=%d；请恢复原内容或清理对应发布状态后重新发布", state.SourceUploadID)
			}
			state.Code = packed.Code
			state.SourceSHA256 = packed.SHA256
			if err := publishstate.Save(state); err != nil {
				return err
			}
			if err := progress.start(3, "创建或恢复源码会话"); err != nil {
				return err
			}
			client := marketplace.NewClient(&http.Client{Timeout: options.timeout})
			upload, err := client.UploadTemplateSource(command.Context(), marketplace.UploadSourceInput{
				Session: session, ApplicationCode: packed.Code, ArchivePath: packed.ArchivePath, Changelog: message,
				SourceRef: strings.TrimSpace(options.sourceRef), CommitSHA: strings.TrimSpace(options.commitSHA),
				PipelineID: strings.TrimSpace(options.pipelineID), IdempotencyKey: state.IdempotencyKey,
			})
			if err != nil {
				if marketplace.IsUnauthorized(err) {
					return fmt.Errorf("Sparq Cloud 登录态无效，请重新登录: %w", err)
				}
				return fmt.Errorf("提交主题 %q 版本 %s 的审核包失败: %w", packed.Code, packed.Version, err)
			}
			if upload.Version != packed.Version || !strings.EqualFold(upload.SHA256, packed.SHA256) {
				return fmt.Errorf("平台登记结果与主题包不一致: 本地 %s/%s，平台 %s/%s", packed.Version, packed.SHA256, upload.Version, upload.SHA256)
			}
			if state.SourceUploadID != 0 && state.SourceUploadID != upload.ID {
				return fmt.Errorf("平台返回的主题源码会话 %d 与恢复状态 %d 不一致", upload.ID, state.SourceUploadID)
			}
			if state.SourceUploadID == 0 {
				state.SourceUploadID = upload.ID
				if err := publishstate.Save(state); err != nil {
					return err
				}
			}
			if err := progress.done(fmt.Sprintf("source_upload_id=%d", upload.ID)); err != nil {
				return err
			}
			if err := progress.start(4, "完成提交并进入审核"); err != nil {
				return err
			}
			upload, err = client.FinalizeTemplateSource(command.Context(), session, upload.ID)
			if err != nil {
				return fmt.Errorf("主题包已上传，但完成发布会话失败；再次执行相同命令可继续: %w", err)
			}
			if err := publishstate.Delete("theme", session.Endpoint, profile, sourceDir); err != nil {
				return err
			}
			if err := progress.done("提交已冻结，等待平台审核"); err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "主题发布申请已提交，等待平台审核\n应用: %s\n版本: %s\n发布记录: %d\n状态: %s\n主题包 SHA-256: %s\n平台记录 SHA-256: %s\n", packed.Code, upload.Version, upload.ID, upload.Status, packed.SHA256, upload.SHA256)
			return err
		},
	}
	command.Flags().StringVar(&options.configPath, "config", "", "认证配置路径")
	command.Flags().StringVar(&options.profile, "profile", "", "认证 profile")
	command.Flags().DurationVar(&options.timeout, "timeout", options.timeout, "发布请求超时时间")
	command.Flags().StringVarP(&options.message, "message", "m", "", "版本说明")
	command.Flags().StringVarP(&options.messageFile, "message-file", "F", "", "从文件读取版本说明")
	command.Flags().StringVar(&options.sourceRef, "source-ref", "", "源码来源引用")
	command.Flags().StringVar(&options.commitSHA, "commit-sha", "", "源码提交 SHA")
	command.Flags().StringVar(&options.pipelineID, "pipeline-id", "", "CI Pipeline 标识")
	command.Flags().StringVar(&options.idempotencyKey, "idempotency-key", "", "发布幂等键，默认按本地发布会话生成")
	command.Flags().StringVar(&options.bump, "bump", "", "递增发布版本: patch、minor 或 major")
	command.Flags().StringVar(&options.version, "version", "", "设置自定义 SemVer 发布版本")
	command.Flags().BoolVar(&options.keepVersion, "keep-version", false, "保持主题双清单当前版本")
	return command
}

type themePublishProgress struct {
	writer io.Writer
}

func (progress themePublishProgress) start(step int, label string) error {
	_, err := fmt.Fprintf(progress.writer, "[%d/%d] %s\n", step, themePublishProgressSteps, label)
	return err
}

func (progress themePublishProgress) done(detail string) error {
	_, err := fmt.Fprintf(progress.writer, "      完成：%s\n", detail)
	return err
}

func prepareThemePublishVersion(command *cobra.Command, sourceDir string, options publishOptions, isTerminal func(io.Reader) bool) (string, error) {
	currentValue, err := sourcepack.ReadVersion(sourceDir)
	if err != nil {
		return "", fmt.Errorf("读取主题发布版本失败: %w", err)
	}
	current, err := pluginmanifest.ParseVersion(currentValue)
	if err != nil {
		return "", fmt.Errorf("主题清单版本无效: %w", err)
	}
	target, err := chooseThemePublishVersion(command, current, options, isTerminal)
	if err != nil {
		return "", err
	}
	if err := applyThemePublishVersion(command, sourceDir, current.String(), target.String()); err != nil {
		return "", err
	}
	return target.String(), nil
}

func resolveThemePublishState(command *cobra.Command, sourceDir, endpoint, profile string, options publishOptions, isTerminal func(io.Reader) bool) (publishstate.State, bool, error) {
	currentValue, err := sourcepack.ReadVersion(sourceDir)
	if err != nil {
		return publishstate.State{}, false, fmt.Errorf("读取主题发布版本失败: %w", err)
	}
	current, err := pluginmanifest.ParseVersion(currentValue)
	if err != nil {
		return publishstate.State{}, false, fmt.Errorf("主题清单版本无效: %w", err)
	}
	state, found, err := publishstate.Load("theme", endpoint, profile, sourceDir)
	if err != nil {
		return publishstate.State{}, false, err
	}
	if found {
		if current.String() != state.OriginalVersion && current.String() != state.TargetVersion {
			return publishstate.State{}, false, fmt.Errorf("主题当前版本 %s 与发布恢复状态 %s -> %s 不一致", current, state.OriginalVersion, state.TargetVersion)
		}
		if key := strings.TrimSpace(options.idempotencyKey); key != "" && key != state.IdempotencyKey {
			return publishstate.State{}, false, fmt.Errorf("--idempotency-key 与未完成发布会话不一致")
		}
		return state, true, nil
	}
	target, err := chooseThemePublishVersion(command, current, options, isTerminal)
	if err != nil {
		return publishstate.State{}, false, err
	}
	key := strings.TrimSpace(options.idempotencyKey)
	if key == "" {
		key, err = publishstate.NewAttemptKey()
		if err != nil {
			return publishstate.State{}, false, err
		}
	}
	state = publishstate.State{
		Kind: "theme", SourceDir: sourceDir, Endpoint: endpoint, Profile: profile,
		OriginalVersion: current.String(), TargetVersion: target.String(), IdempotencyKey: key,
	}
	if err := publishstate.Save(state); err != nil {
		return publishstate.State{}, false, err
	}
	return state, false, nil
}

func applyThemePublishVersion(command *cobra.Command, sourceDir, originalVersion, targetVersion string) error {
	currentValue, err := sourcepack.ReadVersion(sourceDir)
	if err != nil {
		return fmt.Errorf("读取主题发布版本失败: %w", err)
	}
	current, err := pluginmanifest.ParseVersion(currentValue)
	if err != nil {
		return fmt.Errorf("主题清单版本无效: %w", err)
	}
	target, err := pluginmanifest.ParseVersion(targetVersion)
	if err != nil {
		return fmt.Errorf("发布恢复状态目标版本无效: %w", err)
	}
	if current.String() != originalVersion && current.String() != target.String() {
		return fmt.Errorf("主题当前版本 %s 与发布目标 %s 不一致", current, target)
	}
	if current.String() != target.String() {
		if err := sourcepack.UpdateVersion(sourceDir, target.String()); err != nil {
			return err
		}
		_, err = fmt.Fprintf(command.ErrOrStderr(), "发布版本：%s -> %s\n已更新 template.yaml 和 theme.yaml\n", originalVersion, target)
	} else {
		_, err = fmt.Fprintf(command.ErrOrStderr(), "发布版本：保持 %s\n", target)
	}
	return err
}

func chooseThemePublishVersion(command *cobra.Command, current pluginmanifest.Version, options publishOptions, isTerminal func(io.Reader) bool) (pluginmanifest.Version, error) {
	if options.keepVersion {
		return current, nil
	}
	if value := strings.TrimSpace(options.version); value != "" {
		return pluginmanifest.ParseVersion(value)
	}
	if bump := strings.ToLower(strings.TrimSpace(options.bump)); bump != "" {
		return current.Bump(bump)
	}
	if !isTerminal(command.InOrStdin()) {
		return pluginmanifest.Version{}, fmt.Errorf("当前环境无法交互选择发布版本，请使用 --bump patch、--version <version> 或 --keep-version")
	}
	patch, err := current.Bump("patch")
	if err != nil {
		return pluginmanifest.Version{}, err
	}
	minor, err := current.Bump("minor")
	if err != nil {
		return pluginmanifest.Version{}, err
	}
	major, err := current.Bump("major")
	if err != nil {
		return pluginmanifest.Version{}, err
	}
	if _, err := fmt.Fprintf(command.ErrOrStderr(), `当前版本：%s

请选择本次发布版本：
  1. 保持 %s
  2. Patch %s（默认）
  3. Minor %s
  4. Major %s
  5. 自定义版本
选择 [2]: `, current, current, patch, minor, major); err != nil {
		return pluginmanifest.Version{}, err
	}
	scanner := bufio.NewScanner(command.InOrStdin())
	scanner.Buffer(make([]byte, 128), 512)
	for {
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return pluginmanifest.Version{}, fmt.Errorf("读取发布版本选择失败: %w", err)
			}
			return pluginmanifest.Version{}, fmt.Errorf("读取发布版本选择失败: 输入已结束")
		}
		switch strings.ToLower(strings.TrimSpace(scanner.Text())) {
		case "1", "keep", "保持":
			return current, nil
		case "", "2", "patch":
			return patch, nil
		case "3", "minor":
			return minor, nil
		case "4", "major":
			return major, nil
		case "5", "custom", "自定义":
			return readCustomThemeVersion(command, scanner)
		default:
			if _, err := fmt.Fprint(command.ErrOrStderr(), "请输入 1-5，或 keep、patch、minor、major: "); err != nil {
				return pluginmanifest.Version{}, err
			}
		}
	}
}

func readCustomThemeVersion(command *cobra.Command, scanner *bufio.Scanner) (pluginmanifest.Version, error) {
	for {
		if _, err := fmt.Fprint(command.ErrOrStderr(), "自定义版本: "); err != nil {
			return pluginmanifest.Version{}, err
		}
		if !scanner.Scan() {
			return pluginmanifest.Version{}, fmt.Errorf("读取自定义版本失败: 输入已结束")
		}
		version, err := pluginmanifest.ParseVersion(strings.TrimSpace(scanner.Text()))
		if err == nil {
			return version, nil
		}
		if _, writeErr := fmt.Fprintf(command.ErrOrStderr(), "%v\n", err); writeErr != nil {
			return pluginmanifest.Version{}, writeErr
		}
	}
}

func validateThemePublishOptions(options publishOptions, message string) error {
	versionOptions := 0
	if strings.TrimSpace(options.bump) != "" {
		versionOptions++
	}
	if strings.TrimSpace(options.version) != "" {
		versionOptions++
	}
	if options.keepVersion {
		versionOptions++
	}
	if versionOptions > 1 {
		return fmt.Errorf("--bump、--version 和 --keep-version 不能同时使用")
	}
	if bump := strings.ToLower(strings.TrimSpace(options.bump)); bump != "" && bump != "patch" && bump != "minor" && bump != "major" {
		return fmt.Errorf("--bump 必须是 patch、minor 或 major")
	}
	if value := strings.TrimSpace(options.version); value != "" {
		if _, err := pluginmanifest.ParseVersion(value); err != nil {
			return fmt.Errorf("--version 无效: %w", err)
		}
	}
	if options.timeout <= 0 || len(message) > 20_000 || len(strings.TrimSpace(options.sourceRef)) > 255 ||
		len(strings.TrimSpace(options.commitSHA)) > 128 || len(strings.TrimSpace(options.pipelineID)) > 128 || len(strings.TrimSpace(options.idempotencyKey)) > 128 {
		return fmt.Errorf("主题发布参数无效")
	}
	return nil
}

func themePublishInputIsTerminal(input io.Reader) bool {
	file, ok := input.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func themeVersionMessage(value, path string) (string, error) {
	if value != "" && path != "" {
		return "", fmt.Errorf("--message 和 --message-file 不能同时使用")
	}
	if path == "" {
		return strings.TrimSpace(value), nil
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, 20_001))
	if err != nil || len(body) > 20_000 {
		return "", fmt.Errorf("版本说明不能超过 20000 字节")
	}
	return strings.TrimSpace(string(body)), nil
}
