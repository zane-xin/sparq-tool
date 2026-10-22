package plugincmd

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-tool/internal/plugin/buildpipeline"
	"github.com/zane-xin/sparq-tool/internal/plugin/manifest"
	"github.com/zane-xin/sparq-tool/internal/plugin/sourcepack"
	"github.com/zane-xin/sparq-tool/internal/publishcache"
	"github.com/zane-xin/sparq-tool/internal/publishstate"
	remoteauth "github.com/zane-xin/sparq-tool/internal/remote/auth"
	remotemarketplace "github.com/zane-xin/sparq-tool/internal/remote/marketplace"
	"golang.org/x/term"
)

const (
	defaultPublishTimeout = 5 * time.Minute
	maxMessageBytes       = 20_000
	publishProgressSteps  = 10
	candidateBuildRules   = "sparq.plugin.candidate-cache.v1"
)

type publishProgress struct {
	writer io.Writer
}

func (progress publishProgress) start(step int, label string) error {
	_, err := fmt.Fprintf(progress.writer, "[%d/%d] %s\n", step, publishProgressSteps, label)
	return err
}

func (progress publishProgress) done(detail string) error {
	_, err := fmt.Fprintf(progress.writer, "      完成：%s\n", detail)
	return err
}

func (progress publishProgress) item(current int, total int, label string) error {
	_, err := fmt.Fprintf(progress.writer, "      [%d/%d] %s\n", current, total, label)
	return err
}

type publishOptions struct {
	configPath string
	timeout    time.Duration
	message    string
	bump       string
	version    string
	rebuild    bool
	restart    bool
}

func newPublishCommand() *cobra.Command {
	return newPublishCommandWithTerminalCheck(publishInputIsTerminal)
}

func newPublishCommandWithTerminalCheck(isTerminal func(io.Reader) bool) *cobra.Command {
	options := publishOptions{timeout: defaultPublishTimeout}
	command := &cobra.Command{
		Use:   "publish [path]",
		Short: "构建插件并提交 Sparq Cloud 审核",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runPluginPublish(command, args, options, isTerminal)
		},
	}
	command.Flags().StringVar(&options.configPath, "config", "", "认证配置路径，默认使用 "+remoteauth.DefaultConfigPathDisplay)
	command.Flags().DurationVar(&options.timeout, "timeout", defaultPublishTimeout, "发布请求超时时间")
	command.Flags().StringVarP(&options.message, "message", "m", "", "版本说明")
	command.Flags().StringVar(&options.bump, "bump", "", "递增发布版本: patch、minor 或 major")
	command.Flags().StringVar(&options.version, "version", "", "设置自定义 SemVer 发布版本")
	command.Flags().BoolVar(&options.rebuild, "rebuild", false, "忽略候选包缓存并重新构建")
	command.Flags().BoolVar(&options.restart, "restart", false, "取消未完成的远端会话并重新发布")
	return command
}

