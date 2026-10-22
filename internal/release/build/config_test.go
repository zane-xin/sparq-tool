package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveBuildConfigPathUsesEnvironmentFile(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "build.yaml")
	if err := os.WriteFile(filepath.Join(root, "build.release.yaml"), []byte("app: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := ResolveBuildConfigPath(base, "release")
	if err != nil || path != filepath.Join(root, "build.release.yaml") {
		t.Fatalf("ResolveBuildConfigPath() = %q, %v", path, err)
	}
}

func TestResolveBuildConfigPathRejectsMissingEnvironmentFile(t *testing.T) {
	_, err := ResolveBuildConfigPath(filepath.Join(t.TempDir(), "build.yaml"), "release")
	if err == nil || !strings.Contains(err.Error(), "构建环境配置不存在") {
		t.Fatalf("ResolveBuildConfigPath() error = %v", err)
	}
}

func TestBuildConfigValidatesOptionalComponentFields(t *testing.T) {
	config := BuildConfig{App: BuildGoComponent{Path: ".", Package: "./cmd/app", Binary: "app"}, Output: BuildOutput{Dir: "release"}, Updater: BuildGoComponent{Path: "updater"}}
	if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "updater.package") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestBuildConfigValidatesCopyEntries(t *testing.T) {
	base := BuildConfig{App: BuildGoComponent{Path: ".", Package: "./cmd/app", Binary: "app"}, Output: BuildOutput{Dir: "release"}}
	base.Copy = []BuildCopyEntry{{Source: "themes", Target: "themes"}}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid copy directory rejected: %v", err)
	}
	base.Copy[0].Target = "../themes"
	if err := base.Validate(); err == nil || !strings.Contains(err.Error(), "copy[0].target") {
		t.Fatalf("path escape was accepted: %v", err)
	}
	base.Copy[0] = BuildCopyEntry{Source: "../themes", Target: "themes"}
	if err := base.Validate(); err == nil || !strings.Contains(err.Error(), "copy[0].source") {
		t.Fatalf("source path escape was accepted: %v", err)
	}
}
