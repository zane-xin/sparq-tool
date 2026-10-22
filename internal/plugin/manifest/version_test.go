package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseVersionAndBump(t *testing.T) {
	current, err := ParseVersion("1.2.3-beta.1+build.7")
	if err != nil {
		t.Fatal(err)
	}
	if current.String() != "1.2.3-beta.1+build.7" {
		t.Fatalf("version = %s", current)
	}
	wants := map[string]string{"patch": "1.2.4", "minor": "1.3.0", "major": "2.0.0"}
	for kind, want := range wants {
		next, bumpErr := current.Bump(kind)
		if bumpErr != nil {
			t.Fatalf("Bump(%s): %v", kind, bumpErr)
		}
		if next.String() != want {
			t.Fatalf("Bump(%s) = %s, want %s", kind, next, want)
		}
	}
}

func TestParseVersionRejectsInvalidSemVer(t *testing.T) {
	for _, value := range []string{"1", "1.2", "01.2.3", "1.02.3", "1.2.03", "1.2.3-01", "1.2.3-", "1.2.3+", "v1.2.3", "1.2.3_4", " 1.2.3"} {
		if _, err := ParseVersion(value); err == nil {
			t.Fatalf("ParseVersion(%q) error = nil", value)
		}
	}
}

func TestUpdateVersionPreservesComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plugin.yaml")
	body := `# 插件清单说明
apiVersion: sparq.io/v1
kind: Plugin
metadata:
  id: cms
  code: cms
  name: CMS
  version: 1.2.3 # 当前发布版本
sparq:
  protocol: ">=0.1.0 <0.2.0"
spec:
  runtime:
    supported: [local]
`
	if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	target, err := ParseVersion("1.3.0-beta.1")
	if err != nil {
		t.Fatal(err)
	}
	previous, err := UpdateVersion(path, target)
	if err != nil {
		t.Fatal(err)
	}
	if previous != "1.2.3" {
		t.Fatalf("previous = %q", previous)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"# 插件清单说明", "version: 1.3.0-beta.1 # 当前发布版本"} {
		if !strings.Contains(string(updated), expected) {
			t.Fatalf("updated manifest missing %q:\n%s", expected, updated)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}
