package packager

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	packageFormatV1 = "sparq.plugin.package.v1"
	manifestPath    = "manifest.json"
	releasePath     = "release.json"
	checksumsPath   = "checksums.txt"
	toolName        = "sparq-tool"
	toolVersion     = "dev"
)

const (
	// DeliveryKindFrontendBundle 表示与操作系统无关的独立前端交付包。
	DeliveryKindFrontendBundle = "frontend_bundle"
	// DeliveryKindManagedRemote 表示绑定 GOOS/GOARCH，且由宿主管理的 Remote Runner 组合交付包。
	DeliveryKindManagedRemote = "managed_remote"
)

// PluginPackageOptions 描述插件打包参数。
type PluginPackageOptions struct {
	SourceDir     string
	OutputDir     string
	Code          string
	Name          string
	Version       string
	ProtocolRange string
	ManifestJSON  []byte
	CreatedAt     time.Time
	DeliveryKind  string
	Runtime       string
	TargetOS      string
	TargetArch    string
	BackendPath   string
	FrontendPath  string
	FrontendOnly  bool
	// ReferencedFiles 是 plugin.yaml 引用且必须随主清单一起发布的插件内文件。
	ReferencedFiles []string
}

// PluginPackageResult 描述插件打包结果。
type PluginPackageResult struct {
	ArchivePath  string
	ChecksumPath string
}

// PackagePlugin 生成插件 package v1 tar.gz 包。
func PackagePlugin(options PluginPackageOptions) (PluginPackageResult, error) {
	code := strings.TrimSpace(options.Code)
	if code == "" {
		return PluginPackageResult{}, fmt.Errorf("插件 code 不能为空")
	}
	version := strings.TrimSpace(options.Version)
	if version == "" {
		return PluginPackageResult{}, fmt.Errorf("插件版本不能为空")
	}
	if len(options.ManifestJSON) == 0 {
		return PluginPackageResult{}, fmt.Errorf("插件协议清单内容不能为空")
	}
	sourceDir := defaultString(strings.TrimSpace(options.SourceDir), ".")
	outputDir := defaultString(strings.TrimSpace(options.OutputDir), filepath.Join(sourceDir, "release"))
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return PluginPackageResult{}, err
	}
	stagingDir, err := os.MkdirTemp("", "sparq-plugin-package-*")
	if err != nil {
		return PluginPackageResult{}, fmt.Errorf("创建插件发布包临时目录失败: %w", err)
	}
	defer os.RemoveAll(stagingDir)
	metadata := pluginPackageMetadata{
		Code:            code,
		Name:            options.Name,
		Version:         version,
		ProtocolRange:   options.ProtocolRange,
		ManifestJSON:    options.ManifestJSON,
		ReferencedFiles: options.ReferencedFiles,
		CreatedAt:       options.CreatedAt,
		DeliveryKind:    strings.TrimSpace(options.DeliveryKind),
		Runtime:         strings.TrimSpace(options.Runtime),
		TargetOS:        strings.ToLower(strings.TrimSpace(options.TargetOS)),
		TargetArch:      strings.ToLower(strings.TrimSpace(options.TargetArch)),
		BackendPath:     strings.TrimSpace(options.BackendPath),
		FrontendPath:    strings.TrimSpace(options.FrontendPath),
		FrontendOnly:    options.FrontendOnly,
	}
	if err := validateDelivery(metadata); err != nil {
		return PluginPackageResult{}, err
	}
	release, err := stagePackage(stagingDir, sourceDir, metadata)
	if err != nil {
		return PluginPackageResult{}, err
	}
	archivePath := filepath.Join(outputDir, packageFilename(metadata))
	if err := writeArchive(stagingDir, archivePath); err != nil {
		return PluginPackageResult{}, err
	}
	return PluginPackageResult{ArchivePath: archivePath, ChecksumPath: release.ChecksumPath}, nil
}

type pluginPackageMetadata struct {
	Code            string
	Name            string
	Version         string
	ProtocolRange   string
	ManifestJSON    []byte
	ReferencedFiles []string
	CreatedAt       time.Time
	DeliveryKind    string
	Runtime         string
	TargetOS        string
	TargetArch      string
	BackendPath     string
	FrontendPath    string
	FrontendOnly    bool
}

type stagedRelease struct {
	ChecksumPath string
}

type releaseJSON struct {
	Format      string               `json:"format"`
	Plugin      releasePluginJSON    `json:"plugin"`
	Protocol    releaseProtocolJSON  `json:"protocol"`
	Artifacts   releaseArtifactsJSON `json:"artifacts"`
	Delivery    *releaseDeliveryJSON `json:"delivery,omitempty"`
	SourceFiles []string             `json:"source_files"`
	CreatedAt   string               `json:"created_at"`
	Tool        releaseToolJSON      `json:"tool"`
}

