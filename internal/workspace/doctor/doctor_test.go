package doctor

import (
	"path/filepath"
	"testing"
)

func TestCheckManifestWarnsWhenMissing(t *testing.T) {
	check := checkManifest(filepath.Join(t.TempDir(), "plugin.yaml"))
	if check.Status != StatusWarn {
		t.Fatalf("status = %s, want %s", check.Status, StatusWarn)
	}
}

func TestHasFailed(t *testing.T) {
	if !HasFailed([]Check{{Status: StatusFail}}) {
		t.Fatal("expected failed checks")
	}
	if HasFailed([]Check{{Status: StatusOK}, {Status: StatusWarn}}) {
		t.Fatal("did not expect failed checks")
	}
}

func TestCheckTarget(t *testing.T) {
	check := checkTarget()
	if check.Status != StatusOK || check.Detail == "" {
		t.Fatalf("unexpected target check: %#v", check)
	}
}
