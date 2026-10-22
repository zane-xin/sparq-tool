package buildpipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPlatformsBuildsEachRequestedTarget(t *testing.T) {
	root := createPlugin(t, true, false)
	var calls []string
	results, err := buildPlatforms(context.Background(), PlatformOptions{
		Dir:       root,
		Platforms: []Platform{{OS: "linux", Arch: "amd64"}, {OS: "windows", Arch: "arm64"}},
	}, func(_ context.Context, _ string, environment []string, name string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(environment[len(environment)-4:], ",")+" "+name+" "+strings.Join(args, " "))
		for index, argument := range args {
			if argument == "-o" && index+1 < len(args) {
				if err := os.WriteFile(args[index+1], []byte("binary"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || filepath.Base(results[0].Path) != "demo" || filepath.Base(results[1].Path) != "demo.exe" {
		t.Fatalf("results = %+v", results)
	}
	joined := strings.Join(calls, "\n")
	for _, expected := range []string{"GOOS=linux,GOARCH=amd64,CGO_ENABLED=0,GOWORK=off", "GOOS=windows,GOARCH=arm64,CGO_ENABLED=0,GOWORK=off"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("calls missing %q:\n%s", expected, joined)
		}
	}
	if !strings.Contains(joined, "go build -buildvcs=false -trimpath -ldflags -s -w -tags production -o") {
		t.Fatalf("platform builds do not use size optimization:\n%s", joined)
	}
}

func TestBuildPlatformsKeepsDebugInformationWhenConfigured(t *testing.T) {
	root := createPlugin(t, true, false)
	writeFile(t, root, ConfigFileName, "version: 1\ngo:\n  optimization: debug\n")
	var arguments string
	_, err := buildPlatforms(context.Background(), PlatformOptions{
		Dir: root, Platforms: []Platform{{OS: "linux", Arch: "amd64"}},
	}, func(_ context.Context, _ string, _ []string, _ string, args ...string) ([]byte, error) {
		arguments = strings.Join(args, " ")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(arguments, "-trimpath") || strings.Contains(arguments, "-ldflags") {
		t.Fatalf("debug build arguments = %q", arguments)
	}
}

func TestBuildPlatformsSkipsPluginWithoutRemoteRunner(t *testing.T) {
	root := createPlugin(t, true, false)
	if err := os.RemoveAll(filepath.Join(root, "cmd")); err != nil {
		t.Fatal(err)
	}
	results, err := buildPlatforms(context.Background(), PlatformOptions{Dir: root}, func(context.Context, string, []string, string, ...string) ([]byte, error) {
		t.Fatal("runner should not be called")
		return nil, nil
	})
	if err != nil || len(results) != 0 {
		t.Fatalf("results = %+v, error = %v", results, err)
	}
}

func TestValidatePlatformsRejectsUnsupportedOrDuplicateTarget(t *testing.T) {
	for _, platforms := range [][]Platform{
		{{OS: "freebsd", Arch: "amd64"}},
		{{OS: "linux", Arch: "386"}},
		{{OS: "linux", Arch: "amd64"}, {OS: "linux", Arch: "amd64"}},
	} {
		if err := validatePlatforms(platforms); err == nil {
			t.Fatalf("validatePlatforms(%+v) error = nil", platforms)
		}
	}
}

func TestDefaultPublishPlatformsIncludesSixSupportedTargets(t *testing.T) {
	platforms := DefaultPublishPlatforms()
	if len(platforms) != 6 {
		t.Fatalf("platform count = %d, want 6", len(platforms))
	}
	if err := validatePlatforms(platforms); err != nil {
		t.Fatalf("default platforms are invalid: %v", err)
	}
}
