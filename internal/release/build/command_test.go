package build

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zane-xin/sparq-tool/internal/toolchain"
)

func TestBuildFailuresPreserveMissingExecutable(t *testing.T) {
	t.Setenv("SPARQ_HOME", t.TempDir())
	root := t.TempDir()
	missing := toolchain.Spec{Command: filepath.Join(root, "missing-tool")}
	component := BuildGoComponent{Path: ".", Package: "./cmd/app", Binary: "app"}
	err := buildConfiguredGo(root, component, filepath.Join(root, "app"), Platform{OS: "linux", Arch: "amd64"}, AppOptions{}, missing, false)
	if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), missing.Command) {
		t.Fatalf("buildConfiguredGo() error = %v", err)
	}
}

func TestRunBuildCommandPreservesOutputAndExitStatus(t *testing.T) {
	if os.Getenv("SPARQ_BUILD_ERROR_TEST") == "1" {
		fmt.Fprintln(os.Stdout, "compiler stdout")
		fmt.Fprintln(os.Stderr, "compiler stderr")
		os.Exit(7)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestRunBuildCommandPreservesOutputAndExitStatus$")
	command.Dir = t.TempDir()
	command.Env = append(os.Environ(), "SPARQ_BUILD_ERROR_TEST=1")
	err = runBuildCommand("应用构建失败", command)
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
		t.Fatalf("exit status was lost: %v", err)
	}
	for _, want := range []string{"应用构建失败", executable, command.Dir, "compiler stdout", "compiler stderr"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}
