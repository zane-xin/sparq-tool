package sourcepack

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zane-xin/sparq-tool/internal/plugin/manifest"
)

const (
	maxArchiveEntries = 10_000
	maxExpandedBytes  = 512 << 20
	maxArchiveBytes   = 100 << 20
)

// Options 描述插件源码上传包的输入和输出目录。
type Options struct {
	SourceDir string
	OutputDir string
}

// Result 保存源码包路径以及上传协议需要的清单摘要。
type Result struct {
	ArchivePath string
	Code        string
	Version     string
	SHA256      string
	SizeBytes   int64
	FileCount   int
}

// FingerprintResult 是排除构建输出后得到的确定性源码输入摘要，用于判断候选构建包能否复用。
type FingerprintResult struct {
	SHA256    string
	FileCount int
}

// Fingerprint 计算实际构建输入的内容摘要；生成文件不参与摘要，避免构建动作自身使缓存失效。
func Fingerprint(sourceDir string) (FingerprintResult, error) {
	root, item, ignore, err := resolveSourceTree(sourceDir)
	if err != nil {
		return FingerprintResult{}, err
	}
	hash := sha256.New()
	if _, err := hash.Write([]byte("sparq.plugin.build-input.v1\x00")); err != nil {
		return FingerprintResult{}, err
	}
	files := 0
	err = filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		relative = filepath.ToSlash(relative)
		if ignore.excluded(relative, entry.IsDir()) || generatedBuildInput(relative) {
			if entry.IsDir() && (protectedSourcePath(relative, true) || generatedBuildInput(relative)) {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("读取源码文件 %s 失败: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("源码输入不允许包含符号链接: %s", relative)
		}
		if entry.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("源码输入不允许包含特殊文件: %s", relative)
		}
		files++
		if files > maxArchiveEntries {
			return fmt.Errorf("源码文件数量超过 %d 个限制", maxArchiveEntries)
		}
		if err := writeFingerprintValue(hash, []byte(relative)); err != nil {
			return err
		}
		var metadata [12]byte
		binary.BigEndian.PutUint32(metadata[:4], uint32(info.Mode().Perm()))
		binary.BigEndian.PutUint64(metadata[4:], uint64(info.Size()))
		if _, err := hash.Write(metadata[:]); err != nil {
			return err
		}
		file, err := os.Open(current)
		if err != nil {
			return fmt.Errorf("打开源码文件 %s 失败: %w", relative, err)
		}
		written, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return fmt.Errorf("计算源码文件 %s 摘要失败: %w", relative, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("关闭源码文件 %s 失败: %w", relative, closeErr)
		}
		if written != info.Size() {
			return fmt.Errorf("计算源码文件 %s 摘要时内容发生变化", relative)
		}
		return nil
	})
	if err != nil {
		return FingerprintResult{}, err
	}
	if files == 0 || item.Metadata.Code == "" {
		return FingerprintResult{}, fmt.Errorf("插件源码输入为空")
	}
	return FingerprintResult{SHA256: hex.EncodeToString(hash.Sum(nil)), FileCount: files}, nil
}

func writeFingerprintValue(writer io.Writer, value []byte) error {
	if uint64(len(value)) > uint64(^uint32(0)) {
		return fmt.Errorf("源码输入路径超过摘要编码限制")
	}
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(value)))
	if _, err := writer.Write(size[:]); err != nil {
		return err
	}
	_, err := writer.Write(value)
	return err
}

func generatedBuildInput(relative string) bool {
	return relative == manifest.DefaultGeneratedFileName || relative == "frontend/artifact.json"
}