type releaseDeliveryJSON struct {
	Kind       string `json:"kind"`
	Runtime    string `json:"runtime,omitempty"`
	TargetOS   string `json:"target_os,omitempty"`
	TargetArch string `json:"target_arch,omitempty"`
}

type releasePluginJSON struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type releaseProtocolJSON struct {
	Range string `json:"range"`
}

type releaseArtifactsJSON struct {
	Manifest string `json:"manifest"`
	Backend  string `json:"backend,omitempty"`
	Frontend string `json:"frontend,omitempty"`
}

type releaseToolJSON struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func stagePackage(stagingDir string, sourceDir string, metadata pluginPackageMetadata) (stagedRelease, error) {
	sourceFiles, err := sourceFileEntries(metadata.ReferencedFiles)
	if err != nil {
		return stagedRelease{}, err
	}
	copied := make(map[string]struct{})
	for _, relative := range sourceFiles {
		if err := copyPackageFile(stagingDir, sourceDir, relative, 0o644, copied); err != nil {
			return stagedRelease{}, err
		}
	}
	if exists(filepath.Join(sourceDir, "README.md")) {
		if err := copyPackageFile(stagingDir, sourceDir, "README.md", 0o644, copied); err != nil {
			return stagedRelease{}, err
		}
	}
	artifacts := releaseArtifactsJSON{Manifest: manifestPath}
	backend := ""
	if !metadata.FrontendOnly {
		backend, err = copyBackend(stagingDir, sourceDir, metadata.Code, metadata.BackendPath, copied)
		if err != nil {
			return stagedRelease{}, err
		}
		if metadata.DeliveryKind == DeliveryKindManagedRemote && backend == "" {
			return stagedRelease{}, fmt.Errorf("Managed Remote 交付包缺少后端文件: %s", metadata.BackendPath)
		}
	}
	artifacts.Backend = backend
	frontend := ""
	if metadata.DeliveryKind == "" || metadata.FrontendOnly || metadata.DeliveryKind == DeliveryKindManagedRemote {
		frontend, err = copyFrontendOutput(stagingDir, sourceDir, metadata.FrontendPath, copied)
		if err != nil {
			return stagedRelease{}, err
		}
		if (metadata.DeliveryKind == DeliveryKindFrontendBundle || metadata.DeliveryKind == DeliveryKindManagedRemote) && frontend == "" {
			return stagedRelease{}, fmt.Errorf("%s 交付包缺少前端构建产物", metadata.DeliveryKind)
		}
	}
	artifacts.Frontend = frontend
	if err := writePackageFile(stagingDir, manifestPath, appendTrailingNewline(metadata.ManifestJSON), 0o644, copied); err != nil {
		return stagedRelease{}, err
	}
	createdAt := metadata.CreatedAt
	if createdAt.IsZero() {
		// 未显式提供构建时间时使用固定值，保证相同输入的候选包可重复发布。
		createdAt = time.Unix(0, 0).UTC()
	} else {
		createdAt = createdAt.UTC()
	}
	releaseMetadata := releaseJSON{
		Format: packageFormatV1,
		Plugin: releasePluginJSON{
			Code:    metadata.Code,
			Name:    metadata.Name,
			Version: metadata.Version,
		},
		Protocol:    releaseProtocolJSON{Range: metadata.ProtocolRange},
		Artifacts:   artifacts,
		SourceFiles: sourceFiles,
		CreatedAt:   createdAt.Format(time.RFC3339),
		Tool:        releaseToolJSON{Name: toolName, Version: toolVersion},
	}
	if metadata.DeliveryKind != "" {
		releaseMetadata.Delivery = &releaseDeliveryJSON{
			Kind: metadata.DeliveryKind, Runtime: metadata.Runtime,
			TargetOS: metadata.TargetOS, TargetArch: metadata.TargetArch,
		}
	}
	releaseBody, err := json.MarshalIndent(releaseMetadata, "", "  ")
	if err != nil {
		return stagedRelease{}, fmt.Errorf("编码插件发布元数据失败: %w", err)
	}
	if err := writePackageFile(stagingDir, releasePath, appendTrailingNewline(releaseBody), 0o644, copied); err != nil {
		return stagedRelease{}, err
	}
	entries, err := checksumEntries(stagingDir)
	if err != nil {
		return stagedRelease{}, err
	}
	body := checksumBody(entries)
	if err := writePackageFile(stagingDir, checksumsPath, []byte(body), 0o644, copied); err != nil {
		return stagedRelease{}, err
	}
	return stagedRelease{ChecksumPath: checksumsPath}, nil
}

