package rootcmd

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRootCommandIdentityAndSurface(t *testing.T) {
	command := New()
	if command.Use != "sparq-tool" {
		t.Fatalf("root use = %q, want sparq-tool", command.Use)
	}

	got := make([]string, 0, len(command.Commands()))
	seen := make(map[string]struct{}, len(command.Commands()))
	for _, child := range command.Commands() {
		name := child.Name()
		if _, exists := seen[name]; exists {
			t.Fatalf("duplicate root command %q", name)
		}
		seen[name] = struct{}{}
		got = append(got, name)
	}
	sort.Strings(got)
	want := []string{
		"artifact", "auth", "build", "builder", "checksum", "dev", "doctor",
		"local", "manifest", "package", "plugin", "restart", "signature", "theme", "watch",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("root commands = %v, want %v", got, want)
	}
}

func TestPluginAndThemeCommandSurfaces(t *testing.T) {
	root := New()
	assertCommandNames(t, root, "plugin", []string{"add", "build", "download", "info", "init", "list", "package", "publish", "search"})
	assertCommandNames(t, root, "theme", []string{"apply", "download", "info", "install", "list", "publish", "remove", "rollback", "search", "update"})
}

func assertCommandNames(t *testing.T, root *cobra.Command, name string, want []string) {
	t.Helper()
	command, _, err := root.Find([]string{name})
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(command.Commands()))
	for _, child := range command.Commands() {
		got = append(got, child.Name())
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%s commands = %v, want %v", name, got, want)
	}
}

func TestCapabilitiesDoNotImportCobra(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file")
	}
	capabilityRoot := filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "internal")
	err := filepath.WalkDir(capabilityRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if importPath == "github.com/spf13/cobra" {
				t.Errorf("capability file imports Cobra: %s", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
