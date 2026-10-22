package buildercmd

import "testing"

func TestBuilderCommandOnlyExposesOfflineCapsuleOperations(t *testing.T) {
	command := New()
	commands := command.Commands()
	if len(commands) != 1 || commands[0].Name() != "capsule" {
		t.Fatalf("builder commands = %+v", commands)
	}
	for _, name := range []string{"endpoint", "token", "token-file", "token-stdin"} {
		if command.Flags().Lookup(name) != nil || command.PersistentFlags().Lookup(name) != nil {
			t.Fatalf("builder command should not expose --%s", name)
		}
	}
}