func sourceFileEntries(referencedFiles []string) ([]string, error) {
	seen := map[string]struct{}{"plugin.yaml": {}}
	entries := []string{"plugin.yaml"}
	references := make([]string, 0, len(referencedFiles))
	for _, reference := range referencedFiles {
		cleaned, err := cleanReferencedFile(reference)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[cleaned]; exists {
			continue
		}
		seen[cleaned] = struct{}{}
		references = append(references, cleaned)
	}
	sort.Strings(references)
	entries = append(entries, references...)
	return entries, nil
}

func copyBackend(stagingDir string, sourceDir string, code string, explicitPath string, copied map[string]struct{}) (string, error) {
	name := binaryName(code)
	sources := []string{
		filepath.Join(sourceDir, "release", "bin", name),
		filepath.Join(sourceDir, "bin", name),
	}
	if explicitPath != "" {
		path := explicitPath
		if !filepath.IsAbs(path) {
			path = filepath.Join(sourceDir, path)
		}
		sources = []string{path}
		if strings.EqualFold(filepath.Ext(path), ".exe") {
			name = code + ".exe"
		} else {
			name = code
		}
	}
	for _, source := range sources {
		if !exists(source) {
			continue
		}
		target := filepath.ToSlash(filepath.Join("bin", name))
		if err := copyFileToPackage(stagingDir, source, target, 0o755, copied); err != nil {
			return "", err
		}
		return target, nil
	}
	return "", nil
}

func validateDelivery(metadata pluginPackageMetadata) error {
	switch metadata.DeliveryKind {
	case "":
		if metadata.Runtime != "" || metadata.TargetOS != "" || metadata.TargetArch != "" || metadata.BackendPath != "" || metadata.FrontendPath != "" || metadata.FrontendOnly {
			return fmt.Errorf("未指定交付类型时不能设置交付变体参数")
		}
		return nil
	case DeliveryKindFrontendBundle:
		if metadata.Runtime != "" || metadata.TargetOS != "" || metadata.TargetArch != "" || metadata.BackendPath != "" || metadata.FrontendPath == "" || !metadata.FrontendOnly {
			return fmt.Errorf("前端交付包不能包含 Runtime、目标平台或后端文件")
		}
	case DeliveryKindManagedRemote:
		if metadata.Runtime != "http" || metadata.TargetOS == "" || metadata.TargetArch == "" || metadata.BackendPath == "" || metadata.FrontendPath == "" || metadata.FrontendOnly {
			return fmt.Errorf("Managed Remote 交付包必须包含 Runtime、目标平台、后端文件和前端构建产物")
		}
	default:
		return fmt.Errorf("不支持的插件交付类型 %q", metadata.DeliveryKind)
	}
	return nil
}

func packageFilename(metadata pluginPackageMetadata) string {
	base := metadata.Code + "-" + metadata.Version
	switch metadata.DeliveryKind {
	case DeliveryKindFrontendBundle:
		base += "-frontend"
	case DeliveryKindManagedRemote:
		base += "-" + metadata.TargetOS + "-" + metadata.TargetArch
	}
	return base + ".tar.gz"
}

func copyFrontendOutput(stagingDir string, sourceDir string, explicitPath string, copied map[string]struct{}) (string, error) {
	source := filepath.Join(sourceDir, "frontend", "dist")
	if explicitPath != "" {
		source = explicitPath
		if !filepath.IsAbs(source) {
			source = filepath.Join(sourceDir, source)
		}
	}
	if !exists(source) {
		return "", nil
	}
	before := len(copied)
	if err := copyFrontendDirectory(stagingDir, source, "frontend", copied); err != nil {
		return "", err
	}
	if len(copied) == before {
		return "", nil
	}
	return "frontend", nil
}

func copyFrontendDirectory(stagingDir string, sourceDir string, targetRelative string, copied map[string]struct{}) error {
	return filepath.Walk(sourceDir, func(sourcePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if sourcePath != sourceDir && info.Name() == ".vite" {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Name() == ".vite" {
			return nil
		}
		relative, err := filepath.Rel(sourceDir, sourcePath)
		if err != nil {
			return err
		}
		target := filepath.ToSlash(filepath.Join(targetRelative, relative))
		return copyFileToPackage(stagingDir, sourcePath, target, 0o644, copied)
	})
}

func copyPackageFile(stagingDir string, sourceDir string, relative string, mode os.FileMode, copied map[string]struct{}) error {
	sourcePath := filepath.Join(sourceDir, filepath.FromSlash(relative))
	if !exists(sourcePath) {
		return fmt.Errorf("插件发布文件不存在: %s", relative)
	}
	return copyFileToPackage(stagingDir, sourcePath, relative, mode, copied)
}

func copyFileToPackage(stagingDir string, sourcePath string, relative string, mode os.FileMode, copied map[string]struct{}) error {
	cleaned, err := cleanReferencedFile(relative)
	if err != nil {
		return err
	}
	if _, exists := copied[cleaned]; exists {
		return fmt.Errorf("插件发布包路径重复: %s", cleaned)
	}
	if err := copyFile(sourcePath, filepath.Join(stagingDir, filepath.FromSlash(cleaned)), mode); err != nil {
		return err
	}
	copied[cleaned] = struct{}{}
	return nil
}

func writePackageFile(stagingDir string, relative string, body []byte, mode os.FileMode, copied map[string]struct{}) error {
	cleaned, err := cleanReferencedFile(relative)
	if err != nil {
		return err
	}
	if _, exists := copied[cleaned]; exists {
		return fmt.Errorf("插件发布包路径重复: %s", cleaned)
	}
	path := filepath.Join(stagingDir, filepath.FromSlash(cleaned))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, body, mode); err != nil {
		return err
	}
	copied[cleaned] = struct{}{}
	return nil
}