func runPluginPublish(command *cobra.Command, args []string, options publishOptions, isTerminal func(io.Reader) bool) error {
	progress := publishProgress{writer: command.ErrOrStderr()}
	sourceDir := "."
	if len(args) > 0 {
		sourceDir = args[0]
	}
	message := strings.TrimSpace(options.message)
	if err := validatePublishOptions(options, message); err != nil {
		return err
	}
	if strings.TrimSpace(options.version) == "" && strings.TrimSpace(options.bump) == "" && !isTerminal(command.InOrStdin()) {
		return fmt.Errorf("当前环境无法交互选择发布版本，请使用 --bump patch 或 --version <version>")
	}
	session, profile, _, err := remoteauth.ResolveSession(options.configPath, "")
	if err != nil {
		return err
	}
	if session.ExpiresAt > 0 && time.Now().Unix() >= session.ExpiresAt {
		return fmt.Errorf("认证 profile %q 已过期，请重新运行 sparq-tool auth login", profile)
	}
	if err := publishcache.CleanExpired(time.Now()); err != nil {
		return err
	}
	if err := progress.start(1, "准备发布版本"); err != nil {
		return err
	}
	state, resumed, err := resolvePluginPublishState(command, sourceDir, session.Endpoint, profile, options, isTerminal)
	if err != nil {
		return err
	}
	item, err := manifest.Read(filepath.Join(sourceDir, manifest.DefaultFileName))
	if err != nil {
		return fmt.Errorf("读取插件清单失败: %w", err)
	}
	if state.Code != "" && state.Code != item.Metadata.Code {
		return fmt.Errorf("发布恢复状态属于插件 %q，当前插件为 %q", state.Code, item.Metadata.Code)
	}
	versionDetail := state.TargetVersion
	if resumed {
		versionDetail += "（恢复发布）"
	}
	if err := progress.done(versionDetail); err != nil {
		return err
	}

	client := remotemarketplace.NewClient(&http.Client{Timeout: options.timeout})
	if err := progress.start(2, "远程发布预检"); err != nil {
		return err
	}
	preflight, err := preflightApplicationPublish(command, client, session, profile, item.Metadata.Code, state, message)
	if err != nil {
		return err
	}
	if options.restart {
		state, preflight, err = restartApplicationPublish(command, client, session, profile, sourceDir, state, preflight, item.Metadata.Code, message)
		if err != nil {
			return err
		}
	}
	if preflight.ResumableUpload != nil {
		if state.SourceUploadID != 0 && state.SourceUploadID != preflight.ResumableUpload.ID {
			return fmt.Errorf("本地发布状态与平台可恢复会话不一致: local=%d remote=%d", state.SourceUploadID, preflight.ResumableUpload.ID)
		}
		expired := preflight.ResumableUpload.Status == "expired" ||
			(preflight.ResumableUpload.Status == "uploading" && preflight.ResumableUpload.ExpiresAt > 0 && time.Now().Unix() >= int64(preflight.ResumableUpload.ExpiresAt))
		if preflight.ResumableUpload.Status == "canceled" || expired {
			status := preflight.ResumableUpload.Status
			if expired {
				status = "expired"
			}
			return fmt.Errorf("远端发布会话 source_upload_id=%d 已%s，请使用 --restart 重新发布", preflight.ResumableUpload.ID, publishSessionStatus(status))
		}
		state.SourceUploadID = preflight.ResumableUpload.ID
		if err := publishstate.Save(state); err != nil {
			return err
		}
	}
	preflightDetail := fmt.Sprintf("%s %s", preflight.ApplicationCode, preflight.Version)
	if preflight.ResumableUpload != nil {
		preflightDetail += fmt.Sprintf("，恢复 source_upload_id=%d", preflight.ResumableUpload.ID)
	}
	if err := progress.done(preflightDetail); err != nil {
		return err
	}

	if err := progress.start(3, "校验源码与候选缓存"); err != nil {
		return err
	}
	if err := applyPublishVersion(command, sourceDir, state.OriginalVersion, state.TargetVersion); err != nil {
		return err
	}
	fingerprint, err := sourcepack.Fingerprint(sourceDir)
	if err != nil {
		return fmt.Errorf("计算插件源码指纹失败: %w", err)
	}
	if state.SourceFingerprint != "" && !strings.EqualFold(state.SourceFingerprint, fingerprint.SHA256) {
		if state.SourceUploadID != 0 {
			return fmt.Errorf("插件源码已变化，不能继续 source_upload_id=%d；请使用 --restart 取消旧会话并重新构建", state.SourceUploadID)
		}
		if err := publishcache.Delete(state.IdempotencyKey); err != nil {
			return err
		}
		state.SourceSHA256 = ""
	}
	state.Code = item.Metadata.Code
	state.SourceFingerprint = fingerprint.SHA256
	if err := publishstate.Save(state); err != nil {
		return err
	}
	if options.rebuild {
		if err := publishcache.Delete(state.IdempotencyKey); err != nil {
			return err
		}
	}
	packageResults, cacheHit, err := loadPublishCandidates(command, state, candidateBuildFingerprint())
	if err != nil {
		return err
	}
	cacheDetail := fmt.Sprintf("source_sha256=%s，%d 个源码文件", fingerprint.SHA256, fingerprint.FileCount)
	if cacheHit {
		cacheDetail += fmt.Sprintf("，命中 %d 个候选包", len(packageResults))
	}
	if err := progress.done(cacheDetail); err != nil {
		return err
	}

	var buildResult buildpipeline.Result
	var platformBinaries []buildpipeline.PlatformBinary
	if err := progress.start(4, "构建插件"); err != nil {
		return err
	}
	if cacheHit {
		if err := progress.done("复用候选包缓存，跳过插件构建"); err != nil {
			return err
		}
	} else {
		buildResult, err = buildpipeline.Build(command.Context(), buildpipeline.Options{Dir: sourceDir, Target: buildpipeline.TargetAll})
		if err != nil {
			return fmt.Errorf("插件发布前构建验证失败: %w", err)
		}
		if err := progress.done(publishBuildSummary(buildResult)); err != nil {
			return err
		}
	}
	if err := progress.start(5, "构建平台产物"); err != nil {
		return err
	}
	if cacheHit {
		if err := progress.done("复用候选包缓存，跳过平台构建"); err != nil {
			return err
		}
	} else {
		platformBinaries, err = buildpipeline.BuildPlatforms(command.Context(), buildpipeline.PlatformOptions{Dir: sourceDir})
		if err != nil {
			return fmt.Errorf("插件发布前构建 Remote Runner 平台矩阵失败: %w", err)
		}
		if err := progress.done(fmt.Sprintf("%d 个 Remote Runner", len(platformBinaries))); err != nil {
			return err
		}
	}
	if err := progress.start(6, "准备候选构建包"); err != nil {
		return err
	}
	if cacheHit {
		if err := progress.done(fmt.Sprintf("复用 %d 个候选构建包", len(packageResults))); err != nil {
			return err
		}
	} else {
		packageResults, err = createPublishPackages(sourceDir, buildResult, platformBinaries)
		if err != nil {
			return fmt.Errorf("插件发布前打包失败: %w", err)
		}
		afterBuild, err := sourcepack.Fingerprint(sourceDir)
		if err != nil {
			return fmt.Errorf("重新计算插件源码指纹失败: %w", err)
		}
		if !strings.EqualFold(fingerprint.SHA256, afterBuild.SHA256) {
			return fmt.Errorf("插件源码在构建过程中发生变化，候选包未缓存也未上传；请确认源码稳定后重新执行发布")
		}
		packageResults, err = savePublishCandidates(state, candidateBuildFingerprint(), packageResults)
		if err != nil {
			return err
		}
		if err := progress.done(fmt.Sprintf("已缓存 %d 个候选构建包，有效期 %s", len(packageResults), publishcache.Lifetime)); err != nil {
			return err
		}
	}

	if err := progress.start(7, "打包源码"); err != nil {
		return err
	}
	beforeUpload, err := sourcepack.Fingerprint(sourceDir)
	if err != nil {
		return fmt.Errorf("上传前计算插件源码指纹失败: %w", err)
	}
	if !strings.EqualFold(fingerprint.SHA256, beforeUpload.SHA256) {
		if deleteErr := publishcache.Delete(state.IdempotencyKey); deleteErr != nil {
			return fmt.Errorf("插件源码在候选包准备后发生变化，且清理候选包缓存失败: %w", deleteErr)
		}
		return fmt.Errorf("插件源码在候选包准备后发生变化，候选包已作废且未上传；请重新执行发布")
	}
	temporary, err := os.MkdirTemp("", "sparq-plugin-publish-*")
	if err != nil {
		return fmt.Errorf("创建源码包临时目录失败: %w", err)
	}
	defer os.RemoveAll(temporary)
	packed, err := sourcepack.Create(sourcepack.Options{SourceDir: sourceDir, OutputDir: temporary})
	if err != nil {
		return err
	}
	if state.SourceSHA256 != "" && !strings.EqualFold(state.SourceSHA256, packed.SHA256) && state.SourceUploadID != 0 {
		return fmt.Errorf("源码归档与远端 source_upload_id=%d 不一致；请使用 --restart 取消旧会话并重新发布", state.SourceUploadID)
	}
	state.SourceSHA256 = packed.SHA256
	if err := publishstate.Save(state); err != nil {
		return err
	}
	if err := progress.done(fmt.Sprintf("%s（%d bytes）", filepath.Base(packed.ArchivePath), packed.SizeBytes)); err != nil {
		return err
	}

	if err := progress.start(8, "创建或恢复源码会话"); err != nil {
		return err
	}
	var submission remotemarketplace.SourceSubmission
	if state.SourceUploadID != 0 {
		submission, err = client.GetApplicationSourceSubmission(command.Context(), session, state.SourceUploadID)
	} else {
		var upload remotemarketplace.SourceUpload
		upload, err = client.UploadApplicationSource(command.Context(), remotemarketplace.UploadSourceInput{
			Session: session, ApplicationCode: packed.Code, ArchivePath: packed.ArchivePath,
			Changelog: message, IdempotencyKey: state.IdempotencyKey,
		})
		submission.Upload = upload
	}
	if err != nil {
		return publishSourceError(packed.Code, packed.Version, session.Endpoint, profile, err)
	}
	upload := submission.Upload
	if err := validatePublishedSource(packed, upload); err != nil {
		return err
	}
	if state.SourceUploadID == 0 {
		state.SourceUploadID = upload.ID
		if err := publishstate.Save(state); err != nil {
			return err
		}
	}
	if err := publishcache.Touch(state.IdempotencyKey); err != nil {
		return err
	}
	if err := progress.done(fmt.Sprintf("source_upload_id=%d", upload.ID)); err != nil {
		return err
	}

	if err := progress.start(9, "补传候选构建包"); err != nil {
		return err
	}
	candidates := make([]remotemarketplace.CandidateArtifact, 0, len(packageResults))
	expectations := make([]remotemarketplace.CandidateExpectation, 0, len(packageResults))
	for index, packageResult := range packageResults {
		if err := progress.item(index+1, len(packageResults), publishPackageLabel(packageResult)); err != nil {
			return err
		}
		candidateSHA256, err := publishFileSHA256(packageResult.ArchivePath)
		if err != nil {
			return fmt.Errorf("计算候选构建包 %s 校验码失败: %w", publishPackageLabel(packageResult), err)
		}
		kind := packageResult.Kind
		if kind == "" {
			kind = remotemarketplace.CandidateKindPluginPackage
		}
		expectation := remotemarketplace.CandidateExpectation{
			SHA256: candidateSHA256, Kind: kind, Runtime: packageResult.Runtime,
			TargetOS: packageResult.TargetOS, TargetArch: packageResult.TargetArch,
		}
		expectations = append(expectations, expectation)
		candidate, found, matchErr := matchingPublishedCandidate(submission.Candidates, expectation)
		if matchErr != nil {
			return matchErr
		}
		if !found {
			candidate, err = client.UploadCandidateArtifact(command.Context(), remotemarketplace.UploadCandidateArtifactInput{
				Session: session, SourceUploadID: upload.ID, SourceSHA256: upload.SHA256,
				ArchivePath: packageResult.ArchivePath, Format: "sparq.plugin.package.v1",
				Kind: kind, Runtime: packageResult.Runtime,
				TargetOS: packageResult.TargetOS, TargetArch: packageResult.TargetArch,
				IdempotencyKey: "sha256:" + candidateSHA256,
			})
		}
		if err != nil {
			return fmt.Errorf("源码上传成功，但候选构建包 %s 上传失败\nsource_upload_id: %d\nsource_sha256: %s\n错误: %w", publishPackageLabel(packageResult), upload.ID, upload.SHA256, err)
		}
		if err := validatePublishedCandidate(packageResult, candidateSHA256, upload, candidate); err != nil {
			return err
		}
		if err := publishcache.Touch(state.IdempotencyKey); err != nil {
			return err
		}
		candidates = append(candidates, candidate)
	}
	if err := progress.done(fmt.Sprintf("%d/%d", len(candidates), len(packageResults))); err != nil {
		return err
	}

	if err := progress.start(10, "完成提交并进入审核"); err != nil {
		return err
	}
	finalized, err := client.FinalizeApplicationSource(command.Context(), session, upload.ID, expectations)
	if err != nil {
		return fmt.Errorf("候选构建包已上传，但完成发布会话失败；再次执行相同命令可继续: %w", err)
	}
	if err := publishcache.Delete(state.IdempotencyKey); err != nil {
		return err
	}
	if err := publishstate.Delete("plugin", session.Endpoint, profile, sourceDir); err != nil {
		return err
	}
	upload = finalized.Upload
	candidates = finalized.Candidates
	if err := progress.done("提交已冻结，等待平台审核"); err != nil {
		return err
	}
	if _, err = fmt.Fprintf(command.OutOrStdout(), "插件发布申请已提交，等待平台审核\n应用: %s\n版本: %s\n源码上传记录: %d\n源码状态: %s\n源码 SHA-256: %s\n候选 Artifact 数量: %d\n", packed.Code, upload.Version, upload.ID, upload.Status, upload.SHA256, len(candidates)); err != nil {
		return err
	}
	for _, candidate := range candidates {
		label := candidate.Kind
		if candidate.TargetOS != "" {
			label += " " + candidate.TargetOS + "/" + candidate.TargetArch
		}
		if _, err = fmt.Fprintf(command.OutOrStdout(), "- %s: ID %d, SHA-256 %s, 校验 %s, 信任 %s\n", label, candidate.ID, candidate.SHA256, candidate.VerificationStatus, candidate.TrustDecision); err != nil {
			return err
		}
	}
	return nil
}

