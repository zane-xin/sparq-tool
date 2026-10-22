package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadCopiesLocalFile(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.txt")
	output := filepath.Join(root, "out", "artifact.txt")
	if err := os.WriteFile(source, []byte("artifact"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	result, err := Download(DownloadOptions{Source: source, Output: output})
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	if result.Output != output {
		t.Fatalf("output = %s, want %s", result.Output, output)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(data) != "artifact" {
		t.Fatalf("content = %s", string(data))
	}
}

func TestDownloadVerifiedWritesOnlyAfterIntegrityChecks(t *testing.T) {
	body := []byte("artifact")
	digest := sha256.Sum256(body)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write(body)
	}))
	defer server.Close()
	output := filepath.Join(t.TempDir(), "artifact.bin")
	result, err := DownloadVerified(VerifiedDownloadOptions{
		Source: server.URL + "/artifact.bin", Output: output,
		ExpectedSHA256: hex.EncodeToString(digest[:]), ExpectedSize: uint64(len(body)),
		Context: context.Background(), Client: server.Client(),
	})
	if err != nil {
		t.Fatalf("DownloadVerified() error = %v", err)
	}
	if result.Output != output {
		t.Fatalf("output = %s", result.Output)
	}
	if data, err := os.ReadFile(output); err != nil || string(data) != string(body) {
		t.Fatalf("downloaded body = %q, error = %v", data, err)
	}
}

func TestDownloadVerifiedRejectsMismatchWithoutOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("changed"))
	}))
	defer server.Close()
	output := filepath.Join(t.TempDir(), "artifact.bin")
	_, err := DownloadVerified(VerifiedDownloadOptions{
		Source: server.URL, Output: output,
		ExpectedSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ExpectedSize: 7,
		Client: server.Client(),
	})
	if err == nil {
		t.Fatal("DownloadVerified should reject a mismatched response")
	}
	if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
		t.Fatalf("output should not exist, stat error = %v", statErr)
	}
}

func TestDownloadVerifiedStopsAtExpectedSize(t *testing.T) {
	body := []byte("artifact-content-is-larger-than-declared")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write(body)
	}))
	defer server.Close()
	output := filepath.Join(t.TempDir(), "artifact.bin")
	_, err := DownloadVerified(VerifiedDownloadOptions{
		Source: server.URL, Output: output,
		ExpectedSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ExpectedSize: 8,
		Client: server.Client(),
	})
	if err == nil {
		t.Fatal("DownloadVerified should reject content larger than the declared size")
	}
	if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
		t.Fatalf("output should not exist, stat error = %v", statErr)
	}
}

func TestDownloadHTTPUsesBearerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("missing bearer token")
		}
		_, _ = writer.Write([]byte("artifact"))
	}))
	defer server.Close()
	output := filepath.Join(t.TempDir(), "artifact.bin")
	if _, err := Download(DownloadOptions{Source: server.URL + "/artifact.bin", Output: output, Token: "token"}); err != nil {
		t.Fatalf("Download failed: %v", err)
	}
}