func copyFile(source string, target string, mode os.FileMode) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("插件发布文件不能是符号链接: %s", source)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("插件发布文件必须是普通文件: %s", source)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	sourceFile, err := os.Open(source)
	if err != nil {
		return err
	}
	defer sourceFile.Close()
	targetFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer targetFile.Close()
	if _, err := io.Copy(targetFile, sourceFile); err != nil {
		return err
	}
	return targetFile.Chmod(mode)
}

type checksumEntry struct {
	Path string
	Sum  string
}

func checksumEntries(stagingDir string) ([]checksumEntry, error) {
	entries := []checksumEntry{}
	if err := filepath.WalkDir(stagingDir, func(current string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(stagingDir, current)
		if err != nil {
			return err
		}
		packagePath := filepath.ToSlash(relative)
		if packagePath == checksumsPath || strings.HasPrefix(packagePath, "signatures/") {
			return nil
		}
		sum, err := fileSHA256(current)
		if err != nil {
			return err
		}
		entries = append(entries, checksumEntry{Path: packagePath, Sum: sum})
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func checksumBody(entries []checksumEntry) string {
	var builder strings.Builder
	for _, entry := range entries {
		fmt.Fprintf(&builder, "%s  %s\n", entry.Sum, entry.Path)
	}
	return builder.String()
}

func writeArchive(sourceDir string, archivePath string) error {
	file, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipWriter := gzip.NewWriter(file)
	defer gzipWriter.Close()
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()
	entries, err := archiveEntries(sourceDir)
	if err != nil {
		return err
	}
	for _, relative := range entries {
		path := filepath.Join(sourceDir, relative)
		if err := addPath(tarWriter, sourceDir, path); err != nil {
			return err
		}
	}
	return nil
}

func cleanReferencedFile(reference string) (string, error) {
	if strings.Contains(reference, "\\") {
		return "", fmt.Errorf("Manifest 引用文件路径必须使用正斜杠: %s", reference)
	}
	cleaned := path.Clean(reference)
	if cleaned == "." || path.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("Manifest 引用文件必须位于插件目录内: %s", reference)
	}
	return cleaned, nil
}

func archiveEntries(sourceDir string) ([]string, error) {
	entries := []string{}
	if err := filepath.WalkDir(sourceDir, func(current string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("插件发布包文件必须是普通文件: %s", current)
		}
		relative, err := filepath.Rel(sourceDir, current)
		if err != nil {
			return err
		}
		entries = append(entries, filepath.ToSlash(relative))
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Strings(entries)
	return entries, nil
}

func addPath(writer *tar.Writer, sourceDir string, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return filepath.WalkDir(path, func(current string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			return addFile(writer, sourceDir, current)
		})
	}
	return addFile(writer, sourceDir, path)
}

func addFile(writer *tar.Writer, sourceDir string, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(sourceDir, path)
	if err != nil {
		return err
	}
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	header.Name = filepath.ToSlash(relative)
	// 发布包必须字节级确定；文件系统 mtime、用户名和组名不能进入包摘要。
	header.ModTime = time.Unix(0, 0).UTC()
	header.AccessTime = time.Time{}
	header.ChangeTime = time.Time{}
	header.Uid = 0
	header.Gid = 0
	header.Uname = ""
	header.Gname = ""
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(writer, file)
	return err
}

func fileSHA256(path string) (string, error) {
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

func appendTrailingNewline(content []byte) []byte {
	if len(content) == 0 || content[len(content)-1] == '\n' {
		return content
	}
	output := make([]byte, 0, len(content)+1)
	output = append(output, content...)
	return append(output, '\n')
}

func binaryName(code string) string {
	if runtime.GOOS == "windows" {
		return code + ".exe"
	}
	return code
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
