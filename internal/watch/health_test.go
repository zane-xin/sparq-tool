package watch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHealthURLComposesPathAndRemovesQuery(t *testing.T) {
	target, err := healthURL("https://example.com/api/?token=secret#fragment", "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	if target != "https://example.com/api/healthz" {
		t.Fatalf("healthURL() = %q", target)
	}
}

func TestHTTPCheckerRetriesUntilReady(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/base/healthz" {
			t.Errorf("request path = %q", request.URL.Path)
			http.Error(writer, "wrong path", http.StatusBadRequest)
			return
		}
		if requests.Add(1) < 2 {
			http.Error(writer, "starting", http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	process := &Process{spec: Command{Name: "test"}, done: make(chan struct{})}
	checker := HTTPChecker{URL: server.URL + "/base", Path: "/healthz", Timeout: time.Second}
	if err := checker.Wait(context.Background(), process); err != nil {
		t.Fatal(err)
	}
	if requests.Load() < 2 {
		t.Fatalf("request count = %d", requests.Load())
	}
}

func TestHTTPCheckerTimeoutIncludesBlockedRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()

	process := &Process{spec: Command{Name: "test"}, done: make(chan struct{})}
	started := time.Now()
	err := (HTTPChecker{URL: server.URL, Timeout: 50 * time.Millisecond}).Wait(t.Context(), process)
	if err == nil {
		t.Fatal("HTTPChecker.Wait() error = nil")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("HTTPChecker.Wait() elapsed = %s", elapsed)
	}
}

func TestHTTPCheckerStopsWhenProcessExits(t *testing.T) {
	process := &Process{
		spec:    Command{Name: "test"},
		done:    make(chan struct{}),
		exitErr: errors.New("crashed"),
	}
	close(process.done)
	err := (HTTPChecker{URL: "http://127.0.0.1:1", Timeout: time.Minute}).Wait(context.Background(), process)
	if err == nil || !strings.Contains(err.Error(), "crashed") {
		t.Fatalf("HTTPChecker.Wait() error = %v", err)
	}
}
