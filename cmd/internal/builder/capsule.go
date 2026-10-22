package buildercmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-xin/sparq-protocol/artifact/sourcecapsule"
)

const maxKeyFileBytes = 64 << 10

type capsuleCreateOptions struct {
	sourcePath              string
	outputPath              string
	appCode                 string
	version                 string
	expectedSourceSHA256    string
	builderEnvironment      string
	encryptionKeyID         string
	encryptionPublicKeyPath string
	signingKeyID            string
	signingPrivateKeyPath   string
}

func newCapsuleCommand() *cobra.Command {
	command := &cobra.Command{Use: "capsule", Short: "生成 Local 插件加密构建胶囊"}
	command.AddCommand(newCapsuleCreateCommand())
	command.AddCommand(newCapsuleKeygenCommand())
	return command
}

func newCapsuleCreateCommand() *cobra.Command {
	var options capsuleCreateOptions
	command := &cobra.Command{
		Use:   "create",
		Short: "加密并签名人工审核通过的插件源码包",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runCapsuleCreate(command, options)
		},
	}
	command.Flags().StringVar(&options.sourcePath, "source", "", "人工审核通过的源码包路径")
	command.Flags().StringVar(&options.outputPath, "output", "", "胶囊输出路径，默认使用应用代码和版本生成文件名")
	command.Flags().StringVar(&options.appCode, "app-code", "", "审核版本所属应用代码")
	command.Flags().StringVar(&options.version, "version", "", "审核版本号")
	command.Flags().StringVar(&options.expectedSourceSHA256, "expected-source-sha256", "", "人工审核快照的源码 SHA-256")
	command.Flags().StringVar(&options.builderEnvironment, "builder-environment", "", "签名锁定的构建环境摘要")
	command.Flags().StringVar(&options.encryptionKeyID, "encryption-key-id", "", "胶囊数据密钥包装公钥 ID")
	command.Flags().StringVar(&options.encryptionPublicKeyPath, "encryption-public-key", "", "RSA 胶囊加密公钥路径")
	command.Flags().StringVar(&options.signingKeyID, "signing-key-id", "", "Marketplace Artifact 签名密钥 ID")
	command.Flags().StringVar(&options.signingPrivateKeyPath, "signing-private-key", "", "Ed25519 Artifact 签名私钥路径")
	return command
}

func runCapsuleCreate(command *cobra.Command, options capsuleCreateOptions) error {
	options.sourcePath = strings.TrimSpace(options.sourcePath)
	options.appCode = strings.TrimSpace(options.appCode)
	options.version = strings.TrimSpace(options.version)
	options.outputPath = strings.TrimSpace(options.outputPath)
	if options.sourcePath == "" || options.appCode == "" || options.version == "" || strings.TrimSpace(options.expectedSourceSHA256) == "" ||
		strings.TrimSpace(options.builderEnvironment) == "" || strings.TrimSpace(options.encryptionKeyID) == "" || strings.TrimSpace(options.encryptionPublicKeyPath) == "" ||
		strings.TrimSpace(options.signingKeyID) == "" || strings.TrimSpace(options.signingPrivateKeyPath) == "" {
		return fmt.Errorf("源码包、应用、版本、审核摘要、构建环境和加密签名密钥参数不能为空")
	}
	source, err := readRegularFile(options.sourcePath, sourcecapsule.SourceMaxBytes)
	if err != nil {
		return fmt.Errorf("读取审核源码包失败: %w", err)
	}
	encryptionPublicKey, err := readRegularFile(options.encryptionPublicKeyPath, maxKeyFileBytes)
	if err != nil {
		return fmt.Errorf("读取胶囊加密公钥失败: %w", err)
	}
	signingPrivateKey, err := readRegularFile(options.signingPrivateKeyPath, maxKeyFileBytes)
	if err != nil {
		return fmt.Errorf("读取 Artifact 签名私钥失败: %w", err)
	}
	result, err := sourcecapsule.Create(sourcecapsule.CreateOptions{
		AppCode: options.appCode, Version: options.version, Source: source,
		ExpectedSourceSHA256: strings.TrimSpace(options.expectedSourceSHA256),
		BuilderEnvironment:   strings.TrimSpace(options.builderEnvironment),
		EncryptionKeyID:      strings.TrimSpace(options.encryptionKeyID), EncryptionPublicKeyPEM: encryptionPublicKey,
		SigningKeyID: strings.TrimSpace(options.signingKeyID), SigningPrivateKeyPEM: signingPrivateKey,
	})
	if err != nil {
		return err
	}
	if options.outputPath == "" {
		options.outputPath = options.appCode + "-" + options.version + "-local.sparq"
	}
	outputPath, err := filepath.Abs(options.outputPath)
	if err != nil {
		return fmt.Errorf("解析胶囊输出路径失败: %w", err)
	}
	if err := writeExclusiveFile(outputPath, result.Body, 0o600); err != nil {
		return err
	}
	_, err = fmt.Fprintf(command.OutOrStdout(), "Local 加密构建胶囊生成完成\n应用: %s\n版本: %s\n文件: %s\n源码 SHA-256: %s\n胶囊 SHA-256: %s\n签名密钥: %s\n加密密钥: %s\n", result.Manifest.AppCode, result.Manifest.Version, outputPath, result.Manifest.Source.SHA256, result.SHA256, result.Manifest.Signature.KeyID, result.Manifest.Payload.KeyID)
	return err
}

