//go:build !windows

package updater

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	protocolrelease "github.com/zane-xin/sparq-protocol/release"
)

func TestExecuteStartsRuntimeAndAcceptsMatchingConfirmation(t *testing.T) {
	root := t.TempDir()
	artifact := testArtifact(t, root, protocolrelease.ArtifactRuntime, "release/bin/sparq")
	writeTestFile(t, artifact.TargetPath, "old")
	writeTestFile(t, artifact.SourcePath, "#!/bin/sh\nprintf '{\"format\":\"sparq.update.confirmation.v1\",\"transaction_id\":\"update-1\",\"confirmed_at\":\"2026-08-19T12:00:00Z\"}' > \"$1\"\nsleep 5\n")
	plan := testPlan(root, artifact)
	plan.Process.Arguments = []string{plan.Process.ConfirmationPath}

	result, err := Execute(plan)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(result.RuntimePID, syscall.SIGKILL) })
	if result.RuntimePID <= 0 || readTestFile(t, artifact.TargetPath)[:9] != "#!/bin/sh" {
		t.Fatalf("Execute() = %#v", result)
	}
}

func TestExecuteRollsBackWhenRuntimeExitsBeforeConfirmation(t *testing.T) {
	root := t.TempDir()
	artifact := testArtifact(t, root, protocolrelease.ArtifactRuntime, "release/bin/sparq")
	marker := filepath.Join(root, "restored-runtime.started")
	writeTestFile(t, artifact.TargetPath, "#!/bin/sh\nprintf restored > \"$1\"\n")
	writeTestFile(t, artifact.SourcePath, "#!/bin/sh\nexit 1\n")
	plan := testPlan(root, artifact)
	plan.Process.Arguments = []string{marker}

	if _, err := Execute(plan); err == nil {
		t.Fatal("expected startup failure")
	}
	if !strings.HasPrefix(readTestFile(t, artifact.TargetPath), "#!/bin/sh") {
		t.Fatal("startup failure did not restore previous runtime")
	}
	if _, err := os.Stat(filepath.Clean(artifact.SourcePath)); err != nil {
		t.Fatalf("failed runtime was not retained: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(filepath.Dir(plan.Process.ConfirmationPath), "transaction.json"))
	if err != nil {
		t.Fatal(err)
	}
	var transaction protocolrelease.UpdateTransaction
	if err := json.Unmarshal(body, &transaction); err != nil {
		t.Fatal(err)
	}
	if transaction.Status != protocolrelease.UpdateStatusRolledBack {
		t.Fatalf("transaction status = %s, want rolled_back", transaction.Status)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if body, readErr := os.ReadFile(marker); readErr == nil && string(body) == "restored" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("restored runtime was not restarted")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
