package cloudcatalog

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	remoteauth "github.com/zane-xin/sparq-tool/internal/remote/auth"
)

func TestListCommandUsesAuthProfileEndpointAndToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/cloud-api/v1/applications" || request.URL.RawQuery != "" {
			t.Fatalf("request = %s", request.URL.String())
		}
		if request.Header.Get("Authorization") != "Bearer catalog-token" {
			t.Fatalf("Authorization = %q", request.Header.Get("Authorization"))
		}
		fmt.Fprint(response, `{"code":0,"data":[]}`)
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "auth.json")
	if _, err := remoteauth.SaveSession(configPath, "release", remoteauth.Session{Endpoint: server.URL, AccessToken: "catalog-token"}); err != nil {
		t.Fatal(err)
	}
	command := NewListCommand(Resource{Kind: KindApplication, DisplayName: "插件"})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--config", configPath, "--profile", "release"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "未找到线上插件") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestCatalogCommandsDoNotExposeIndependentURL(t *testing.T) {
	command := NewListCommand(Resource{Kind: KindApplication, DisplayName: "插件"})
	if command.Flags().Lookup("url") != nil {
		t.Fatal("catalog command should not expose --url")
	}
	for _, name := range []string{"config", "profile"} {
		if command.Flags().Lookup(name) == nil {
			t.Fatalf("catalog command is missing --%s", name)
		}
	}
}
