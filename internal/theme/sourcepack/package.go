package sourcepack

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	protocolversion "github.com/zane-xin/sparq-protocol/protocol"
	pluginmanifest "github.com/zane-xin/sparq-tool/internal/plugin/manifest"
	"gopkg.in/yaml.v3"
)

const (
	maxArchiveBytes = 64 << 20
	maxFileBytes    = 16 << 20
	maxFileCount    = 2_000
)

var identityPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`)
var hexColorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
var assetExtensions = map[string]struct{}{
	".avif": {}, ".css": {}, ".eot": {}, ".gif": {}, ".ico": {}, ".jpeg": {}, ".jpg": {},
	".js": {}, ".json": {}, ".map": {}, ".mjs": {}, ".otf": {}, ".png": {}, ".svg": {},
	".ttf": {}, ".webp": {}, ".woff": {}, ".woff2": {},
}

var licenseExtensions = map[string]struct{}{
	".md": {}, ".txt": {},
}

type Options struct {
	SourceDir string
	OutputDir string
}

type Result struct {
	ArchivePath string
	Code        string
	Version     string
	SHA256      string
	SizeBytes   int64
	FileCount   int
}

type sourceManifest struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		ID      string `yaml:"id"`
		Code    string `yaml:"code"`
		Version string `yaml:"version"`
	} `yaml:"metadata"`
	Sparq struct {
		Protocol string `yaml:"protocol"`
	} `yaml:"sparq"`
}

type runtimeManifest struct {
	Code       string `yaml:"code"`
	Name       string `yaml:"name"`
	Version    string `yaml:"version"`
	Appearance struct {
		ColorSchemes []string          `yaml:"colorSchemes"`
		Palette      map[string]string `yaml:"palette"`
	} `yaml:"appearance"`
	FormFactors []string `yaml:"formFactors"`
}

func Create(options Options) (Result, error) {
	root, err := filepath.Abs(defaultValue(strings.TrimSpace(options.SourceDir), "."))
	if err != nil {
		return Result{}, fmt.Errorf("解析主题目录失败: %w", err)
	}
	if info, statErr := os.Stat(root); statErr != nil || !info.IsDir() {
		return Result{}, fmt.Errorf("主题源码路径必须是目录: %s", root)
	}
	source, runtime, err := loadManifests(root)
	if err != nil {
		return Result{}, err
	}
	for _, required := range []string{"views", "assets"} {
		info, statErr := os.Stat(filepath.Join(root, required))
		if statErr != nil || !info.IsDir() {
			return Result{}, fmt.Errorf("主题目录缺少必需目录 %s", required)
		}
	}
	outputDir := strings.TrimSpace(options.OutputDir)
	if outputDir == "" {
		return Result{}, fmt.Errorf("主题包输出目录不能为空")
	}
	outputDir, err = filepath.Abs(outputDir)
	if err != nil {
		return Result{}, fmt.Errorf("解析主题包输出目录失败: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return Result{}, fmt.Errorf("创建主题包输出目录失败: %w", err)
	}
	archivePath := filepath.Join(outputDir, source.Metadata.Code+"-"+source.Metadata.Version+"-theme.zip")
	file, err := os.OpenFile(archivePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Result{}, fmt.Errorf("创建主题包失败: %w", err)
	}
	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(archivePath)
		}
	}()
	hash := sha256.New()
	limited := &limitedWriter{writer: io.MultiWriter(file, hash), limit: maxArchiveBytes}
	writer := zip.NewWriter(limited)
	count, err := writeTree(writer, root)
	if err != nil {
		_ = writer.Close()
		return Result{}, err
	}
	if err := writer.Close(); err != nil {
		return Result{}, fmt.Errorf("完成主题包压缩失败: %w", err)
	}
	if err := file.Sync(); err != nil {
		return Result{}, fmt.Errorf("同步主题包失败: %w", err)
	}
	if err := file.Close(); err != nil {
		return Result{}, fmt.Errorf("关闭主题包失败: %w", err)
	}
	success = true
	return Result{ArchivePath: archivePath, Code: runtime.Code, Version: runtime.Version, SHA256: hex.EncodeToString(hash.Sum(nil)), SizeBytes: limited.written, FileCount: count}, nil
}

func loadManifests(root string) (sourceManifest, runtimeManifest, error) {
	var source sourceManifest
	if err := loadYAML(filepath.Join(root, "template.yaml"), &source); err != nil {
		return sourceManifest{}, runtimeManifest{}, fmt.Errorf("主题上架清单无效: %w", err)
	}
	var runtime runtimeManifest
	if err := loadYAML(filepath.Join(root, "theme.yaml"), &runtime); err != nil {
		return sourceManifest{}, runtimeManifest{}, fmt.Errorf("主题运行清单无效: %w", err)
	}
	_, versionErr := pluginmanifest.ParseVersion(source.Metadata.Version)
	if source.APIVersion != "sparq.io/v1" || source.Kind != "Template" || !identityPattern.MatchString(source.Metadata.Code) ||
		source.Metadata.ID != source.Metadata.Code || versionErr != nil || protocolversion.ValidateRange(source.Sparq.Protocol) != nil {
		return sourceManifest{}, runtimeManifest{}, fmt.Errorf("template.yaml 的身份、版本或 sparq.protocol 无效")
	}
	if runtime.Code != source.Metadata.Code || runtime.Version != source.Metadata.Version || strings.TrimSpace(runtime.Name) == "" {
		return sourceManifest{}, runtimeManifest{}, fmt.Errorf("theme.yaml 与 template.yaml 的 code 或 version 不一致")
	}
	if !validThemeCapabilities(runtime) {
		return sourceManifest{}, runtimeManifest{}, fmt.Errorf("theme.yaml 的终端适配或色彩能力无效")
	}
	return source, runtime, nil
}

func validThemeCapabilities(manifest runtimeManifest) bool {
	if !validClosedValues(manifest.FormFactors, map[string]struct{}{"desktop": {}, "tablet": {}, "mobile": {}}) ||
		!validClosedValues(manifest.Appearance.ColorSchemes, map[string]struct{}{"light": {}, "dark": {}}) {
		return false
	}
	primary := strings.TrimSpace(manifest.Appearance.Palette["primary"])
	if !hexColorPattern.MatchString(primary) {
		return false
	}
	for name, color := range manifest.Appearance.Palette {
		if !identityPattern.MatchString(name) || !hexColorPattern.MatchString(strings.TrimSpace(color)) {
			return false
		}
	}
	return true
}

func validClosedValues(values []string, allowed map[string]struct{}) bool {
	if len(values) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, ok := allowed[value]; !ok {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func loadYAML(path string, output any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(body) == 0 || len(body) > 1<<20 {
		return fmt.Errorf("清单为空或超过 1 MB")
	}
	return yaml.Unmarshal(body, output)
}

func writeTree(writer *zip.Writer, root string) (int, error) {
	var files []string
	var expanded int64
	hasView := false
	err := filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, current)
		if err != nil || relative == "." {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") && relative != "." {
				return filepath.SkipDir
			}
			included, err := selectPath(relative, true)
			if err != nil {
				return err
			}
			if !included {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		included, err := selectPath(relative, false)
		if err != nil {
			return err
		}
		if !included {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("主题包不允许特殊文件: %s", relative)
		}
		if info.Size() > maxFileBytes || expanded > maxArchiveBytes-info.Size() {
			return fmt.Errorf("主题文件大小超过限制: %s", relative)
		}
		expanded += info.Size()
		files = append(files, relative)
		hasView = hasView || strings.HasPrefix(relative, "views/")
		if len(files) > maxFileCount {
			return fmt.Errorf("主题文件数量超过 %d 个限制", maxFileCount)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if !hasView {
		return 0, fmt.Errorf("主题 views 目录至少需要一个模板文件")
	}
	sort.Strings(files)
	directories := []string{"assets/", "views/"}
	if info, err := os.Stat(filepath.Join(root, "licenses")); err == nil && info.IsDir() {
		directories = append(directories, "licenses/")
	}
	for _, directory := range directories {
		header := &zip.FileHeader{Name: directory, Method: zip.Store}
		header.SetMode(fs.ModeDir | 0o755)
		header.Modified = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
		if _, err := writer.CreateHeader(header); err != nil {
			return 0, err
		}
	}
	for _, relative := range files {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			return 0, err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return 0, err
		}
		header.Name = relative
		header.Method = zip.Deflate
		header.Modified = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
		target, err := writer.CreateHeader(header)
		if err != nil {
			return 0, err
		}
		file, err := os.Open(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			return 0, err
		}
		_, copyErr := io.Copy(target, file)
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil {
			return 0, fmt.Errorf("写入主题文件 %s 失败: %v", relative, errors.Join(copyErr, closeErr))
		}
	}
	return len(files), nil
}

func selectPath(relative string, directory bool) (bool, error) {
	segments := strings.Split(relative, "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." || strings.HasPrefix(segment, ".") {
			return false, fmt.Errorf("主题包包含无效路径: %s", relative)
		}
	}
	if relative == "template.yaml" || relative == "theme.yaml" || relative == "cover.png" {
		if directory {
			return false, fmt.Errorf("主题清单或封面不能是目录: %s", relative)
		}
		return true, nil
	}
	top := segments[0]
	if top != "views" && top != "assets" && top != "licenses" {
		return false, nil
	}
	if directory {
		return true, nil
	}
	extension := strings.ToLower(filepath.Ext(relative))
	if top == "views" && extension != ".html" && extension != ".tpl" {
		return false, fmt.Errorf("主题模板文件 %s 的类型 %s 不受支持，只允许 .html 和 .tpl", relative, extension)
	}
	if top == "assets" {
		if _, allowed := assetExtensions[extension]; !allowed {
			return false, fmt.Errorf("主题资源文件 %s 的类型 %s 不受支持；许可证文本请放入 licenses/ 目录", relative, extension)
		}
	}
	if top == "licenses" {
		if _, allowed := licenseExtensions[extension]; !allowed {
			return false, fmt.Errorf("主题许可证文件 %s 的类型 %s 不受支持，只允许 .txt 和 .md", relative, extension)
		}
	}
	return true, nil
}

func defaultValue(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

type limitedWriter struct {
	writer  io.Writer
	limit   int64
	written int64
}

func (writer *limitedWriter) Write(body []byte) (int, error) {
	if int64(len(body)) > writer.limit-writer.written {
		return 0, fmt.Errorf("主题包压缩后超过 %d MB 限制", writer.limit>>20)
	}
	written, err := writer.writer.Write(body)
	writer.written += int64(written)
	return written, err
}
