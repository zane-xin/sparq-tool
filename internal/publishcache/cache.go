package publishcache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zane-xin/sparq-tool/internal/userpath"
)

const (
	directoryName = "publish-cache"
	manifestName  = "candidates.json"
	Lifetime      = 2 * time.Hour
)

// Candidate 描述可恢复上传的一个候选包；缓存目录只持久化 ArchivePath 指向的包文件。
type Candidate struct {
	Kind        string `json:"kind"`
	Runtime     string `json:"runtime"`
	TargetOS    string `json:"target_os"`
	TargetArch  string `json:"target_arch"`
	Filename    string `json:"filename"`
	SHA256      string `json:"sha256"`
	SizeBytes   int64  `json:"size_bytes"`
	ArchivePath string `json:"-"`
}

// Snapshot 把候选包绑定到一次发布的源码与构建规则，避免源码变化后复用旧产物。
type Snapshot struct {
	Code              string      `json:"code"`
	TargetVersion     string      `json:"target_version"`
	SourceFingerprint string      `json:"source_fingerprint"`
	BuildFingerprint  string      `json:"build_fingerprint"`
	Candidates        []Candidate `json:"candidates"`
	UpdatedAt         string      `json:"updated_at"`
}

func Save(attemptKey string, snapshot Snapshot) (Snapshot, error) {
	if err := validateSnapshot(snapshot); err != nil {
		return Snapshot{}, err
	}
	root, err := cacheDirectory()
	if err != nil {
		return Snapshot{}, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return Snapshot{}, fmt.Errorf("创建候选包缓存目录失败: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return Snapshot{}, fmt.Errorf("设置候选包缓存目录权限失败: %w", err)
	}
	temporary, err := os.MkdirTemp(root, ".publish-candidates-*")
	if err != nil {
		return Snapshot{}, fmt.Errorf("创建候选包缓存临时目录失败: %w", err)
	}
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.RemoveAll(temporary)
		}
	}()
	if err := os.Chmod(temporary, 0o700); err != nil {
		return Snapshot{}, fmt.Errorf("设置候选包缓存临时目录权限失败: %w", err)
	}
	stored := snapshot
	stored.Candidates = make([]Candidate, 0, len(snapshot.Candidates))
	for index, candidate := range snapshot.Candidates {
		filename := candidateCacheFilename(index, candidate)
		target := filepath.Join(temporary, filename)
		digest, size, err := copyCandidate(candidate.ArchivePath, target)
		if err != nil {
			return Snapshot{}, err
		}
		if candidate.SHA256 != "" && !strings.EqualFold(candidate.SHA256, digest) {
			return Snapshot{}, fmt.Errorf("候选包 %s 内容摘要与缓存清单不一致", filepath.Base(candidate.ArchivePath))
		}
		candidate.Filename = filename
		candidate.SHA256 = digest
		candidate.SizeBytes = size
		candidate.ArchivePath = target
		stored.Candidates = append(stored.Candidates, candidate)
	}
	stored.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := writeManifest(filepath.Join(temporary, manifestName), stored); err != nil {
		return Snapshot{}, err
	}
	target, err := attemptDirectory(attemptKey)
	if err != nil {
		return Snapshot{}, err
	}
	if err := os.RemoveAll(target); err != nil {
		return Snapshot{}, fmt.Errorf("替换旧候选包缓存失败: %w", err)
	}
	if err := os.Rename(temporary, target); err != nil {
		return Snapshot{}, fmt.Errorf("提交候选包缓存失败: %w", err)
	}
	removeTemporary = false
	for index := range stored.Candidates {
		stored.Candidates[index].ArchivePath = filepath.Join(target, stored.Candidates[index].Filename)
	}
	return stored, nil
}

func Load(attemptKey string, now time.Time) (Snapshot, bool, error) {
	directory, err := attemptDirectory(attemptKey)
	if err != nil {
		return Snapshot{}, false, err
	}
	body, err := os.ReadFile(filepath.Join(directory, manifestName))
	if os.IsNotExist(err) {
		return Snapshot{}, false, nil
	}
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("读取候选包缓存清单失败: %w", err)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(body, &snapshot); err != nil {
		return Snapshot{}, false, fmt.Errorf("解析候选包缓存清单失败: %w", err)
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, snapshot.UpdatedAt)
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("候选包缓存更新时间无效: %w", err)
	}
	if !now.Before(updatedAt.Add(Lifetime)) {
		if err := os.RemoveAll(directory); err != nil {
			return Snapshot{}, false, fmt.Errorf("清理过期候选包缓存失败: %w", err)
		}
		return Snapshot{}, false, nil
	}
	if err := validateSnapshot(snapshot); err != nil {
		return Snapshot{}, false, err
	}
	for index := range snapshot.Candidates {
		candidate := &snapshot.Candidates[index]
		if filepath.Base(candidate.Filename) != candidate.Filename || candidate.Filename == manifestName {
			return Snapshot{}, false, fmt.Errorf("候选包缓存文件名无效: %q", candidate.Filename)
		}
		candidate.ArchivePath = filepath.Join(directory, candidate.Filename)
		digest, size, err := fileDigest(candidate.ArchivePath)
		if err != nil {
			return Snapshot{}, false, err
		}
		if size != candidate.SizeBytes || !strings.EqualFold(digest, candidate.SHA256) {
			return Snapshot{}, false, fmt.Errorf("候选包缓存 %s 已损坏", candidate.Filename)
		}
	}
	return snapshot, true, nil
}

