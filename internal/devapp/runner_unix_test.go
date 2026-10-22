//go:build unix

package devapp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zane-xin/sparq-tool/internal/toolchain"
	"github.com/zane-xin/sparq-tool/internal/watch"
)

func TestAppDevHTTPProcess(t *testing.T) {
	address := os.Getenv("SPARQ_DEVAPP_TEST_ADDRESS")
	if address == "" {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/exit", func(http.ResponseWriter, *http.Request) { os.Exit(1) })
	mux.HandleFunc("/pending", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
	if err := http.ListenAndServe(address, mux); err != nil {
		t.Fatal(err)
	}
}

func TestRunCleansApplicationProcesses(t *testing.T) {
	for _, scenario := range []string{"backend-exit", "frontend-exit", "cancel", "frontend-timeout", "artifact"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			backendAddress := unusedTestAddress(t)
			frontendAddress := unusedTestAddress(t)
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			backend := watch.NewPlan(watch.Config{
				CWD:    root,
				Run:    watch.CommandConfig{Command: fmt.Sprintf("exec %q -test.run=^TestAppDevHTTPProcess$", executable), Env: map[string]string{"SPARQ_DEVAPP_TEST_ADDRESS": backendAddress}},
				Watch:  watch.WatchConfig{Mode: "poll", PollInterval: "20ms"},
				Stop:   watch.StopConfig{GraceTimeout: "200ms", KillTimeout: "1s"},
				Health: watch.HealthConfig{URL: "http://" + backendAddress, Path: "/health", Timeout: "5s"},
			})
			config := Config{
				Backend: BackendConfig{WatchFile: filepath.Join(root, "sparq.watch.yaml")},
				Frontend: FrontendConfig{
					Mode: "source", CWD: root, Command: "-test.run=^TestAppDevHTTPProcess$",
					URL: "http://" + frontendAddress, Health: "/health", Timeout: "5s",
					Env:  map[string]string{"SPARQ_DEVAPP_TEST_ADDRESS": frontendAddress},
					Stop: StopConfig{GraceTimeout: "200ms", KillTimeout: "1s"},
				},
				Toolchain: toolchain.Toolchain{Frontend: toolchain.Spec{Command: executable}},
			}
			if scenario == "artifact" {
				config.Frontend.Mode = "artifact"
			}
			if scenario == "frontend-timeout" {
				config.Frontend.Health = "/pending"
				config.Frontend.Timeout = "100ms"
			}
			logPath := filepath.Join(root, "session.log")
			log, err := os.Create(logPath)
			if err != nil {
				t.Fatal(err)
			}
			defer log.Close()
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			var runErr error
			go func() {
				runErr = Run(ctx, NewPlan(config, backend), Streams{Stdout: log, Stderr: log})
				close(done)
			}()
			t.Cleanup(func() {
				cancel()
				select {
				case <-done:
				case <-time.After(10 * time.Second):
					t.Error("application session cleanup timed out")
				}
			})
			readyMessage := "应用联调已就绪"
			if scenario == "frontend-timeout" {
				readyMessage = "开发进程仍在运行"
			} else if scenario == "artifact" {
				readyMessage = "文件监听已启用"
			}
			deadline := time.After(10 * time.Second)
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
		waitReady:
			for {
				select {
				case <-done:
					contents, _ := os.ReadFile(logPath)
					t.Fatalf("session exited before ready: %v\n%s", runErr, contents)
				case <-deadline:
					contents, _ := os.ReadFile(logPath)
					t.Fatalf("session did not become ready: %s", contents)
				case <-ticker.C:
					contents, _ := os.ReadFile(logPath)
					if strings.Contains(string(contents), readyMessage) {
						break waitReady
					}
				}
			}
			if scenario == "backend-exit" || scenario == "frontend-exit" {
				address := backendAddress
				if scenario == "frontend-exit" {
					address = frontendAddress
				}
				client := &http.Client{Timeout: time.Second}
				response, _ := client.Post("http://"+address+"/exit", "text/plain", nil)
				if response != nil {
					_ = response.Body.Close()
				}
			} else {
				cancel()
			}
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("session did not stop its peer process")
			}
			if strings.HasSuffix(scenario, "-exit") {
				if runErr == nil || !strings.Contains(runErr.Error(), "已退出") {
					t.Fatalf("process exit error = %v", runErr)
				}
			} else if runErr != nil {
				t.Fatalf("cancellation error = %v", runErr)
			}
			for _, address := range []string{backendAddress, frontendAddress} {
				listener, err := net.Listen("tcp", address)
				if err != nil {
					t.Fatalf("session retained port %s: %v", address, err)
				}
				_ = listener.Close()
			}
		})
	}
}

func unusedTestAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}