func preflightApplicationPublish(command *cobra.Command, client remotemarketplace.Client, session remoteauth.Session, profile, code string, state publishstate.State, message string) (remotemarketplace.PublishPreflight, error) {
	result, err := client.PreflightApplicationPublish(command.Context(), remotemarketplace.PublishPreflightInput{
		Session: session, ApplicationCode: code, Version: state.TargetVersion, Changelog: message,
		IdempotencyKey: state.IdempotencyKey,
	})
	if err != nil {
		if remotemarketplace.IsUnauthorized(err) {
			return remotemarketplace.PublishPreflight{}, fmt.Errorf("Sparq Cloud 登录态无效，请重新运行 sparq-tool auth login: %w", err)
		}
		if remotemarketplace.IsNotFound(err) {
			return remotemarketplace.PublishPreflight{}, publishAppNotFoundError(code, session.Endpoint, profile, err)
		}
		return remotemarketplace.PublishPreflight{}, fmt.Errorf("插件 %q 版本 %s 发布预检失败: %w", code, state.TargetVersion, err)
	}
	return result, nil
}

func restartApplicationPublish(command *cobra.Command, client remotemarketplace.Client, session remoteauth.Session, profile, sourceDir string, state publishstate.State, preflight remotemarketplace.PublishPreflight, code, message string) (publishstate.State, remotemarketplace.PublishPreflight, error) {
	uploadID := state.SourceUploadID
	if preflight.ResumableUpload != nil {
		uploadID = preflight.ResumableUpload.ID
	}
	if uploadID != 0 {
		if _, err := client.CancelApplicationSource(command.Context(), session, uploadID); err != nil {
			return publishstate.State{}, remotemarketplace.PublishPreflight{}, fmt.Errorf("重新发布前取消 source_upload_id=%d 失败: %w", uploadID, err)
		}
	}
	if err := publishcache.Delete(state.IdempotencyKey); err != nil {
		return publishstate.State{}, remotemarketplace.PublishPreflight{}, err
	}
	key, err := publishstate.NewAttemptKey()
	if err != nil {
		return publishstate.State{}, remotemarketplace.PublishPreflight{}, err
	}
	state.IdempotencyKey = key
	state.Code = code
	state.SourceFingerprint = ""
	state.SourceSHA256 = ""
	state.SourceUploadID = 0
	if err := publishstate.Save(state); err != nil {
		return publishstate.State{}, remotemarketplace.PublishPreflight{}, err
	}
	result, err := preflightApplicationPublish(command, client, session, profile, code, state, message)
	if err != nil {
		return publishstate.State{}, remotemarketplace.PublishPreflight{}, err
	}
	if result.ResumableUpload != nil {
		return publishstate.State{}, remotemarketplace.PublishPreflight{}, fmt.Errorf("新生成的发布会话仍绑定 source_upload_id=%d，请重新执行 --restart", result.ResumableUpload.ID)
	}
	return state, result, nil
}

