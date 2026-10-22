package checksum

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Entry 描述一个文件的 sha256 校验结果。
type Entry struct {
	Path string
	Sum  string
}

// Create 为文件或目录生成 sha256 清单，目录会递归展开普通文件。
func Create(paths []string) ([]Entry, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("至少需要提供一个文件或目录")
	}
	files, err := expandFiles(paths)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(files))
	for _, path := range files {
		sum, err := fileSHA256(path)
		if err != nil {
			return nil, err
		}
		entries = append(entries, Entry{
			Path: filepath.ToSlash(path),
			Sum:  sum,
		})
	}
	return entries, nil
}

// Write 写入 checksum 清单，格式兼容常见 sha256sum 输出。
func Write(path string, entries []Entry) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("输出文件不能为空")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	for _, entry := range entries {
		if _, err := fmt.Fprintf(file, "%s  %s\n", entry.Sum, filepath.ToSlash(entry.Path)); err != nil {
			return err
		}
	}
	return nil
}

// RelativeTo 将清单路径转换为相对 baseDir 的路径，便于清单随产物目录移动。
func RelativeTo(baseDir string, entries []Entry) ([]Entry, error) {
	if strings.TrimSpace(baseDir) == "" {
		baseDir = "."
	}
	output := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		path := filepath.FromSlash(entry.Path)
		if filepath.IsAbs(path) {
			relative, err := filepath.Rel(baseDir, path)
			if err != nil {
				return nil, err
			}
			path = relative
		} else {
			relative, err := filepath.Rel(baseDir, path)
			if err == nil && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && relative != ".." {
				path = relative
			}
		}
		output = append(output, Entry{
			Path: filepath.ToSlash(path),
			Sum:  entry.Sum,
		})
	}
	return output, nil
}

// Verify 校验 checksum 清单，路径按清单所在目录解析。
func Verify(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	baseDir := filepath.Dir(path)
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		expected, target, ok := parseLine(line)
		if !ok {
			return fmt.Errorf("第 %d 行 checksum 格式无效", lineNumber)
		}
		targetPath := target
		if !filepath.IsAbs(targetPath) {
			targetPath = filepath.Join(baseDir, filepath.FromSlash(target))
		}
		actual, err := fileSHA256(targetPath)
		if err != nil {
			return err
		}
		if !strings.EqualFold(expected, actual) {
			return fmt.Errorf("文件校验失败: %s", target)
		}
	}
	return scanner.Err()
}

func expandFiles(paths []string) ([]string, error) {
	files := []string{}
	for _, item := range paths {
		path := strings.TrimSpace(item)
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			files = append(files, path)
			continue
		}
		if err := filepath.WalkDir(path, func(current string, entry os.DirEntry, err error) error {
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
			if info.Mode().IsRegular() {
				files = append(files, current)
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, fmt.Errorf("没有找到可校验的文件")
	}
	return files, nil
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

func parseLine(line string) (string, string, bool) {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}
