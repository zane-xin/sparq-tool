package watch

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFileMatcherAppliesIncludeAndExcludeGlobs(t *testing.T) {
	root := t.TempDir()
	matcher, err := newFileMatcher(WatchOptions{
		Include:      []string{"**/*.go", "go.mod"},
		ExcludeGlobs: []string{"**/*_test.go", "generated/**"},
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path string
		want bool
	}{
		{path: "main.go", want: true},
		{path: "internal/service.go", want: true},
		{path: "internal/service_test.go", want: false},
		{path: "generated/model.go", want: false},
		{path: "go.mod", want: true},
		{path: "README.md", want: false},
	}
	for _, test := range tests {
		if got := matcher.matches(root, filepath.Join(root, filepath.FromSlash(test.path))); got != test.want {
			t.Errorf("matches(%q) = %v, want %v", test.path, got, test.want)
		}
	}
}

func TestFileMatcherRejectsEmptyGlob(t *testing.T) {
	_, err := newFileMatcher(WatchOptions{Include: []string{" "}})
	if err == nil || !strings.Contains(err.Error(), "空 pattern") {
		t.Fatalf("newFileMatcher() error = %v", err)
	}
}

func TestFileMatcherSupportsUnicodeGlobs(t *testing.T) {
	root := t.TempDir()
	matcher, err := newFileMatcher(WatchOptions{Include: []string{"内容/**/*.go"}})
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(root, "内容", "页面", "详情.go")
	if !matcher.matches(root, filename) {
		t.Fatalf("unicode path %q did not match", filename)
	}
}