func loadPublishCandidates(command *cobra.Command, state publishstate.State, buildFingerprint string) ([]pluginPackageResult, bool, error) {
	snapshot, found, err := publishcache.Load(state.IdempotencyKey, time.Now())
	if err != nil {
		if deleteErr := publishcache.Delete(state.IdempotencyKey); deleteErr != nil {
			return nil, false, fmt.Errorf("候选包缓存不可用: %v；清理缓存失败: %w", err, deleteErr)
		}
		if _, writeErr := fmt.Fprintf(command.ErrOrStderr(), "候选包缓存不可用，已清理并重新构建：%v\n", err); writeErr != nil {
			return nil, false, writeErr
		}
		return nil, false, nil
	}
	if !found {
		return nil, false, nil
	}
	if snapshot.Code != state.Code || snapshot.TargetVersion != state.TargetVersion ||
		!strings.EqualFold(snapshot.SourceFingerprint, state.SourceFingerprint) || snapshot.BuildFingerprint != buildFingerprint {
		if err := publishcache.Delete(state.IdempotencyKey); err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}
	return packageResultsFromCache(snapshot.Candidates), true, nil
}

func savePublishCandidates(state publishstate.State, buildFingerprint string, packages []pluginPackageResult) ([]pluginPackageResult, error) {
	candidates := make([]publishcache.Candidate, 0, len(packages))
	for _, result := range packages {
		kind := result.Kind
		if kind == "" {
			kind = remotemarketplace.CandidateKindPluginPackage
		}
		candidates = append(candidates, publishcache.Candidate{
			Kind: kind, Runtime: result.Runtime, TargetOS: result.TargetOS, TargetArch: result.TargetArch,
			ArchivePath: result.ArchivePath,
		})
	}
	snapshot, err := publishcache.Save(state.IdempotencyKey, publishcache.Snapshot{
		Code: state.Code, TargetVersion: state.TargetVersion, SourceFingerprint: state.SourceFingerprint,
		BuildFingerprint: buildFingerprint, Candidates: candidates,
	})
	if err != nil {
		return nil, fmt.Errorf("保存候选包缓存失败: %w", err)
	}
	return packageResultsFromCache(snapshot.Candidates), nil
}

