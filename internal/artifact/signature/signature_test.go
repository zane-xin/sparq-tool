package signature

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSignAndVerify(t *testing.T) {
	root := t.TempDir()
	pair, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}
	privatePath := filepath.Join(root, "private.pem")
	publicPath := filepath.Join(root, "public.pem")
	if err := WriteKeyPair(privatePath, publicPath, pair); err != nil {
		t.Fatalf("WriteKeyPair failed: %v", err)
	}
	artifactPath := filepath.Join(root, "plugin.tar.gz")
	if err := os.WriteFile(artifactPath, []byte("artifact"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	signaturePEM, err := Sign(artifactPath, privatePath)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}
	signaturePath := filepath.Join(root, "signature.pem")
	if err := WriteSignature(signaturePath, signaturePEM); err != nil {
		t.Fatalf("WriteSignature failed: %v", err)
	}
	if err := Verify(artifactPath, publicPath, signaturePath); err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
}

func TestVerifyDetectsInvalidSignature(t *testing.T) {
	root := t.TempDir()
	pair, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}
	privatePath := filepath.Join(root, "private.pem")
	publicPath := filepath.Join(root, "public.pem")
	if err := WriteKeyPair(privatePath, publicPath, pair); err != nil {
		t.Fatalf("WriteKeyPair failed: %v", err)
	}
	artifactPath := filepath.Join(root, "plugin.tar.gz")
	if err := os.WriteFile(artifactPath, []byte("artifact"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	signaturePEM, err := Sign(artifactPath, privatePath)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}
	signaturePath := filepath.Join(root, "signature.pem")
	if err := WriteSignature(signaturePath, signaturePEM); err != nil {
		t.Fatalf("WriteSignature failed: %v", err)
	}
	if err := os.WriteFile(artifactPath, []byte("changed"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := Verify(artifactPath, publicPath, signaturePath); err == nil {
		t.Fatalf("Verify should fail after artifact change")
	}
}