func newCapsuleKeygenCommand() *cobra.Command {
	var outputDir string
	command := &cobra.Command{
		Use:   "keygen",
		Short: "生成胶囊加密密钥和 Artifact 签名密钥",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			outputDir = strings.TrimSpace(outputDir)
			if outputDir == "" {
				return fmt.Errorf("密钥输出目录不能为空")
			}
			root, err := filepath.Abs(outputDir)
			if err != nil {
				return fmt.Errorf("解析密钥输出目录失败: %w", err)
			}
			if err := writeCapsuleKeySet(root); err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "胶囊密钥已生成: %s\n", root)
			return err
		},
	}
	command.Flags().StringVar(&outputDir, "output-dir", "", "密钥输出目录，不覆盖已有文件")
	return command
}

func writeCapsuleKeySet(root string) error {
	paths := []string{
		filepath.Join(root, "capsule-encryption-private.pem"),
		filepath.Join(root, "capsule-encryption-public.pem"),
		filepath.Join(root, "artifact-signing-private.pem"),
		filepath.Join(root, "artifact-signing-public.pem"),
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("密钥文件已存在: %s", path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("检查密钥文件 %s 失败: %w", path, err)
		}
	}
	keys, err := sourcecapsule.GenerateKeySet()
	if err != nil {
		return err
	}
	files := []struct {
		path string
		body []byte
		perm os.FileMode
	}{
		{paths[0], keys.EncryptionPrivatePEM, 0o600},
		{paths[1], keys.EncryptionPublicPEM, 0o644},
		{paths[2], keys.SigningPrivatePEM, 0o600},
		{paths[3], keys.SigningPublicPEM, 0o644},
	}
	created := make([]string, 0, len(files))
	for _, file := range files {
		if err := writeExclusiveFile(file.path, file.body, file.perm); err != nil {
			for _, path := range created {
				_ = os.Remove(path)
			}
			return err
		}
		created = append(created, file.path)
	}
	return nil
}

func readRegularFile(path string, maxBytes int64) ([]byte, error) {
	path = strings.TrimSpace(path)
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxBytes {
		return nil, fmt.Errorf("文件必须是大小不超过 %d 字节的普通文件: %s", maxBytes, path)
	}
	body, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) != info.Size() {
		return nil, fmt.Errorf("文件读取长度发生变化: %s", path)
	}
	return body, nil
}

func writeExclusiveFile(path string, body []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("创建输出目录失败: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("创建文件 %s 失败: %w", path, err)
	}
	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(body); err != nil {
		return fmt.Errorf("写入文件 %s 失败: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("同步文件 %s 失败: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("关闭文件 %s 失败: %w", path, err)
	}
	success = true
	return nil
}