func packageResultsFromCache(candidates []publishcache.Candidate) []pluginPackageResult {
	results := make([]pluginPackageResult, 0, len(candidates))
	for _, candidate := range candidates {
		kind := candidate.Kind
		if kind == remotemarketplace.CandidateKindPluginPackage {
			kind = ""
		}
		results = append(results, pluginPackageResult{
			ArchivePath: candidate.ArchivePath, Kind: kind, Runtime: candidate.Runtime,
			TargetOS: candidate.TargetOS, TargetArch: candidate.TargetArch,
		})
	}
	return results
}

func candidateBuildFingerprint() string {
	platforms := buildpipeline.DefaultPublishPlatforms()
	parts := []string{candidateBuildRules, runtime.Version(), runtime.GOOS, runtime.GOARCH}
	for _, platform := range platforms {
		parts = append(parts, platform.OS+"/"+platform.Arch)
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}

func publishSessionStatus(status string) string {
	if status == "expired" {
		return "过期"
	}
	return "取消"
}

func publishSourceError(code, version, endpoint, profile string, err error) error {
	if remotemarketplace.IsUnauthorized(err) {
		return fmt.Errorf("Sparq Cloud 登录态无效，请重新运行 sparq-tool auth login: %w", err)
	}
	if remotemarketplace.IsNotFound(err) {
		return publishAppNotFoundError(code, endpoint, profile, err)
	}
	return fmt.Errorf("提交插件 %q 版本 %s 的审核源码失败: %w", code, version, err)
}

func publishBuildSummary(result buildpipeline.Result) string {
	completed := make([]string, 0, 2)
	if result.GoTested {
		completed = append(completed, "Go 测试")
	}
	if result.FrontendBuilt {
		completed = append(completed, "前端产物")
	}
	if len(completed) == 0 {
		return "构建配置检查"
	}
	return strings.Join(completed, "、")
}

func createPublishPackages(sourceDir string, buildResult buildpipeline.Result, binaries []buildpipeline.PlatformBinary) ([]pluginPackageResult, error) {
	results := make([]pluginPackageResult, 0, len(binaries)+1)
	if len(binaries) == 0 && buildResult.FrontendBuilt {
		result, err := packagePlugin(pluginPackageOptions{
			Dir: sourceDir, Output: "release", DeliveryKind: remotemarketplace.ArtifactKindFrontendBundle,
			FrontendPath: buildResult.FrontendPath, FrontendOnly: true,
		})
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	for _, binary := range binaries {
		if !buildResult.FrontendBuilt {
			return nil, fmt.Errorf("Managed Remote 发布必须同时构建前端产物")
		}
		result, err := packagePlugin(pluginPackageOptions{
			Dir: sourceDir, Output: "release", DeliveryKind: remotemarketplace.ArtifactKindManagedRemote,
			Runtime: "http", TargetOS: binary.Platform.OS, TargetArch: binary.Platform.Arch,
			BackendPath: binary.Path, FrontendPath: buildResult.FrontendPath,
		})
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	if len(results) == 0 {
		result, err := packagePlugin(pluginPackageOptions{Dir: sourceDir, Output: "release"})
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func publishPackageLabel(result pluginPackageResult) string {
	if result.TargetOS != "" {
		return result.Kind + " " + result.TargetOS + "/" + result.TargetArch
	}
	if result.Kind == "" {
		return remotemarketplace.CandidateKindPluginPackage
	}
	return result.Kind
}

func validatePublishedSource(packed sourcepack.Result, upload remotemarketplace.SourceUpload) error {
	if upload.Version != packed.Version || !strings.EqualFold(upload.SHA256, packed.SHA256) ||
		upload.SizeBytes != uint64(packed.SizeBytes) || upload.Status == "" {
		return fmt.Errorf("平台登记结果与插件源码包不一致: source_upload_id=%d，本地 %s/%s/%d bytes，平台 %s/%s/%d bytes", upload.ID, packed.Version, packed.SHA256, packed.SizeBytes, upload.Version, upload.SHA256, upload.SizeBytes)
	}
	return nil
}

func validatePublishedCandidate(result pluginPackageResult, localSHA256 string, upload remotemarketplace.SourceUpload, candidate remotemarketplace.CandidateArtifact) error {
	kind := result.Kind
	if kind == "" {
		kind = remotemarketplace.CandidateKindPluginPackage
	}
	if candidate.SourceUploadID != upload.ID || candidate.VersionID != upload.VersionID ||
		!strings.EqualFold(candidate.SourceSHA256, upload.SHA256) || !strings.EqualFold(candidate.SHA256, localSHA256) ||
		candidate.Format != "sparq.plugin.package.v1" || candidate.Kind != kind || candidate.Runtime != result.Runtime ||
		candidate.TargetOS != result.TargetOS || candidate.TargetArch != result.TargetArch ||
		candidate.VerificationStatus == "" || candidate.TrustDecision == "" {
		return fmt.Errorf("平台登记的候选构建包与本地产物不一致: source_upload_id=%d，%s，本地 SHA-256 %s，平台 SHA-256 %s", upload.ID, publishPackageLabel(result), localSHA256, candidate.SHA256)
	}
	return nil
}

func publishInputIsTerminal(input io.Reader) bool {
	file, ok := input.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func preparePublishVersion(command *cobra.Command, sourceDir string, options publishOptions, isTerminal func(io.Reader) bool) (string, error) {
	manifestPath := filepath.Join(sourceDir, manifest.DefaultFileName)
	item, err := manifest.Read(manifestPath)
	if err != nil {
		return "", fmt.Errorf("读取发布版本失败: %w", err)
	}
	current, err := manifest.ParseVersion(item.Metadata.Version)
	if err != nil {
		return "", fmt.Errorf("plugin.yaml metadata.version 无效: %w", err)
	}
	target, err := choosePublishVersion(command, current, options, isTerminal)
	if err != nil {
		return "", err
	}
	if err := applyPublishVersion(command, sourceDir, current.String(), target.String()); err != nil {
		return "", err
	}
	return target.String(), nil
}

func resolvePluginPublishState(command *cobra.Command, sourceDir, endpoint, profile string, options publishOptions, isTerminal func(io.Reader) bool) (publishstate.State, bool, error) {
	item, err := manifest.Read(filepath.Join(sourceDir, manifest.DefaultFileName))
	if err != nil {
		return publishstate.State{}, false, fmt.Errorf("读取发布版本失败: %w", err)
	}
	current, err := manifest.ParseVersion(item.Metadata.Version)
	if err != nil {
		return publishstate.State{}, false, fmt.Errorf("plugin.yaml metadata.version 无效: %w", err)
	}
	state, found, err := publishstate.Load("plugin", endpoint, profile, sourceDir)
	if err != nil {
		return publishstate.State{}, false, err
	}
	if found {
		if current.String() != state.OriginalVersion && current.String() != state.TargetVersion {
			return publishstate.State{}, false, fmt.Errorf("plugin.yaml 当前版本 %s 与发布恢复状态 %s -> %s 不一致", current, state.OriginalVersion, state.TargetVersion)
		}
		return state, true, nil
	}
	target, err := choosePublishVersion(command, current, options, isTerminal)
	if err != nil {
		return publishstate.State{}, false, err
	}
	key, err := publishstate.NewAttemptKey()
	if err != nil {
		return publishstate.State{}, false, err
	}
	state = publishstate.State{
		Kind: "plugin", SourceDir: sourceDir, Endpoint: endpoint, Profile: profile,
		OriginalVersion: current.String(), TargetVersion: target.String(), IdempotencyKey: key,
	}
	if err := publishstate.Save(state); err != nil {
		return publishstate.State{}, false, err
	}
	return state, false, nil
}

func applyPublishVersion(command *cobra.Command, sourceDir, originalVersion, targetVersion string) error {
	manifestPath := filepath.Join(sourceDir, manifest.DefaultFileName)
	item, err := manifest.Read(manifestPath)
	if err != nil {
		return fmt.Errorf("读取发布版本失败: %w", err)
	}
	current, err := manifest.ParseVersion(item.Metadata.Version)
	if err != nil {
		return fmt.Errorf("plugin.yaml metadata.version 无效: %w", err)
	}
	target, err := manifest.ParseVersion(targetVersion)
	if err != nil {
		return fmt.Errorf("发布恢复状态目标版本无效: %w", err)
	}
	if current.String() != originalVersion && current.String() != target.String() {
		return fmt.Errorf("plugin.yaml 当前版本 %s 与发布目标 %s 不一致", current, target)
	}
	changed := current.String() != target.String()
	if changed {
		if _, err := manifest.UpdateVersion(manifestPath, target); err != nil {
			return err
		}
	}
	if err := invalidatePublishManifest(sourceDir); err != nil {
		return err
	}
	generated, err := manifest.GenerateGo(manifest.GoGenerateOptions{ManifestPath: manifestPath})
	if err != nil {
		return fmt.Errorf("重新生成插件 Manifest 失败: %w", err)
	}
	if err := manifest.WriteGo(generated); err != nil {
		return fmt.Errorf("写入插件 Manifest 生成文件失败: %w", err)
	}
	if changed {
		_, err = fmt.Fprintf(command.ErrOrStderr(), "发布版本：%s -> %s\n已更新 plugin.yaml 和 manifest_gen.go\n", originalVersion, target)
	} else {
		_, err = fmt.Fprintf(command.ErrOrStderr(), "发布版本：保持 %s\n已重新生成 manifest_gen.go\n", target)
	}
	return err
}

func matchingPublishedCandidate(candidates []remotemarketplace.CandidateArtifact, expected remotemarketplace.CandidateExpectation) (remotemarketplace.CandidateArtifact, bool, error) {
	var match remotemarketplace.CandidateArtifact
	found := false
	for _, candidate := range candidates {
		if candidate.Kind != expected.Kind || candidate.Runtime != expected.Runtime ||
			candidate.TargetOS != expected.TargetOS || candidate.TargetArch != expected.TargetArch {
			continue
		}
		if found {
			return remotemarketplace.CandidateArtifact{}, false, fmt.Errorf("平台发布会话存在重复候选变体 %s", expected.Kind)
		}
		if !strings.EqualFold(candidate.SHA256, expected.SHA256) {
			return remotemarketplace.CandidateArtifact{}, false, fmt.Errorf("平台候选变体 %s 的 SHA-256 与本地产物不一致", expected.Kind)
		}
		match = candidate
		found = true
	}
	return match, found, nil
}

func choosePublishVersion(command *cobra.Command, current manifest.Version, options publishOptions, isTerminal func(io.Reader) bool) (manifest.Version, error) {
	if value := strings.TrimSpace(options.version); value != "" {
		return manifest.ParseVersion(value)
	}
	if bump := strings.ToLower(strings.TrimSpace(options.bump)); bump != "" {
		return current.Bump(bump)
	}
	if !isTerminal(command.InOrStdin()) {
		return manifest.Version{}, fmt.Errorf("当前环境无法交互选择发布版本，请使用 --bump patch 或 --version <version>")
	}
	patch, err := current.Bump("patch")
	if err != nil {
		return manifest.Version{}, err
	}
	minor, err := current.Bump("minor")
	if err != nil {
		return manifest.Version{}, err
	}
	major, err := current.Bump("major")
	if err != nil {
		return manifest.Version{}, err
	}
	if _, err := fmt.Fprintf(command.ErrOrStderr(), `当前版本：%s

请选择本次发布版本：
  1. 保持 %s
  2. Patch %s（默认）
  3. Minor %s
  4. Major %s
  5. 自定义版本
选择 [2]: `, current, current, patch, minor, major); err != nil {
		return manifest.Version{}, err
	}
	scanner := bufio.NewScanner(command.InOrStdin())
	scanner.Buffer(make([]byte, 128), 512)
	for {
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return manifest.Version{}, fmt.Errorf("读取发布版本选择失败: %w", err)
			}
			return manifest.Version{}, fmt.Errorf("读取发布版本选择失败: 输入已结束")
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
			return readCustomPublishVersion(command, scanner)
		default:
			if _, err := fmt.Fprint(command.ErrOrStderr(), "请输入 1-5，或 keep、patch、minor、major: "); err != nil {
				return manifest.Version{}, err
			}
		}
	}
}

func readCustomPublishVersion(command *cobra.Command, scanner *bufio.Scanner) (manifest.Version, error) {
	for {
		if _, err := fmt.Fprint(command.ErrOrStderr(), "自定义版本: "); err != nil {
			return manifest.Version{}, err
		}
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return manifest.Version{}, fmt.Errorf("读取自定义版本失败: %w", err)
			}
			return manifest.Version{}, fmt.Errorf("读取自定义版本失败: 输入已结束")
		}
		version, err := manifest.ParseVersion(strings.TrimSpace(scanner.Text()))
		if err == nil {
			return version, nil
		}
		if _, writeErr := fmt.Fprintf(command.ErrOrStderr(), "%v\n", err); writeErr != nil {
			return manifest.Version{}, writeErr
		}
	}
}

func invalidatePublishManifest(sourceDir string) error {
	path := filepath.Join(sourceDir, "release", "manifest.json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("清理旧的完整 Manifest 失败: %w", err)
	}
	return nil
}

func publishFileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func publishAppNotFoundError(appCode string, endpoint string, profile string, err error) error {
	return fmt.Errorf(`发布插件 %q 失败: 平台未找到 code 相同的应用
请检查:
  1. plugin.yaml 的 metadata.code 是否为 %q
  2. 平台管理员是否已在 %s 的 Marketplace 登记该应用
  3. 当前认证 profile %q 是否指向预期平台
服务端返回: %w`, appCode, appCode, endpoint, profile, err)
}

func validatePublishOptions(options publishOptions, message string) error {
	if options.rebuild && options.restart {
		return fmt.Errorf("--rebuild 和 --restart 不能同时使用；--restart 已包含重新构建")
	}
	versionOptions := 0
	if strings.TrimSpace(options.bump) != "" {
		versionOptions++
	}
	if strings.TrimSpace(options.version) != "" {
		versionOptions++
	}
	if versionOptions > 1 {
		return fmt.Errorf("--bump 和 --version 不能同时使用")
	}
	if bump := strings.ToLower(strings.TrimSpace(options.bump)); bump != "" && bump != "patch" && bump != "minor" && bump != "major" {
		return fmt.Errorf("--bump 必须是 patch、minor 或 major")
	}
	if value := strings.TrimSpace(options.version); value != "" {
		if _, err := manifest.ParseVersion(value); err != nil {
			return fmt.Errorf("--version 无效: %w", err)
		}
	}
	if options.timeout <= 0 {
		return fmt.Errorf("发布请求超时时间必须大于零")
	}
	if len(message) > maxMessageBytes {
		return fmt.Errorf("版本说明不能超过 %d 字节", maxMessageBytes)
	}
	return nil
}
