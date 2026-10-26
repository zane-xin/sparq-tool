package buildpipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFrontendIconsAreDigestedAndValidated(t *testing.T) {
	output := t.TempDir()
	if err := os.MkdirAll(filepath.Join(output, ".vite"), 0755); err != nil {
		t.Fatal(err)
	}
	svg := filepath.Join(output, "article.svg")
	safe := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M1 2h3"/></svg>`
	if err := os.WriteFile(svg, []byte(safe), 0644); err != nil {
		t.Fatal(err)
	}
	descriptor, _ := json.Marshal(map[string]any{"schema": "sparq.frontend-icons/v1", "icons": map[string]string{"article": "article.svg"}})
	if err := os.WriteFile(filepath.Join(output, ".vite", "sparq-icons.json"), descriptor, 0644); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	icons, err := readFrontendIcons(output, files)
	if err != nil || icons["article"] != "article.svg" || files["article.svg"] == "" {
		t.Fatalf("icons=%v files=%v err=%v", icons, files, err)
	}
	if err := os.WriteFile(svg, []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><script/></svg>`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readFrontendIcons(output, map[string]string{}); err == nil {
		t.Fatal("accepted scripted SVG")
	}
	if err := os.Remove(svg); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.svg")
	if err := os.WriteFile(outside, []byte(safe), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, svg); err != nil {
		t.Fatal(err)
	}
	if _, err := readFrontendIcons(output, map[string]string{}); err == nil {
		t.Fatal("accepted symlink SVG")
	}
}
