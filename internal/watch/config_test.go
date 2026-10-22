package watch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseConfigRejectsUnknownField(t *testing.T) {
	_, err := parseConfig([]byte("run:\n  command: go run .\nunknown: true\n"))
	if err == nil || !strings.Contains(err.Error(), "field unknown not found") {
		t.Fatalf("parseConfig() error = %v", err)
	}
}

func TestNewPlanResolvesDefaultsRelativeToConfig(t *testing.T) {
	baseDir := t.TempDir()
	projectDir := filepath.Join(baseDir, "project")
	if err := os.Mkdir(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	plan := NewPlan(Config{
		baseDir: baseDir,
		CWD:     "project",
		Run:     CommandConfig{Command: "go run .", Env: map[string]string{"Z": "last", "A": "first"}},
	})
	if err := plan.Validate(); err != nil {
		t.Fatal(err)
	}
	if plan.CWD != projectDir || len(plan.WatchPaths) != 1 || plan.WatchPaths[0] != projectDir {
		t.Fatalf("resolved paths = cwd %q, watch %+v", plan.CWD, plan.WatchPaths)
	}
	if plan.BuildCommand != "" || plan.Debounce != 300*time.Millisecond {
		t.Fatalf("defaults = build %q, debounce %s", plan.BuildCommand, plan.Debounce)
	}
	if strings.Join(plan.RunEnv, ",") != "A=first,Z=last" {
		t.Fatalf("run env = %+v", plan.RunEnv)
	}
}

func TestPlanValidatesRequiredRunAndHealth(t *testing.T) {
	baseDir := t.TempDir()
	basePlan := NewPlan(Config{baseDir: baseDir})
	if err := basePlan.Validate(); err == nil || !strings.Contains(err.Error(), "run.command") {
		t.Fatalf("missing run.command error = %v", err)
	}

	basePlan.RunCommand = "go run ."
	basePlan.healthConfigured = true
	if err := basePlan.Validate(); err == nil || !strings.Contains(err.Error(), "health.url") {
		t.Fatalf("missing health.url error = %v", err)
	}

	basePlan.HealthURL = "ftp://example.com"
	if err := basePlan.Validate(); err == nil || !strings.Contains(err.Error(), "HTTP(S)") {
		t.Fatalf("invalid health.url error = %v", err)
	}
}
