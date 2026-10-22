package plugincmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	remoteauth "github.com/zane-xin/sparq-tool/internal/remote/auth"
)

func TestDownloadCommandResolvesAndVerifiesLocalCapsule(t *testing.T) {
	body := []byte("sealed-capsule")
	digest := sha256.Sum256(body)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/cloud-api/v1/application-downloads/resolve":
			if request.Header.Get("Authorization") != "Bearer download-token" {
				t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
			}
			var input map[string]any
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if input["artifact_kind"] != "local_source_capsule" {
				t.Fatalf("input = %+v", input)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"code":0,"data":{"application":{"code":"cms","name":"CMS","version":"1.2.3"},"authorization":{"allowed":true,"license_id":9},"artifact":{"available":true,"id":12,"kind":"local_source_capsule","runtime":"local","format":"sparq.plugin.source-capsule.v1","filename":"cms-1.2.3-local.sparq","size_bytes":` + fmt.Sprint(len(body)) + `,"sha256":"` + hex.EncodeToString(digest[:]) + `","public_key_id":"key-1","protection_level":"sandboxed","download_url":"` + server.URL + `/artifact/12","expires_at":100}}}`))
		case "/artifact/12":
			_, _ = writer.Write(body)
		default:
			t.Fatalf("path = %s", request.URL.Path)
		}
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "auth.json")
	if _, err := remoteauth.SaveSession(configPath, "develop", remoteauth.Session{Endpoint: server.URL, AccessToken: "download-token"}); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "cms.sparq")
	var stdout bytes.Buffer
	command := newDownloadCommand()
	command.SetOut(&stdout)
	command.SetArgs([]string{"cms", "--config", configPath, "--profile", "develop", "--output", output})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if downloaded, err := os.ReadFile(output); err != nil || string(downloaded) != string(body) {
		t.Fatalf("downloaded = %q, error = %v", downloaded, err)
	}
	if !strings.Contains(stdout.String(), "版本: 1.2.3") || !strings.Contains(stdout.String(), "模式: local") {
		t.Fatalf("output = %s", stdout.String())
	}
}

func TestDownloadOutputUsesServerFilenameForDirectory(t *testing.T) {
	directory := t.TempDir()
	output, err := downloadOutput(directory, "cms-1.0.0.sparq")
	if err != nil {
		t.Fatal(err)
	}
	if output != filepath.Join(directory, "cms-1.0.0.sparq") {
		t.Fatalf("output = %s", output)
	}
}
