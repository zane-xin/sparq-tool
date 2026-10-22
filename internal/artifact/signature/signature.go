package signature

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const signatureBlockType = "SPARQ ED25519 SIGNATURE"

// KeyPair 描述 ed25519 PEM 密钥对。
type KeyPair struct {
	PrivatePEM []byte
	PublicPEM  []byte
}

// GenerateKeyPair 生成用于插件产物签名的 ed25519 密钥对。
func GenerateKeyPair() (KeyPair, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, err
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return KeyPair{}, err
	}
	publicDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return KeyPair{}, err
	}
	return KeyPair{
		PrivatePEM: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}),
		PublicPEM:  pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}),
	}, nil
}

// WriteKeyPair 写入 PEM 密钥对。
func WriteKeyPair(privatePath string, publicPath string, pair KeyPair) error {
	if err := writeFile(privatePath, pair.PrivatePEM, 0o600); err != nil {
		return err
	}
	return writeFile(publicPath, pair.PublicPEM, 0o644)
}

// Sign 对文件内容生成 ed25519 签名。
func Sign(filePath string, privateKeyPath string) ([]byte, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	signed, err := SignBytes(data, privateKeyPath)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: signatureBlockType, Bytes: signed}), nil
}

// SignBytes 对内存中的内容生成原始 Ed25519 签名。
func SignBytes(data []byte, privateKeyPath string) ([]byte, error) {
	privateKey, err := readPrivateKey(privateKeyPath)
	if err != nil {
		return nil, err
	}
	return ed25519.Sign(privateKey, data), nil
}

// WriteSignature 写入签名文件。
func WriteSignature(path string, signature []byte) error {
	return writeFile(path, signature, 0o644)
}

// Verify 校验文件内容和 ed25519 签名是否匹配。
func Verify(filePath string, publicKeyPath string, signaturePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	publicKey, err := readPublicKey(publicKeyPath)
	if err != nil {
		return err
	}
	signature, err := readSignature(signaturePath)
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, data, signature) {
		return fmt.Errorf("签名校验失败: %s", filePath)
	}
	return nil
}

func readPrivateKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("私钥 PEM 格式无效: %s", path)
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	privateKey, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("私钥不是 ed25519 类型: %s", path)
	}
	return privateKey, nil
}

func readPublicKey(path string) (ed25519.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("公钥 PEM 格式无效: %s", path)
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	publicKey, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("公钥不是 ed25519 类型: %s", path)
	}
	return publicKey, nil
}

func readSignature(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != signatureBlockType {
		return nil, fmt.Errorf("签名 PEM 格式无效: %s", path)
	}
	return block.Bytes, nil
}

func writeFile(path string, data []byte, perm os.FileMode) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("文件路径不能为空")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, perm)
}
