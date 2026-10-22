package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// DownloadOptions 描述产物下载参数。
type DownloadOptions struct {
	Source   string
	Output   string
	Token    string
	MaxBytes uint64
	Context  context.Context
	Client   *http.Client
}

type VerifiedDownloadOptions struct {
	Source         string
	Output         string
	ExpectedSHA256 string
	ExpectedSize   uint64
	Context        context.Context
	Client         *http.Client
}

// DownloadResult 描述产物下载结果。
type DownloadResult struct {
	Output string
}

// Download 下载远程产物或复制本地产物到目标位置。
func Download(options DownloadOptions) (DownloadResult, error) {
	source := strings.TrimSpace(options.Source)
	if source == "" {
		return DownloadResult{}, fmt.Errorf("产物来源不能为空")
	}
	output := strings.TrimSpace(options.Output)
	if output == "" {
		output = defaultOutput(source)
	}
	if output == "" {
		return DownloadResult{}, fmt.Errorf("无法推断产物输出路径")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return DownloadResult{}, err
	}
	if isHTTPSource(source) {
		ctx := options.Context
		if ctx == nil {
			ctx = context.Background()
		}
		client := options.Client
		if client == nil {
			client = http.DefaultClient
		}
		if err := downloadHTTP(ctx, client, source, output, strings.TrimSpace(options.Token), options.MaxBytes); err != nil {
			return DownloadResult{}, err
		}
		return DownloadResult{Output: output}, nil
	}
	if err := copyFile(source, output, options.MaxBytes); err != nil {
		return DownloadResult{}, err
	}
	return DownloadResult{Output: output}, nil
}

func DownloadVerified(options VerifiedDownloadOptions) (DownloadResult, error) {
	if !validSHA256(options.ExpectedSHA256) || options.ExpectedSize == 0 {
		return DownloadResult{}, fmt.Errorf("产物 SHA-256 和文件大小不能为空")
	}
	output := strings.TrimSpace(options.Output)
	if output == "" {
		output = defaultOutput(options.Source)
	}
	if output == "" {
		return DownloadResult{}, fmt.Errorf("无法推断产物输出路径")
	}
	if _, err := os.Stat(output); err == nil {
		return DownloadResult{}, fmt.Errorf("输出文件已存在: %s", output)
	} else if !os.IsNotExist(err) {
		return DownloadResult{}, fmt.Errorf("检查输出文件 %s 失败: %w", output, err)
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return DownloadResult{}, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(output), ".sparq-download-*.part")
	if err != nil {
		return DownloadResult{}, fmt.Errorf("创建下载临时文件失败: %w", err)
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		return DownloadResult{}, err
	}
	defer os.Remove(temporaryPath)
	if _, err := Download(DownloadOptions{
		Source: options.Source, Output: temporaryPath, Context: options.Context, Client: options.Client,
		MaxBytes: options.ExpectedSize,
	}); err != nil {
		return DownloadResult{}, err
	}
	info, err := os.Stat(temporaryPath)
	if err != nil {
		return DownloadResult{}, err
	}
	if info.Size() < 0 || uint64(info.Size()) != options.ExpectedSize {
		return DownloadResult{}, fmt.Errorf("产物大小校验失败: 实际 %d 字节，期望 %d 字节", info.Size(), options.ExpectedSize)
	}
	actualSHA256, err := fileSHA256(temporaryPath)
	if err != nil {
		return DownloadResult{}, err
	}
	if !strings.EqualFold(actualSHA256, options.ExpectedSHA256) {
		return DownloadResult{}, fmt.Errorf("产物 SHA-256 校验失败")
	}
	if err := os.Rename(temporaryPath, output); err != nil {
		return DownloadResult{}, fmt.Errorf("保存下载产物 %s 失败: %w", output, err)
	}
	return DownloadResult{Output: output}, nil
}

func downloadHTTP(ctx context.Context, client *http.Client, source string, output string, token string, maxBytes uint64) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return err
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("产物下载失败: HTTP %d", response.StatusCode)
	}
	file, err := os.Create(output)
	if err != nil {
		return err
	}
	defer file.Close()
	return copyWithLimit(file, response.Body, maxBytes)
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

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func copyFile(source string, output string, maxBytes uint64) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	file, err := os.Create(output)
	if err != nil {
		return err
	}
	defer file.Close()
	return copyWithLimit(file, input, maxBytes)
}

func copyWithLimit(output io.Writer, input io.Reader, maxBytes uint64) error {
	if maxBytes == 0 {
		_, err := io.Copy(output, input)
		return err
	}
	written, err := io.Copy(output, io.LimitReader(input, int64(maxBytes)+1))
	if err != nil {
		return err
	}
	if written > int64(maxBytes) {
		return fmt.Errorf("产物内容超过 %d 字节限制", maxBytes)
	}
	return nil
}

func defaultOutput(source string) string {
	if isHTTPSource(source) {
		parsed, err := url.Parse(source)
		if err != nil {
			return ""
		}
		name := filepath.Base(parsed.Path)
		if name == "." || name == "/" || name == "" {
			name = "artifact.bin"
		}
		return name
	}
	return filepath.Base(source)
}

func isHTTPSource(source string) bool {
	return strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://")
}
