package main

import (
	"strings"
	"testing"
)

func TestBuildRemoteInstallCommandsContainsPairingCode(t *testing.T) {
	code := "abc123=="
	cmds := buildRemoteInstallCommands(code)
	if len(cmds) < 3 {
		t.Fatalf("expected 3 platform commands, got %d", len(cmds))
	}
	for _, c := range cmds {
		if !strings.Contains(c.Command, code) {
			t.Fatalf("command for %s does not contain pairing code: %s", c.Platform, c.Command)
		}
	}
}
