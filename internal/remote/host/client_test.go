package host

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestClientThemeLifecycleUsesThemeEndpoints(t *testing.T) {
	var paths []string
	client := NewClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		paths = append(paths, request.Method+" "+request.URL.Path)
		switch request.URL.Path {
		case "/admin-api/site/themes/install-artifact", "/admin-api/site/themes/studio/rollback":
			return jsonResponse(`{"code":0,"data":{"id":"operation","code":"studio","status":"pending","stage":"pending"}}`), nil
		case "/admin-api/site/themes/studio/1.0.0/activate":
			return jsonResponse(`{"code":0,"data":{"code":"studio","version":"1.0.0","active":true}}`), nil
		case "/admin-api/site/themes/studio/1.0.0":
			return jsonResponse(`{"code":0,"data":{"uninstalled":true}}`), nil
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
			return nil, nil
		}
	})})
	credentials := Credentials{Endpoint: "https://host.example.com", Session: "session", CSRF: "csrf"}
	if _, err := client.InstallTheme(context.Background(), credentials, "spqig_test", false); err != nil {
		t.Fatal(err)
	}
	if theme, err := client.ActivateTheme(context.Background(), credentials, "studio", "1.0.0"); err != nil || !theme.Active {
		t.Fatalf("ActivateTheme() = %#v, %v", theme, err)
	}
	if err := client.RemoveTheme(context.Background(), credentials, "studio", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.RollbackTheme(context.Background(), credentials, "studio"); err != nil {
		t.Fatal(err)
	}
	want := "POST /admin-api/site/themes/install-artifact,POST /admin-api/site/themes/studio/1.0.0/activate,DELETE /admin-api/site/themes/studio/1.0.0,POST /admin-api/site/themes/studio/rollback"
	if strings.Join(paths, ",") != want {
		t.Fatalf("paths = %v", paths)
	}
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)),
	}
}
