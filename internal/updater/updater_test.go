package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	protocolrelease "github.com/zane-xin/sparq-protocol/release"
)

func TestPlanRejectsProtectedAndEscapedTargets(t *testing.T) {
	root := t.TempDir()
	plan := testPlan(root, testArtifact(t, root, protocolrelease.ArtifactRuntime, "release/bin/sparq"))
	plan.Artifacts[0].TargetPath = filepath.Join(root, "plugins", "cms")
	if err := plan.Validate(); err == nil {
		t.Fatal("expected protected plugin path to fail")
	}
	plan.Artifacts[0].TargetPath = filepath.Join(root, "storage", "plugins", "cms")
	if err := plan.Validate(); err == nil {
		t.Fatal("expected protected storage plugin path to fail")
	}
	plan.Artifacts[0].TargetPath = filepath.Join(filepath.Dir(root), "escaped")
	if err := plan.Validate(); err == nil {
		t.Fatal("expected escaped target to fail")
	}
}

func TestApplyAndRollbackPreservePreviousArtifact(t *testing.T) {
	root := t.TempDir()
	artifact := testArtifact(t, root, protocolrelease.ArtifactRuntime, "release/bin/sparq")
	writeTestFile(t, artifact.TargetPath, "old")
	writeTestFile(t, artifact.SourcePath, "new")
	digest := sha256.Sum256([]byte("new"))
	artifact.SHA256 = hex.EncodeToString(digest[:])
	plan := testPlan(root, artifact)

	result, err := Apply(plan)
	if err != nil || len(result.Applied) != 1 || readTestFile(t, artifact.TargetPath) != "new" || readTestFile(t, artifact.BackupPath) != "old" {
		t.Fatalf("Apply() = %#v, %v", result, err)
	}
	if err := Rollback(plan); err != nil {
		t.Fatal(err)
	}
	if readTestFile(t, artifact.TargetPath) != "old" || readTestFile(t, artifact.SourcePath) != "new" {
		t.Fatal("rollback did not preserve old and failed replacement files")
	}
}

func TestApplyDigestFailureLeavesCurrentArtifactUntouched(t *testing.T) {
	root := t.TempDir()
	artifact := testArtifact(t, root, protocolrelease.ArtifactRuntime, "release/bin/sparq")
	artifact.SHA256 = string(make([]byte, 64))
	writeTestFile(t, artifact.TargetPath, "old")
	writeTestFile(t, artifact.SourcePath, "new")
	if _, err := Apply(testPlan(root, artifact)); err == nil {
		t.Fatal("expected digest mismatch")
	}
	if readTestFile(t, artifact.TargetPath) != "old" {
		t.Fatal("digest failure changed current artifact")
	}
	if _, err := os.Stat(artifact.BackupPath); !os.IsNotExist(err) {
		t.Fatalf("digest failure created backup: %v", err)
	}
}

func TestApplyAndRollbackReplaceAdminDirectory(t *testing.T) {
	root := t.TempDir()
	artifact := testArtifact(t, root, protocolrelease.ArtifactAdmin, "public/admin")
	artifact.Kind = "directory"
	writeTestFile(t, filepath.Join(artifact.TargetPath, "index.html"), "old")
	writeTestFile(t, filepath.Join(artifact.SourcePath, "index.html"), "new")
	plan := testPlan(root, testArtifact(t, root, protocolrelease.ArtifactRuntime, "release/bin/sparq"), artifact)
	writeTestFile(t, plan.Artifacts[0].SourcePath, "runtime-new")

	if _, err := Apply(plan); err != nil {
		t.Fatal(err)
	}
	if readTestFile(t, filepath.Join(artifact.TargetPath, "index.html")) != "new" {
		t.Fatal("admin directory was not replaced")
	}
	if err := Rollback(plan); err != nil {
		t.Fatal(err)
	}
	if readTestFile(t, filepath.Join(artifact.TargetPath, "index.html")) != "old" || readTestFile(t, filepath.Join(artifact.SourcePath, "index.html")) != "new" {
		t.Fatal("admin directory rollback did not preserve both versions")
	}
}

func testPlan(root string, artifacts ...ArtifactPlan) Plan {
	runtimePath := ""
	for _, artifact := range artifacts {
		if artifact.Code == protocolrelease.ArtifactRuntime {
			runtimePath = artifact.TargetPath
			break
		}
	}
	return Plan{
		Format: PlanFormat, AppRoot: root, WorkingDir: root,
		Transaction: protocolrelease.UpdateTransaction{ID: "update-1", TargetVersion: "0.2.0", Status: protocolrelease.UpdateStatusPrepared},
		Artifacts:   artifacts,
		Process: ProcessPlan{
			HostPID: 2147483647, ExecutablePath: runtimePath,
			ConfirmationPath:       filepath.Join(root, "storage", "update", "update-1", "confirmation.json"),
			HostExitTimeoutSeconds: 1, ConfirmationTimeoutSeconds: 5,
		},
	}
}

func testArtifact(t *testing.T, root, code, target string) ArtifactPlan {
	t.Helper()
	return ArtifactPlan{Code: code, Kind: "file", SourcePath: filepath.Join(root, "storage", "update", "update-1", "prepared", code), TargetPath: filepath.Join(root, filepath.FromSlash(target)), BackupPath: filepath.Join(root, "storage", "update", "update-1", "backup", code)}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
