package app

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zane-xin/sparq-protocol/protocol"
	protocolrelease "github.com/zane-xin/sparq-protocol/release"
	artifactsignature "github.com/zane-xin/sparq-tool/internal/artifact/signature"
)

type PackageOptions struct {
	OutputPath     string
	Version        string
	Runtimes       []RuntimeInput
	Updaters       []UpdaterInput
	AdminPath      string
	ChangelogPath  string
	PublicBaseURL  string
	SigningKeyID   string
	SigningKeyPath string
}

type RuntimeInput struct {
	TargetOS   string
	TargetArch string
	BinaryPath string
}

type UpdaterInput struct {
	TargetOS   string
	TargetArch string
	BinaryPath string
}

type RuntimePackage struct {
	TargetOS   string
	TargetArch string
	Path       string
}

type UpdaterPackage = RuntimePackage

type PackageResult struct {
	Directory     string
	ManifestPath  string
	Runtimes      []RuntimePackage
	Updaters      []UpdaterPackage
	AdminPath     string
	ChangelogPath string
	Manifest      protocolrelease.UpdateManifest
}

type adminArtifactManifest struct {
	Format  string `json:"format"`
	Version string `json:"version"`
}

func Package(options PackageOptions) (PackageResult, error) {
	version := strings.TrimSpace(strings.TrimPrefix(options.Version, "v"))
	if version == "" || version == "dev" {
		return PackageResult{}, fmt.Errorf("官方发行打包必须指定正式版本")
	}
	publicBaseURL := strings.TrimRight(strings.TrimSpace(options.PublicBaseURL), "/")
	if publicBaseURL == "" {
		return PackageResult{}, fmt.Errorf("发行下载基础地址不能为空")
	}
	signingKeyID := strings.TrimSpace(options.SigningKeyID)
	if signingKeyID == "" || signingKeyID != options.SigningKeyID {
		return PackageResult{}, fmt.Errorf("发行签名 key id 不能为空且不能包含首尾空白")
	}
	if info, err := os.Stat(options.SigningKeyPath); err != nil || !info.Mode().IsRegular() {
		if err == nil {
			err = fmt.Errorf("不是普通文件")
		}
		return PackageResult{}, fmt.Errorf("检查发行签名私钥 %s 失败: %w", options.SigningKeyPath, err)
	}
	for name, path := range map[string]string{"Admin Shell": options.AdminPath, "更新日志": options.ChangelogPath} {
		if strings.TrimSpace(path) == "" {
			return PackageResult{}, fmt.Errorf("%s路径不能为空", name)
		}
		if _, err := os.Stat(path); err != nil {
			return PackageResult{}, fmt.Errorf("检查%s %s 失败: %w", name, path, err)
		}
	}
	if err := validateRuntimeInputs(options.Runtimes); err != nil {
		return PackageResult{}, err
	}
	if err := validateUpdaterInputs(options.Updaters); err != nil {
		return PackageResult{}, err
	}
	if err := validateAdminVersion(options.AdminPath, version); err != nil {
		return PackageResult{}, err
	}
	output, err := filepath.Abs(options.OutputPath)
	if err != nil {
		return PackageResult{}, err
	}
	if _, err := os.Stat(output); err == nil {
		return PackageResult{}, fmt.Errorf("发行包目录已存在: %s", output)
	} else if !errors.Is(err, os.ErrNotExist) {
		return PackageResult{}, err
	}
	parent := filepath.Dir(output)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return PackageResult{}, err
	}
	staging, err := os.MkdirTemp(parent, ".sparq-app-release-*")
	if err != nil {
		return PackageResult{}, err
	}
	defer os.RemoveAll(staging)
	adminPath := filepath.Join(staging, "sparq-admin.tar.gz")
	if err := archiveDirectory(options.AdminPath, adminPath); err != nil {
		return PackageResult{}, fmt.Errorf("打包 Admin Shell 失败: %w", err)
	}
	adminSHA, err := fileSHA256(adminPath)
	if err != nil {
		return PackageResult{}, err
	}
	adminSignature, err := signReleaseFile(adminPath, signingKeyID, options.SigningKeyPath)
	if err != nil {
		return PackageResult{}, fmt.Errorf("签名 Admin Shell 失败: %w", err)
	}
	manifest := protocolrelease.UpdateManifest{
		Format: protocolrelease.ManifestFormat, Channel: protocolrelease.ChannelStable, GeneratedAt: time.Now().UTC(),
		Admin: protocolrelease.Artifact{Code: protocolrelease.ArtifactAdmin, LatestVersion: version, DownloadURL: publicBaseURL + "/cloud-api/v1/releases/sparq/download/" + version + "/admin", SHA256: adminSHA, RequiresRestart: true, Signature: adminSignature},
	}
	runtimePackages := make([]RuntimePackage, 0, len(options.Runtimes))
	for _, runtime := range options.Runtimes {
		name := fmt.Sprintf("sparq-runtime-%s-%s.tar.gz", runtime.TargetOS, runtime.TargetArch)
		path := filepath.Join(staging, name)
		if err := archiveFile(runtime.BinaryPath, path); err != nil {
			return PackageResult{}, fmt.Errorf("打包 Runtime %s/%s 失败: %w", runtime.TargetOS, runtime.TargetArch, err)
		}
		digest, err := fileSHA256(path)
		if err != nil {
			return PackageResult{}, err
		}
		signed, err := signReleaseFile(path, signingKeyID, options.SigningKeyPath)
		if err != nil {
			return PackageResult{}, fmt.Errorf("签名 Runtime %s/%s 失败: %w", runtime.TargetOS, runtime.TargetArch, err)
		}
		manifest.Runtimes = append(manifest.Runtimes, protocolrelease.RuntimeArtifact{
			TargetOS: runtime.TargetOS, TargetArch: runtime.TargetArch, ProtocolVersion: protocol.Version,
			Artifact: protocolrelease.Artifact{Code: protocolrelease.ArtifactRuntime, LatestVersion: version, DownloadURL: publicBaseURL + "/cloud-api/v1/releases/sparq/download/" + version + "/runtime/" + runtime.TargetOS + "/" + runtime.TargetArch, SHA256: digest, RequiresRestart: true, Signature: signed},
		})
		runtimePackages = append(runtimePackages, RuntimePackage{TargetOS: runtime.TargetOS, TargetArch: runtime.TargetArch, Path: path})
	}
	updaterPackages := make([]UpdaterPackage, 0, len(options.Updaters))
	for _, updater := range options.Updaters {
		name := fmt.Sprintf("sparq-updater-%s-%s.tar.gz", updater.TargetOS, updater.TargetArch)
		path := filepath.Join(staging, name)
		if err := archiveFile(updater.BinaryPath, path); err != nil {
			return PackageResult{}, fmt.Errorf("打包 updater %s/%s 失败: %w", updater.TargetOS, updater.TargetArch, err)
		}
		digest, err := fileSHA256(path)
		if err != nil {
			return PackageResult{}, err
		}
		signed, err := signReleaseFile(path, signingKeyID, options.SigningKeyPath)
		if err != nil {
			return PackageResult{}, fmt.Errorf("签名 updater %s/%s 失败: %w", updater.TargetOS, updater.TargetArch, err)
		}
		manifest.Updaters = append(manifest.Updaters, protocolrelease.UpdaterArtifact{
			TargetOS: updater.TargetOS, TargetArch: updater.TargetArch,
			Artifact: protocolrelease.Artifact{Code: protocolrelease.ArtifactUpdater, LatestVersion: version, DownloadURL: publicBaseURL + "/cloud-api/v1/releases/sparq/download/" + version + "/updater/" + updater.TargetOS + "/" + updater.TargetArch, SHA256: digest, RequiresRestart: true, Signature: signed},
		})
		updaterPackages = append(updaterPackages, UpdaterPackage{TargetOS: updater.TargetOS, TargetArch: updater.TargetArch, Path: path})
	}
	payload, err := protocolrelease.ManifestSigningBytes(manifest)
	if err != nil {
		return PackageResult{}, fmt.Errorf("生成发行清单签名内容失败: %w", err)
	}
	manifestSignature, err := artifactsignature.SignBytes(payload, options.SigningKeyPath)
	if err != nil {
		return PackageResult{}, fmt.Errorf("签名发行清单失败: %w", err)
	}
	manifest.Signature = protocolrelease.Signature{Algorithm: protocolrelease.SignatureAlgorithmEd25519, KeyID: signingKeyID, Value: base64.StdEncoding.EncodeToString(manifestSignature)}
	if err := manifest.ValidateForExecution(); err != nil {
		return PackageResult{}, fmt.Errorf("生成发行清单失败: %w", err)
	}
	manifestPath := filepath.Join(staging, "manifest.json")
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return PackageResult{}, err
	}
	if err := os.WriteFile(manifestPath, append(encoded, '\n'), 0o644); err != nil {
		return PackageResult{}, err
	}
	changelogPath := filepath.Join(staging, "CHANGELOG.md")
	if err := copyFile(options.ChangelogPath, changelogPath); err != nil {
		return PackageResult{}, fmt.Errorf("复制更新日志失败: %w", err)
	}
	if err := os.Rename(staging, output); err != nil {
		return PackageResult{}, err
	}
	return PackageResult{
		Directory: output, ManifestPath: filepath.Join(output, filepath.Base(manifestPath)),
		Runtimes: finalizedRuntimePackages(output, runtimePackages), Updaters: finalizedRuntimePackages(output, updaterPackages), AdminPath: filepath.Join(output, filepath.Base(adminPath)),
		ChangelogPath: filepath.Join(output, filepath.Base(changelogPath)), Manifest: manifest,
	}, nil
}

