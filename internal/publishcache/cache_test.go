package publishcache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCandidateCacheRoundTripUsesPrivateFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SPARQ_HOME", home)
	archive := filepath.Join(t.TempDir(), "cms.tar.gz")
	if err := os.WriteFile(archive, []byte("candidate"), 0o644); err != nil {
		t.Fatal(err)
	}
	stored, err := Save("attempt:test", Snapshot{
		Code: "cms", TargetVersion: "1.2.3", SourceFingerprint: strings.Repeat("a", 64), BuildFingerprint: "rules-v1",
		Candidates: []Candidate{{Kind: "managed_remote", Runtime: "http", TargetOS: "linux", TargetArch: "amd64", ArchivePath: archive}},
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, found, err := Load("attempt:test", time.Now())
	if err != nil || !found || loaded.Candidates[0].SHA256 != stored.Candidates[0].SHA256 || loaded.Candidates[0].ArchivePath == archive {
		t.Fatalf("Load() = %+v, %v, %v", loaded, found, err)
	}
	root := filepath.Join(home, "sparq-tool", directoryName)
	rootInfo, err := os.Stat(root)
	if err != nil || rootInfo.Mode().Perm() != 0o700 {
		t.Fatalf("cache root mode = %v, %v", rootInfo.Mode().Perm(), err)
	}
	fileInfo, err := os.Stat(loaded.Candidates[0].ArchivePath)
	if err != nil || fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("candidate mode = %v, %v", fileInfo.Mode().Perm(), err)
	}
}

func TestCandidateCacheRejectsDamageAndExpiresAfterTwoHours(t *testing.T) {
	t.Setenv("SPARQ_HOME", t.TempDir())
	archive := filepath.Join(t.TempDir(), "cms.tar.gz")
	if err := os.WriteFile(archive, []byte("candidate"), 0o600); err != nil {
		t.Fatal(err)
	}
	stored, err := Save("attempt:damaged", Snapshot{
		Code: "cms", TargetVersion: "1.2.3", SourceFingerprint: strings.Repeat("b", 64), BuildFingerprint: "rules-v1",
		Candidates: []Candidate{{Kind: "plugin_package", ArchivePath: archive}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stored.Candidates[0].ArchivePath, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load("attempt:damaged", time.Now()); err == nil || !strings.Contains(err.Error(), "已损坏") {
		t.Fatalf("Load(damaged) error = %v", err)
	}
	if err := Delete("attempt:damaged"); err != nil {
		t.Fatal(err)
	}
	stored, err = Save("attempt:expired", Snapshot{
		Code: "cms", TargetVersion: "1.2.3", SourceFingerprint: strings.Repeat("c", 64), BuildFingerprint: "rules-v1",
		Candidates: []Candidate{{Kind: "plugin_package", ArchivePath: archive}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := Load("attempt:expired", time.Now().Add(Lifetime)); err != nil || found {
		t.Fatalf("Load(expired) found=%v error=%v", found, err)
	}
	if _, err := os.Stat(filepath.Dir(stored.Candidates[0].ArchivePath)); !os.IsNotExist(err) {
		t.Fatalf("expired cache remains: %v", err)
	}
}

func TestCleanExpiredUsesLastActivityFromManifest(t *testing.T) {
	t.Setenv("SPARQ_HOME", t.TempDir())
	archive := filepath.Join(t.TempDir(), "cms.tar.gz")
	if err := os.WriteFile(archive, []byte("candidate"), 0o600); err != nil {
		t.Fatal(err)
	}
	stored, err := Save("attempt:cleanup", Snapshot{
		Code: "cms", TargetVersion: "1.2.3", SourceFingerprint: strings.Repeat("d", 64), BuildFingerprint: "rules-v1",
		Candidates: []Candidate{{Kind: "plugin_package", ArchivePath: archive}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := CleanExpired(time.Now().Add(Lifetime + time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(stored.Candidates[0].ArchivePath)); !os.IsNotExist(err) {
		t.Fatalf("CleanExpired() retained cache: %v", err)
	}
}
