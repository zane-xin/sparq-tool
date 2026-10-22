package release

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	remoteauth "github.com/zane-xin/sparq-tool/internal/remote/auth"
)

func TestClientPublishesPlatformMatrixWithAuthProfileToken(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/cloud-api/v1/releases/sparq" {
			t.Fatalf("request path = %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer profile-token" {
			t.Fatalf("Authorization = %q", request.Header.Get("Authorization"))
		}
		if err := request.ParseMultipartForm(16 << 20); err != nil {
			t.Fatal(err)
		}
		fields := []string{"manifest", "admin", "changelog"}
		for _, platform := range testPlatforms() {
			fields = append(fields, RuntimeField(platform[0], platform[1]))
			fields = append(fields, UpdaterField(platform[0], platform[1]))
		}
		for _, field := range fields {
			file, _, err := request.FormFile(field)
			if err != nil {
				t.Fatalf("form file %s: %v", field, err)
			}
			_ = file.Close()
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"version":"0.1.0"}}`)), Request: request}, nil
	})}

	root := t.TempDir()
	input := PublishInput{
		Session:      remoteauth.Session{Endpoint: "http://sparq.example.com", AccessToken: "profile-token"},
		ManifestPath: writeFile(t, root, "manifest.json"), AdminPath: writeFile(t, root, "admin.tar.gz"),
		ChangelogPath: writeFile(t, root, "CHANGELOG.md"),
	}
	for _, platform := range testPlatforms() {
		input.Runtimes = append(input.Runtimes, RuntimeUpload{TargetOS: platform[0], TargetArch: platform[1], Path: writeFile(t, root, platform[0]+"-"+platform[1]+".tar.gz")})
		input.Updaters = append(input.Updaters, UpdaterUpload{TargetOS: platform[0], TargetArch: platform[1], Path: writeFile(t, root, "updater-"+platform[0]+"-"+platform[1]+".tar.gz")})
	}
	result, err := NewClient(httpClient).Publish(context.Background(), input)
	if err != nil || result.Version != "0.1.0" {
		t.Fatalf("Publish() = %+v, %v", result, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestClientRejectsSessionWithoutAccessToken(t *testing.T) {
	_, err := NewClient(http.DefaultClient).Publish(context.Background(), PublishInput{Session: remoteauth.Session{Endpoint: "https://sparq.example.com"}})
	if err == nil {
		t.Fatal("expected missing access token to be rejected")
	}
}

func TestValidateUploadEndpointRejectsUnsupportedScheme(t *testing.T) {
	if err := validateUploadEndpoint("ftp://example.com"); err == nil {
		t.Fatal("expected unsupported endpoint scheme to be rejected")
	}
}

func testPlatforms() [][2]string {
	return [][2]string{{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}, {"windows", "amd64"}, {"windows", "arm64"}}
}

func writeFile(t *testing.T, root, name string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