func signReleaseFile(path, keyID, privateKeyPath string) (protocolrelease.Signature, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return protocolrelease.Signature{}, err
	}
	signed, err := artifactsignature.SignBytes(body, privateKeyPath)
	if err != nil {
		return protocolrelease.Signature{}, err
	}
	return protocolrelease.Signature{Algorithm: protocolrelease.SignatureAlgorithmEd25519, KeyID: keyID, Value: base64.StdEncoding.EncodeToString(signed)}, nil
}

func validateRuntimeInputs(runtimes []RuntimeInput) error {
	want := map[string]bool{"linux/amd64": false, "linux/arm64": false, "darwin/amd64": false, "darwin/arm64": false, "windows/amd64": false, "windows/arm64": false}
	if len(runtimes) != len(want) {
		return fmt.Errorf("正式发行必须包含完整的 6 个 Runtime 平台")
	}
	for _, runtime := range runtimes {
		key := runtime.TargetOS + "/" + runtime.TargetArch
		seen, supported := want[key]
		if !supported || seen {
			return fmt.Errorf("Runtime 平台无效或重复: %s", key)
		}
		if info, err := os.Stat(runtime.BinaryPath); err != nil || !info.Mode().IsRegular() {
			if err == nil {
				err = fmt.Errorf("不是普通文件")
			}
			return fmt.Errorf("检查 Runtime %s 失败: %w", key, err)
		}
		want[key] = true
	}
	return nil
}

