package main

import (
	"strings"
	"testing"
)

func TestBuildRemoteInstallCommandsContainsPairingCode(t *testing.T) {
	code := "abc123=="
	cmds := buildRemoteInstallCommands(code)
	if len(cmds) != 2 {
		t.Fatalf("expected 2 platform commands, got %d", len(cmds))
	}
	hasWindows := false
	hasUnix := false
	for _, c := range cmds {
		if !strings.Contains(c.Command, code) {
			t.Fatalf("command for %s does not contain pairing code: %s", c.Platform, c.Command)
		}
		if c.Platform == "Windows (PowerShell)" {
			hasWindows = true
		}
		if c.Platform == "macOS / Linux" {
			hasUnix = true
		}
	}
	if !hasWindows {
		t.Fatalf("expected Windows (PowerShell) command")
	}
	if !hasUnix {
		t.Fatalf("expected macOS / Linux command")
	}
}
