package bundle

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	releasebuild "github.com/zane-xin/sparq-tool/internal/release/build"
)

const (
	Format        = "sparq.app.bundle.v1"
	ArchiveAuto   = "auto"
	ArchiveNone   = "none"
	ArchiveTarGz  = "tar.gz"
	ArchiveZip    = "zip"
	defaultInput  = "release/bin"
	defaultOutput = "release/packages"
)

var versionPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._-]*$`)

type Options struct {
	Root    string
	Input   string
	Output  string
	Version string
	Archive string
	Targets []releasebuild.Platform
}

type Artifact struct {
	Platform releasebuild.Platform
	Path     string
	Archive  string
}

type Result struct {
	Directory string
	Artifacts []Artifact
}

type Manifest struct {
	Format     string         `json:"format"`
	Version    string         `json:"version"`
	TargetOS   string         `json:"target_os"`
	TargetArch string         `json:"target_arch"`
	Files      []ManifestFile `json:"files"`
}

type ManifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type adminManifest struct {
	Format string   `json:"format"`
	Files  []string `json:"files"`
}

// Package 将 build app 的产物组装成可直接解压安装的完整 app bundle。
func Package(options Options) (Result, error) {
	root, err := filepath.Abs(defaultString(strings.TrimSpace(options.Root), "."))
	if err != nil {
		return Result{}, fmt.Errorf("解析 workspace root 失败: %w", err)
	}
	appDir := filepath.Join(root, "sparq-app")
	if info, err := os.Stat(filepath.Join(appDir, "go.mod")); err != nil || !info.Mode().IsRegular() {
		return Result{}, fmt.Errorf("app 目录缺少 go.mod: %s", appDir)
	}
	version := strings.TrimPrefix(strings.TrimSpace(defaultString(options.Version, "dev")), "v")
	if !versionPattern.MatchString(version) {
		return Result{}, fmt.Errorf("app bundle 版本无效: %q", options.Version)
	}
	archive := defaultString(strings.TrimSpace(options.Archive), ArchiveAuto)
	if archive != ArchiveAuto && archive != ArchiveNone && archive != ArchiveTarGz && archive != ArchiveZip {
		return Result{}, fmt.Errorf("不支持的归档格式 %q，可选 auto、none、tar.gz、zip", archive)
	}
	targets := options.Targets
	if len(targets) == 0 {
		targets = []releasebuild.Platform{{OS: envOrDefault("GOOS", runtime.GOOS), Arch: envOrDefault("GOARCH", runtime.GOARCH)}}
	}
	input := resolveAppPath(appDir, defaultString(strings.TrimSpace(options.Input), defaultInput))
	output := resolveAppPath(appDir, defaultString(strings.TrimSpace(options.Output), filepath.Join(defaultOutput, version)))
	if _, err := os.Stat(output); err == nil {
		return Result{}, fmt.Errorf("app bundle 输出目录已存在: %s", output)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, fmt.Errorf("检查 app bundle 输出目录失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return Result{}, fmt.Errorf("创建 app bundle 输出父目录失败: %w", err)
	}
	staging, err := os.MkdirTemp(filepath.Dir(output), ".sparq-app-bundle-*")
	if err != nil {
		return Result{}, fmt.Errorf("创建 app bundle 临时目录失败: %w", err)
	}
	defer os.RemoveAll(staging)
	artifactsDir := filepath.Join(staging, "artifacts")
	workDir := filepath.Join(staging, "work")
	if err := os.MkdirAll(artifactsDir, 0o755); err != nil {
		return Result{}, err
	}

	artifacts := make([]Artifact, 0, len(targets))
	for _, target := range targets {
		format := archive
		if format == ArchiveAuto {
			format = ArchiveTarGz
			if target.OS == "windows" {
				format = ArchiveZip
			}
		}
		name := fmt.Sprintf("sparq-%s-%s-%s", version, target.OS, target.Arch)
		bundleDir := filepath.Join(workDir, name, "sparq")
		if format == ArchiveNone {
			bundleDir = filepath.Join(artifactsDir, name, "sparq")
		}
		if err := stageBundle(appDir, input, bundleDir, target, len(targets) > 1, version); err != nil {
			return Result{}, err
		}
		artifactPath := filepath.Dir(bundleDir)
		if format == ArchiveTarGz {
			artifactPath = filepath.Join(artifactsDir, name+".tar.gz")
			if err := writeTarGz(bundleDir, artifactPath); err != nil {
				return Result{}, fmt.Errorf("生成 %s/%s tar.gz 失败: %w", target.OS, target.Arch, err)
			}
		} else if format == ArchiveZip {
			artifactPath = filepath.Join(artifactsDir, name+".zip")
			if err := writeZip(bundleDir, artifactPath); err != nil {
				return Result{}, fmt.Errorf("生成 %s/%s zip 失败: %w", target.OS, target.Arch, err)
			}
		}
		artifacts = append(artifacts, Artifact{Platform: target, Path: filepath.Join(output, filepath.Base(artifactPath)), Archive: format})
	}
	if err := os.Rename(artifactsDir, output); err != nil {
		return Result{}, fmt.Errorf("发布 app bundle 输出目录失败: %w", err)
	}
	return Result{Directory: output, Artifacts: artifacts}, nil
}

func stageBundle(appDir, input, destination string, target releasebuild.Platform, multiTarget bool, version string) error {
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	runtimeName := platformBinaryName("sparq", target, false)
	runtimeSource := resolveInputBinary(input, "sparq", target, multiTarget)
	if err := copyRegularFile(runtimeSource, filepath.Join(destination, runtimeName), 0o755); err != nil {
		return fmt.Errorf("复制 %s/%s 的 %s 失败: %w", target.OS, target.Arch, runtimeName, err)
	}
	updaterName := platformBinaryName("sparq-updater", target, false)
	updaterSource := resolveInputBinary(input, "sparq-updater", target, multiTarget)
	if regularFile(updaterSource) {
		if err := copyRegularFile(updaterSource, filepath.Join(destination, updaterName), 0o755); err != nil {
			return fmt.Errorf("复制 %s/%s 的 %s 失败: %w", target.OS, target.Arch, updaterName, err)
		}
	}
	configDir := filepath.Join(destination, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"app.yaml", "app.example.yaml"} {
		if err := copyRegularFile(filepath.Join(appDir, "config", name), filepath.Join(configDir, name), 0o644); err != nil {
			return fmt.Errorf("复制基础配置 %s 失败: %w", name, err)
		}
	}
	publicDestination := filepath.Join(destination, "public")
	if err := copyStaticPublic(filepath.Join(appDir, "public"), publicDestination); err != nil {
		return fmt.Errorf("复制 public 目录失败: %w", err)
	}
	adminSource := filepath.Join(input, "public", "admin")
	if regularFile(filepath.Join(adminSource, "index.html")) {
		if err := copyAdmin(adminSource, filepath.Join(publicDestination, "admin")); err != nil {
			return fmt.Errorf("复制 Admin 构建产物失败: %w", err)
		}
	}
	captchaSource := filepath.Join(input, "public", "assets", "captcha")
	if regularDirectory(captchaSource) {
		if err := copyDirectory(captchaSource, filepath.Join(publicDestination, "assets", "captcha")); err != nil {
			return fmt.Errorf("复制 Captcha 构建产物失败: %w", err)
		}
	}
	if err := copyRegularFile(filepath.Join(appDir, "README.release.md"), filepath.Join(destination, "README.md"), 0o644); err != nil {
		return fmt.Errorf("复制安装说明失败: %w", err)
	}
	return writeBundleMetadata(destination, target, version)
}

func writeBundleMetadata(root string, target releasebuild.Platform, version string) error {
	files, err := collectManifestFiles(root, nil)
	if err != nil {
		return err
	}
	manifest := Manifest{Format: Format, Version: version, TargetOS: target.OS, TargetArch: target.Arch, Files: files}
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("编码 app bundle manifest 失败: %w", err)
	}
	manifestPath := filepath.Join(root, "manifest.json")
	if err := os.WriteFile(manifestPath, append(body, '\n'), 0o644); err != nil {
		return fmt.Errorf("写入 app bundle manifest 失败: %w", err)
	}
	checksummed, err := collectManifestFiles(root, map[string]bool{"checksums.txt": true})
	if err != nil {
		return err
	}
	var checksums strings.Builder
	for _, file := range checksummed {
		fmt.Fprintf(&checksums, "%s  %s\n", file.SHA256, file.Path)
	}
	if err := os.WriteFile(filepath.Join(root, "checksums.txt"), []byte(checksums.String()), 0o644); err != nil {
		return fmt.Errorf("写入 app bundle checksum 清单失败: %w", err)
	}
	return nil
}

func collectManifestFiles(root string, excluded map[string]bool) ([]ManifestFile, error) {
	files := make([]ManifestFile, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if excluded[relative] {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("app bundle 不能包含非普通文件: %s", relative)
		}
		digest, err := fileSHA256(path)
		if err != nil {
			return err
		}
		files = append(files, ManifestFile{Path: relative, SHA256: digest, Size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func copyStaticPublic(source, destination string) error {
	if _, err := os.Stat(source); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return os.MkdirAll(destination, 0o755)
		}
		relative = filepath.ToSlash(relative)
		if relative == "admin" {
			return filepath.SkipDir
		}
		if strings.HasPrefix(relative, "assets/captcha") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(destination, filepath.FromSlash(relative))
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("public 目录不能包含非普通文件: %s", relative)
		}
		return copyRegularFile(path, target, 0o644)
	})
}

func copyAdmin(source, destination string) error {
	if _, err := os.Stat(filepath.Join(source, "index.html")); err != nil {
		return fmt.Errorf("Admin 构建产物缺少 index.html: %w", err)
	}
	adminFiles, err := readAdminFiles(filepath.Join(source, "sparq-admin-manifest.json"))
	if err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return os.MkdirAll(destination, 0o755)
		}
		relative = filepath.ToSlash(relative)
		if relative == "plugin-assets" {
			return filepath.SkipDir
		}
		// __app__ 是二次开发应用的固定前端资源目录，不属于 Admin Shell
		// artifact manifest，但必须随发行包一起发布，才能让 frontend.mjs
		// 使用相对路径加载其动态 chunk。
		applicationFrontend := relative == "__app__" || strings.HasPrefix(relative, "__app__/")
		if !applicationFrontend && !adminPathIncluded(relative, entry.IsDir(), adminFiles) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(destination, filepath.FromSlash(relative))
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("public 目录不能包含非普通文件: %s", relative)
		}
		return copyRegularFile(path, target, 0o644)
	})
}

func copyDirectory(source, destination string) error {
	if _, err := os.Stat(source); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return os.MkdirAll(destination, 0o755)
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("目录不能包含非普通文件: %s", relative)
		}
		return copyRegularFile(path, target, 0o644)
	})
}

func readAdminFiles(path string) (map[string]bool, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 Admin artifact manifest 失败: %w", err)
	}
	var manifest adminManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("解析 Admin artifact manifest 失败: %w", err)
	}
	if manifest.Format != "sparq.admin.artifact.v1" || len(manifest.Files) == 0 {
		return nil, fmt.Errorf("Admin artifact manifest 缺少当前构建文件清单，请重新运行 build app --admin")
	}
	files := map[string]bool{"sparq-admin-manifest.json": true}
	for _, name := range manifest.Files {
		clean := pathpkg.Clean(name)
		if clean == "." || clean != name || pathpkg.IsAbs(name) || strings.Contains(name, "\\") || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "plugin-assets/") {
			return nil, fmt.Errorf("Admin artifact manifest 包含无效文件路径: %s", name)
		}
		if files[clean] {
			return nil, fmt.Errorf("Admin artifact manifest 包含重复文件路径: %s", name)
		}
		files[clean] = true
	}
	return files, nil
}

func adminPathIncluded(relative string, directory bool, files map[string]bool) bool {
	if files[relative] {
		return true
	}
	if !directory {
		return false
	}
	prefix := strings.TrimSuffix(relative, "/") + "/"
	for name := range files {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func copyRegularFile(source, destination string, mode fs.FileMode) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("不是普通文件")
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func writeTarGz(source, destination string) error {
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	gzipWriter := gzip.NewWriter(output)
	gzipWriter.Header.ModTime = time.Unix(0, 0).UTC()
	tarWriter := tar.NewWriter(gzipWriter)
	err = walkArchive(source, func(path, name string, info fs.FileInfo) error {
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = name
		header.ModTime = time.Unix(0, 0).UTC()
		header.AccessTime = time.Time{}
		header.ChangeTime = time.Time{}
		header.Uid = 0
		header.Gid = 0
		header.Uname = ""
		header.Gname = ""
		if info.IsDir() && !strings.HasSuffix(header.Name, "/") {
			header.Name += "/"
		}
		if err := tarWriter.WriteHeader(header); err != nil || info.IsDir() {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tarWriter, input)
		closeErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	return closeArchiveWriters(err, tarWriter.Close, gzipWriter.Close, output.Close)
}

func writeZip(source, destination string) error {
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	zipWriter := zip.NewWriter(output)
	err = walkArchive(source, func(path, name string, info fs.FileInfo) error {
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = name
		header.Modified = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
		header.Method = zip.Deflate
		if info.IsDir() {
			header.Name += "/"
			header.Method = zip.Store
		}
		writer, err := zipWriter.CreateHeader(header)
		if err != nil || info.IsDir() {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, input)
		closeErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	return closeArchiveWriters(err, zipWriter.Close, output.Close)
}

func walkArchive(source string, visit func(string, string, fs.FileInfo) error) error {
	parent := filepath.Dir(source)
	return filepath.Walk(source, func(path string, info fs.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("归档不能包含非普通文件: %s", path)
		}
		name, err := filepath.Rel(parent, path)
		if err != nil {
			return err
		}
		return visit(path, filepath.ToSlash(name), info)
	})
}

func closeArchiveWriters(operationErr error, closers ...func() error) error {
	result := operationErr
	for _, closeWriter := range closers {
		if err := closeWriter(); result == nil && err != nil {
			result = err
		}
	}
	return result
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

func resolveAppPath(appDir, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(appDir, path)
}

func resolveInputBinary(input, code string, target releasebuild.Platform, multiTarget bool) string {
	preferred := filepath.Join(input, platformBinaryName(code, target, multiTarget))
	if regularFile(preferred) {
		return preferred
	}
	legacy := filepath.Join(input, target.OS, target.Arch, platformBinaryName(code, target, false))
	if regularFile(legacy) {
		return legacy
	}
	return preferred
}

func platformBinaryName(code string, target releasebuild.Platform, qualified bool) string {
	name := code
	if qualified {
		name += "-" + target.OS + "-" + target.Arch
	}
	if target.OS == "windows" {
		return name + ".exe"
	}
	return name
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func regularDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
