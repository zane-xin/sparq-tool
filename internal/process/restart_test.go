package process

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunRejectsUnsupportedDriver(t *testing.T) {
	err := Run(context.Background(), Options{Driver: "unknown"})
	if err == nil {
		t.Fatal("expected unsupported driver error")
	}
}

func TestRunRequiresCommand(t *testing.T) {
	err := Run(context.Background(), Options{Driver: "command"})
	if err == nil {
		t.Fatal("expected command required error")
	}
}

func TestReservedDriver(t *testing.T) {
	if !IsReservedDriver("systemd") || !IsReservedDriver("docker-compose") {
		t.Fatal("expected reserved drivers")
	}
}

func TestRunUsesInjectedOutput(t *testing.T) {
	var output bytes.Buffer
	err := Run(context.Background(), Options{
		Driver: "command", Command: "echo sparq-process-test", Stdout: &output, Stderr: &output,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "sparq-process-test") {
		t.Fatalf("output = %q", output.String())
	}
}
