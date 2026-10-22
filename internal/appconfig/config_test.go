package appconfig

import (
	"strings"
	"testing"
)

func TestParseReadsSharedApplicationConfig(t *testing.T) {
	config, err := Parse([]byte(`
target:
  kind: application
backend:
  watch_file: config/sparq.watch.yaml
app:
  dir: .
  package: ./cmd/site
  binary: site
  output: release/bin
frontend:
  mode: source
  cwd: frontend
  command: pnpm dev
  script: build
toolchain:
  go:
    command: go1.24
    args: [-mod=vendor]
`))
	if err != nil {
		t.Fatal(err)
	}
	if err := config.ValidateCommon(); err != nil {
		t.Fatal(err)
	}
	if config.App.Binary != "site" || config.Frontend.Command != "pnpm dev" || config.Toolchain.Go.Command != "go1.24" {
		t.Fatalf("config = %+v", config)
	}
}

func TestParseRejectsUnknownSharedField(t *testing.T) {
	_, err := Parse([]byte("app:\n  package_name: ./cmd/site\n"))
	if err == nil || !strings.Contains(err.Error(), "package_name") {
		t.Fatalf("Parse() error = %v", err)
	}
}

func TestValidateCommonRejectsUnsupportedTargetAndMode(t *testing.T) {
	for _, config := range []Config{
		{Target: TargetConfig{Kind: "plugin"}},
		{Frontend: FrontendConfig{Mode: "remote"}},
	} {
		if err := config.ValidateCommon(); err == nil {
			t.Fatalf("ValidateCommon(%+v) should fail", config)
		}
	}
}
