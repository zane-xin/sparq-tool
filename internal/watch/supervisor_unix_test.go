//go:build unix

package watch

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSupervisorCleansDescendantsAfterParentExit(t *testing.T) {
	for _, exitCode := range []int{0, 1} {
		t.Run(fmt.Sprintf("exit-%d", exitCode), func(t *testing.T) {
			root := t.TempDir()
			releasePath := filepath.Join(root, "exit-parent")
			groupPath := filepath.Join(root, "parent.pid")
			output, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close()
			supervisor, err := Start(context.Background(), Options{
				Name:  "测试进程",
				Watch: WatchOptions{Paths: []string{root}, Extensions: []string{".go"}, Mode: WatchModePoll, PollInterval: 10 * time.Millisecond, Debounce: 10 * time.Millisecond},
				Run: Command{
					Command: fmt.Sprintf("echo $$ > %q; sleep 30 & while [ ! -f %q ]; do sleep 0.01; done; exit %d", groupPath, releasePath, exitCode),
					Stdout:  output, Stderr: output, GraceTimeout: 100 * time.Millisecond, KillTimeout: time.Second,
				},
				ExitOnProcessExit: true, Writer: io.Discard,
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = supervisor.Close() })
			process, err := os.FindProcess(readPID(t, groupPath))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = killProcessTree(process)
				_ = process.Release()
			})
			if err := os.WriteFile(releasePath, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			select {
			case <-supervisor.Done():
			case <-time.After(3 * time.Second):
				t.Fatal("supervisor did not finish after parent exit")
			}
			if err := supervisor.Close(); err != nil {
				t.Fatal(err)
			}
			if !processTreeStopped(process) {
				t.Fatal("Close returned success with surviving descendants")
			}
		})
	}
}