func Touch(attemptKey string) error {
	directory, err := attemptDirectory(attemptKey)
	if err != nil {
		return err
	}
	body, err := os.ReadFile(filepath.Join(directory, manifestName))
	if err != nil {
		return fmt.Errorf("读取候选包缓存清单失败: %w", err)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(body, &snapshot); err != nil {
		return fmt.Errorf("解析候选包缓存清单失败: %w", err)
	}
	if err := validateSnapshot(snapshot); err != nil {
		return err
	}
	snapshot.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return writeManifest(filepath.Join(directory, manifestName), snapshot)
}

func Delete(attemptKey string) error {
	directory, err := attemptDirectory(attemptKey)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(directory); err != nil {
		return fmt.Errorf("清理候选包缓存失败: %w", err)
	}
	return nil
}

func CleanExpired(now time.Time) error {
	root, err := cacheDirectory()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取候选包缓存目录失败: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(root, entry.Name())
		updatedAt := time.Time{}
		body, readErr := os.ReadFile(filepath.Join(directory, manifestName))
		if readErr == nil {
			var snapshot Snapshot
			if json.Unmarshal(body, &snapshot) == nil {
				updatedAt, _ = time.Parse(time.RFC3339Nano, snapshot.UpdatedAt)
			}
		}
		if updatedAt.IsZero() {
			info, statErr := entry.Info()
			if statErr != nil {
				continue
			}
			updatedAt = info.ModTime()
		}
		if now.Before(updatedAt.Add(Lifetime)) {
			continue
		}
		if err := os.RemoveAll(directory); err != nil {
			return fmt.Errorf("清理过期候选包缓存 %s 失败: %w", entry.Name(), err)
		}
	}
	return nil
}

func validateSnapshot(snapshot Snapshot) error {
	if snapshot.Code == "" || snapshot.TargetVersion == "" || !validSHA256(snapshot.SourceFingerprint) ||
		snapshot.BuildFingerprint == "" || len(snapshot.Candidates) == 0 {
		return fmt.Errorf("候选包缓存清单不完整")
	}
	for _, candidate := range snapshot.Candidates {
		if candidate.Kind == "" || (candidate.ArchivePath == "" && candidate.Filename == "") {
			return fmt.Errorf("候选包缓存条目不完整")
		}
		if candidate.SHA256 != "" && !validSHA256(candidate.SHA256) {
			return fmt.Errorf("候选包缓存摘要无效")
		}
	}
	return nil
}

func cacheDirectory() (string, error) {
	toolDir, err := userpath.ToolDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(toolDir, directoryName), nil
}

func attemptDirectory(attemptKey string) (string, error) {
	attemptKey = strings.TrimSpace(attemptKey)
	if attemptKey == "" {
		return "", fmt.Errorf("发布幂等键不能为空")
	}
	root, err := cacheDirectory()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(attemptKey))
	return filepath.Join(root, hex.EncodeToString(digest[:])), nil
}

func candidateCacheFilename(index int, candidate Candidate) string {
	parts := []string{fmt.Sprintf("%02d", index+1), candidate.Kind, candidate.Runtime, candidate.TargetOS, candidate.TargetArch}
	for index := range parts {
		parts[index] = cacheNamePart(parts[index])
	}
	return strings.Trim(strings.Join(parts, "-"), "-") + ".tar.gz"
}

func cacheNamePart(value string) string {
	value = strings.ToLower(value)
	var result strings.Builder
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' || character == '_' {
			result.WriteRune(character)
		}
	}
	return result.String()
}

func copyCandidate(source, target string) (string, int64, error) {
	input, err := os.Open(source)
	if err != nil {
		return "", 0, fmt.Errorf("打开候选包 %s 失败: %w", source, err)
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		return "", 0, fmt.Errorf("候选包 %s 不是有效普通文件", source)
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", 0, fmt.Errorf("创建候选包缓存文件失败: %w", err)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(output, hash), input)
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil {
		return "", 0, fmt.Errorf("缓存候选包 %s 失败: %w", source, copyErr)
	}
	if syncErr != nil {
		return "", 0, fmt.Errorf("同步候选包缓存 %s 失败: %w", target, syncErr)
	}
	if closeErr != nil {
		return "", 0, fmt.Errorf("关闭候选包缓存 %s 失败: %w", target, closeErr)
	}
	if written != info.Size() {
		return "", 0, fmt.Errorf("缓存候选包 %s 时内容发生变化", source)
	}
	return hex.EncodeToString(hash.Sum(nil)), written, nil
}

func fileDigest(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("打开候选包缓存 %s 失败: %w", path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		return "", 0, fmt.Errorf("候选包缓存 %s 不是有效普通文件", path)
	}
	hash := sha256.New()
	written, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, fmt.Errorf("计算候选包缓存 %s 摘要失败: %w", path, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), written, nil
}

func writeManifest(path string, snapshot Snapshot) error {
	body, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("编码候选包缓存清单失败: %w", err)
	}
	body = append(body, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".candidates-manifest-*")
	if err != nil {
		return fmt.Errorf("创建候选包缓存清单临时文件失败: %w", err)
	}
	temporaryPath := temporary.Name()
	remove := true
	defer func() {
		_ = temporary.Close()
		if remove {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(body); err != nil {
		return fmt.Errorf("写入候选包缓存清单失败: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("同步候选包缓存清单失败: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("关闭候选包缓存清单失败: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("提交候选包缓存清单失败: %w", err)
	}
	remove = false
	return nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