// Create 校验插件清单并创建安全、确定性的 tar.gz 源码包。
func Create(options Options) (Result, error) {
	root, item, ignore, err := resolveSourceTree(options.SourceDir)
	if err != nil {
		return Result{}, err
	}
	manifestPath := filepath.Join(root, manifest.DefaultFileName)
	generated, err := manifest.GenerateGo(manifest.GoGenerateOptions{ManifestPath: manifestPath})
	if err != nil {
		return Result{}, fmt.Errorf("生成插件 Manifest 校验内容失败: %w", err)
	}
	if err := manifest.CheckGo(generated); err != nil {
		return Result{}, err
	}
	if err := requireRegularFile(filepath.Join(root, "go.mod")); err != nil {
		return Result{}, err
	}
	outputDir := strings.TrimSpace(options.OutputDir)
	if outputDir == "" {
		return Result{}, fmt.Errorf("源码包输出目录不能为空")
	}
	outputDir, err = filepath.Abs(outputDir)
	if err != nil {
		return Result{}, fmt.Errorf("解析源码包输出目录失败: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return Result{}, fmt.Errorf("创建源码包输出目录失败: %w", err)
	}
	archivePath := filepath.Join(outputDir, item.Metadata.Code+"-"+item.Metadata.Version+"-source.tar.gz")
	file, err := os.OpenFile(archivePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Result{}, fmt.Errorf("创建源码包 %s 失败: %w", archivePath, err)
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
	gzipWriter := gzip.NewWriter(limited)
	gzipWriter.Header.ModTime = time.Unix(0, 0)
	tarWriter := tar.NewWriter(gzipWriter)
	fileCount, err := writeTree(tarWriter, root, item.Metadata.Code, ignore)
	if err != nil {
		_ = tarWriter.Close()
		_ = gzipWriter.Close()
		return Result{}, err
	}
	if err := tarWriter.Close(); err != nil {
		_ = gzipWriter.Close()
		return Result{}, fmt.Errorf("完成源码包归档失败: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return Result{}, fmt.Errorf("完成源码包压缩失败: %w", err)
	}
	if err := file.Sync(); err != nil {
		return Result{}, fmt.Errorf("同步源码包失败: %w", err)
	}
	if err := file.Close(); err != nil {
		return Result{}, fmt.Errorf("关闭源码包失败: %w", err)
	}
	success = true
	return Result{
		ArchivePath: archivePath,
		Code:        item.Metadata.Code,
		Version:     item.Metadata.Version,
		SHA256:      hex.EncodeToString(hash.Sum(nil)),
		SizeBytes:   limited.written,
		FileCount:   fileCount,
	}, nil
}

func resolveSourceTree(sourceDir string) (string, manifest.Manifest, ignoreMatcher, error) {
	sourceDir = strings.TrimSpace(sourceDir)
	if sourceDir == "" {
		sourceDir = "."
	}
	root, err := filepath.Abs(sourceDir)
	if err != nil {
		return "", manifest.Manifest{}, ignoreMatcher{}, fmt.Errorf("解析插件目录失败: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", manifest.Manifest{}, ignoreMatcher{}, fmt.Errorf("读取插件目录 %s 失败: %w", root, err)
	}
	if !info.IsDir() {
		return "", manifest.Manifest{}, ignoreMatcher{}, fmt.Errorf("插件源码路径必须是目录: %s", root)
	}
	item, err := manifest.Load(filepath.Join(root, manifest.DefaultFileName))
	if err != nil {
		return "", manifest.Manifest{}, ignoreMatcher{}, fmt.Errorf("插件清单无效: %w", err)
	}
	ignore, err := loadIgnoreMatcher(root, item.ReferencedFiles())
	if err != nil {
		return "", manifest.Manifest{}, ignoreMatcher{}, err
	}
	return root, item, ignore, nil
}

func writeTree(writer *tar.Writer, root, code string, ignore ignoreMatcher) (int, error) {
	entries := 0
	var expanded int64
	err := filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		relative = filepath.ToSlash(relative)
		if ignore.excluded(relative, entry.IsDir()) {
			if entry.IsDir() && protectedSourcePath(relative, true) {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("读取源码文件 %s 失败: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("源码包不允许包含符号链接: %s", relative)
		}
		if entry.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("源码包不允许包含特殊文件: %s", relative)
		}
		entries++
		if entries > maxArchiveEntries {
			return fmt.Errorf("源码文件数量超过 %d 个限制", maxArchiveEntries)
		}
		if info.Size() < 0 || info.Size() > maxExpandedBytes || expanded > maxExpandedBytes-info.Size() {
			return fmt.Errorf("源码文件总大小超过 %d MB 限制", maxExpandedBytes>>20)
		}
		expanded += info.Size()
		header := &tar.Header{
			Name: code + "/" + relative, Mode: int64(info.Mode().Perm()), Size: info.Size(),
			Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0),
		}
		if err := writer.WriteHeader(header); err != nil {
			return fmt.Errorf("写入源码文件头 %s 失败: %w", relative, err)
		}
		file, err := os.Open(current)
		if err != nil {
			return fmt.Errorf("打开源码文件 %s 失败: %w", relative, err)
		}
		_, copyErr := io.Copy(writer, file)
		closeErr := file.Close()
		if copyErr != nil {
			return fmt.Errorf("写入源码文件 %s 失败: %w", relative, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("关闭源码文件 %s 失败: %w", relative, closeErr)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return entries, nil
}

func requireRegularFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("插件源码缺少必需文件 %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("插件源码必需文件不是普通文件: %s", path)
	}
	return nil
}

type limitedWriter struct {
	writer  io.Writer
	limit   int64
	written int64
}

func (w *limitedWriter) Write(body []byte) (int, error) {
	if int64(len(body)) > w.limit-w.written {
		return 0, fmt.Errorf("源码包压缩后超过 %d MB 限制", w.limit>>20)
	}
	written, err := w.writer.Write(body)
	w.written += int64(written)
	return written, err
}
