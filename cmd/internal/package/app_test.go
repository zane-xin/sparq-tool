package packagecmd

import "testing"

func TestAppCommandExposesBundleFlags(t *testing.T) {
	command := newAppCommand()
	for _, name := range []string{"input", "output", "version", "target", "archive"} {
		if command.Flags().Lookup(name) == nil {
			t.Fatalf("package app command is missing --%s", name)
		}
	}
}
