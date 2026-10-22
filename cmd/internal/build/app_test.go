package buildcmd

import (
	"strings"
	"testing"

	releasebuild "github.com/zane-xin/sparq-tool/internal/release/build"
)

func TestAppCommandExposesOnlySupportedBuildFlags(t *testing.T) {
	command := newAppCommand()
	for _, name := range []string{
		"target", "version", "upload", "debug", "tags",
		"config", "go-command", "go-arg", "frontend-command", "frontend-arg",
	} {
		if command.Flags().Lookup(name) == nil {
			t.Fatalf("app command is missing --%s", name)
		}
	}
	for _, name := range []string{
		"skip-admin", "distribution", "host-ownership", "host-update-policy", "admin-ownership", "admin-update-policy",
		"profile", "timeout", "changelog", "release-output", "signing-key-id", "signing-private-key",
	} {
		if command.Flags().Lookup(name) != nil {
			t.Fatalf("app command should not expose --%s", name)
		}
	}
}

func TestPrepareAppPublishOptionsSetsOfficialDefaults(t *testing.T) {
	options := releasebuild.AppOptions{Version: "v0.1.0"}
	err := prepareAppPublishOptions(&options, appPublishOptions{upload: true})
	if err != nil {
		t.Fatal(err)
	}
	if options.Version != "0.1.0" || len(options.Targets) != 6 {
		t.Fatalf("options = %+v", options)
	}
}

func TestAppCommandDefaultsToProductionAndAcceptsDevelopmentOverride(t *testing.T) {
	command := newAppCommand()
	tags, err := command.Flags().GetString("tags")
	if err != nil || tags != "production" {
		t.Fatalf("default tags = %q, %v", tags, err)
	}
	if err := command.ParseFlags([]string{"--tags", ""}); err != nil {
		t.Fatal(err)
	}
	tags, err = command.Flags().GetString("tags")
	if err != nil || tags != "" {
		t.Fatalf("development tags = %q, %v", tags, err)
	}
}

func TestPrepareAppPublishOptionsRejectsDevelopmentBuild(t *testing.T) {
	err := prepareAppPublishOptions(&releasebuild.AppOptions{Version: "dev"}, appPublishOptions{upload: true})
	if err == nil || !strings.Contains(err.Error(), "正式版本") {
		t.Fatalf("error = %v", err)
	}
}

func TestPrepareAppPublishOptionsRejectsPartialAndDebugBuilds(t *testing.T) {
	debug := releasebuild.AppOptions{Version: "0.1.0", Debug: true}
	if err := prepareAppPublishOptions(&debug, appPublishOptions{upload: true}); err == nil || !strings.Contains(err.Error(), "--debug") {
		t.Fatalf("debug error = %v", err)
	}
}
