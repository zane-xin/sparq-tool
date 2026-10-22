package buildercmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zane-xin/sparq-protocol/artifact/sourcecapsule"
)

func TestCapsuleCreateCommandWritesVerifiedCapsule(t *testing.T) {
	root := t.TempDir()
	keys, err := sourcecapsule.GenerateKeySet()
	if err != nil {
		t.Fatal(err)
	}
	source := []byte("approved plugin source archive")
	digest := sha256.Sum256(source)
	sourcePath := filepath.Join(root, "source.tar.gz")
	encryptionPublicPath := filepath.Join(root, "encryption-public.pem")
	signingPrivatePath := filepath.Join(root, "signing-private.pem")
	outputPath := filepath.Join(root, "cms-1.2.3-local.sparq")
	for path, body := range map[string][]byte{
		sourcePath: source, encryptionPublicPath: keys.EncryptionPublicPEM, signingPrivatePath: keys.SigningPrivatePEM,
	} {
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var stdout bytes.Buffer
	command := newCapsuleCreateCommand()
	command.SetOut(&stdout)
	command.SetArgs([]string{
		"--source", sourcePath, "--output", outputPath, "--app-code", "cms", "--version", "1.2.3",
		"--expected-source-sha256", hex.EncodeToString(digest[:]), "--builder-environment", "sparq-builder:v1@sha256:test",
		"--encryption-key-id", "encryption-v1", "--encryption-public-key", encryptionPublicPath,
		"--signing-key-id", "signing-v1", "--signing-private-key", signingPrivatePath,
	})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	result, err := sourcecapsule.Inspect(body)
	if err != nil {
		t.Fatal(err)
	}
	if result.Manifest.Source.SHA256 != hex.EncodeToString(digest[:]) || !strings.Contains(stdout.String(), result.SHA256) {
		t.Fatalf("result = %+v, output = %s", result, stdout.String())
	}
	if err := writeExclusiveFile(outputPath, []byte("changed"), 0o600); err == nil {
		t.Fatal("capsule create should reject overwriting an existing file")
	}
}

func TestWriteCapsuleKeySetDoesNotPartiallyWriteWhenTargetExists(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "artifact-signing-private.pem")
	if err := os.WriteFile(existing, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeCapsuleKeySet(root); err == nil {
		t.Fatal("writeCapsuleKeySet should reject an existing target")
	}
	for _, name := range []string{"capsule-encryption-private.pem", "capsule-encryption-public.pem", "artifact-signing-public.pem"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("unexpected partial key file %s: %v", name, err)
		}
	}
	body, err := os.ReadFile(existing)
	if err != nil || string(body) != "existing" {
		t.Fatalf("existing key changed: %q, %v", body, err)
	}
}
