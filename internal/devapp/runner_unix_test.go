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
	mux.HandleFunc("/pending", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(os.Stdout, "测试进程等待就绪")
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	if err := http.ListenAndServe(address, mux); err != nil {
		t.Fatal(err)
	}
}

func TestRunCleansApplicationProcesses(t *testing.T) {
	for _, scenario := range []string{"backend-exit", "frontend-exit", "cancel", "frontend-timeout", "artifact", "admin-exit", "admin-only", "admin-timeout", "admin-start-failure", "frontend-start-failure", "peer-readiness-exit"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			backendAddress := unusedTestAddress(t)
			frontendAddress := unusedTestAddress(t)
			adminAddress := unusedTestAddress(t)
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
			} else {
				config.Admin = &AdminConfig{FrontendConfig: FrontendConfig{
					CWD: root, Command: "-test.run=^TestAppDevHTTPProcess$",
					URL: "http://" + adminAddress, Health: "/health", Timeout: "5s",
					Env:  map[string]string{"SPARQ_DEVAPP_TEST_ADDRESS": adminAddress},
					Stop: StopConfig{GraceTimeout: "200ms", KillTimeout: "1s"},
				}}
			}
			if scenario == "admin-only" {
				config.Frontend.Mode = "disabled"
			}
			if scenario == "admin-timeout" {
				config.Admin.Health = "/pending"
				config.Admin.Timeout = "100ms"
			}
			if scenario == "peer-readiness-exit" {
				config.Admin.Health = "/pending"
				config.Admin.Timeout = "30s"
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
			plan := NewPlan(config, backend)
			if scenario == "admin-start-failure" {
				plan.Admin.Command = filepath.Join(root, "missing-admin")
			}
			if scenario == "frontend-start-failure" {
				plan.FrontendCommand = filepath.Join(root, "missing-frontend")
			}
			go func() {
				runErr = Run(ctx, plan, Streams{Stdout: log, Stderr: log})
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
			if strings.HasSuffix(scenario, "-timeout") {
				readyMessage = "开发进程仍在运行"
			} else if scenario == "artifact" {
				readyMessage = "文件监听已启用"
			} else if scenario == "peer-readiness-exit" {
				readyMessage = "测试进程等待就绪"
			}
			deadline := time.After(10 * time.Second)
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
		waitReady:
			for {
				select {
				case <-done:
					if strings.HasSuffix(scenario, "-start-failure") && runErr != nil && strings.Contains(runErr.Error(), "启动") {
						break waitReady
					}
					contents, _ := os.ReadFile(logPath)
					t.Fatalf("session exited before ready: %v\n%s", runErr, contents)
				case <-deadline:
					contents, _ := os.ReadFile(logPath)
					t.Fatalf("session did not become ready: %s", contents)
				case <-ticker.C:
					contents, _ := os.ReadFile(logPath)
					if strings.Contains(string(contents), readyMessage) {
						if scenario == "peer-readiness-exit" {
							client := &http.Client{Timeout: time.Second}
							response, err := client.Get("http://" + frontendAddress + "/health")
							if err != nil {
								continue
							}
							_ = response.Body.Close()
						}
						break waitReady
					}
				}
			}
			if strings.HasSuffix(scenario, "-exit") {
				address := backendAddress
				if scenario == "frontend-exit" || scenario == "peer-readiness-exit" {
					address = frontendAddress
				}
				if scenario == "admin-exit" {
					address = adminAddress
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
			} else if !strings.HasSuffix(scenario, "-start-failure") && runErr != nil {
				t.Fatalf("cancellation error = %v", runErr)
			}
			for _, address := range []string{backendAddress, frontendAddress, adminAddress} {
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

func TestRunRejectsOccupiedAdminBeforeStartingBackend(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	root := t.TempDir()
	plan := NewPlan(Config{
		Backend: BackendConfig{WatchFile: "watch.yaml"},
		Admin: &AdminConfig{FrontendConfig: FrontendConfig{
			CWD: root, Command: "pnpm dev", URL: "http://" + listener.Addr().String(),
		}},
		Frontend: FrontendConfig{Mode: "disabled"},
	}, watch.NewPlan(watch.Config{
		CWD: root, Run: watch.CommandConfig{Command: "touch backend-started"},
		Health: watch.HealthConfig{URL: "http://" + unusedTestAddress(t)},
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := Run(ctx, plan, Streams{}); err == nil || !strings.Contains(err.Error(), "Admin Shell地址不可用") {
		t.Fatalf("occupied Admin error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "backend-started")); !os.IsNotExist(err) {
		t.Fatalf("backend started despite occupied Admin port: %v", err)
	}
	connection, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatalf("existing listener was affected: %v", err)
	}
	_ = connection.Close()
}