func validateUpdaterInputs(updaters []UpdaterInput) error {
	if len(updaters) != 6 {
		return fmt.Errorf("正式发行必须包含完整的 6 个 updater 平台")
	}
	seen := map[string]bool{}
	for _, updater := range updaters {
		key := updater.TargetOS + "/" + updater.TargetArch
		if seen[key] || !supportedPlatform(key) {
			return fmt.Errorf("updater 平台无效或重复: %s", key)
		}
		info, err := os.Stat(updater.BinaryPath)
		if err != nil || !info.Mode().IsRegular() {
			if err == nil {
				err = fmt.Errorf("不是普通文件")
			}
			return fmt.Errorf("检查 updater %s 失败: %w", key, err)
		}
		seen[key] = true
	}
	return nil
}

func supportedPlatform(key string) bool {
	switch key {
	case "linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64", "windows/arm64":
		return true
	default:
		return false
	}
}

func finalizedRuntimePackages(output string, packages []RuntimePackage) []RuntimePackage {
	result := make([]RuntimePackage, 0, len(packages))
	for _, runtime := range packages {
		runtime.Path = filepath.Join(output, filepath.Base(runtime.Path))
		result = append(result, runtime)
	}
	return result
}

func validateAdminVersion(adminPath, version string) error {
	body, err := os.ReadFile(filepath.Join(adminPath, "sparq-admin-manifest.json"))
	if err != nil {
		return fmt.Errorf("读取 Admin artifact manifest 失败: %w", err)
	}
	var manifest adminArtifactManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return fmt.Errorf("解析 Admin artifact manifest 失败: %w", err)
	}
	if manifest.Format != "sparq.admin.artifact.v1" || strings.TrimPrefix(manifest.Version, "v") != version {
		return fmt.Errorf("Admin artifact 版本 %q 与发行版本 %q 不一致", manifest.Version, version)
	}
	return nil
}

func archiveFile(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("构建产物必须是普通文件: %s", source)
	}
	return writeArchive(destination, []archiveEntry{{source: source, name: filepath.Base(source), info: info}})
}

func archiveDirectory(root, destination string) error {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return fmt.Errorf("Admin Shell 构建产物必须是普通目录: %s", root)
	}
	var entries []archiveEntry
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!entry.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("Admin Shell 只能包含普通文件和目录: %s", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if top, _, _ := strings.Cut(filepath.ToSlash(relative), "/"); top == "plugin-assets" {
			return fmt.Errorf("Admin Shell 发行包不能包含运行期插件资源: %s", relative)
		}
		entries = append(entries, archiveEntry{source: path, name: filepath.ToSlash(relative), info: info})
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	return writeArchive(destination, entries)
}

type archiveEntry struct {
	source string
	name   string
	info   fs.FileInfo
}

func writeArchive(destination string, entries []archiveEntry) error {
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	var writeErr error
	for _, entry := range entries {
		header, err := tar.FileInfoHeader(entry.info, "")
		if err != nil {
			writeErr = err
			break
		}
		header.Name = entry.name
		header.ModTime = time.Unix(0, 0)
		header.AccessTime = time.Time{}
		header.ChangeTime = time.Time{}
		if entry.info.IsDir() && !strings.HasSuffix(header.Name, "/") {
			header.Name += "/"
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			writeErr = err
			break
		}
		if entry.info.IsDir() {
			continue
		}
		input, err := os.Open(entry.source)
		if err != nil {
			writeErr = err
			break
		}
		_, copyErr := io.Copy(tarWriter, input)
		closeErr := input.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			writeErr = err
			break
		}
	}
	return errors.Join(writeErr, tarWriter.Close(), gzipWriter.Close(), file.Close())
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	return errors.Join(copyErr, output.Close())
}
