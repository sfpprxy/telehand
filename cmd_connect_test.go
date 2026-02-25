package main

import (
	"strings"
	"testing"
)

func TestBuildRemoteInstallCommandsContainsPairingCode(t *testing.T) {
	code := "abc123=="
	cmds := buildRemoteInstallCommands(code)
	if len(cmds) != 4 {
		t.Fatalf("expected 4 platform commands, got %d", len(cmds))
	}
	hasWindowsInstall := false
	hasUnixInstall := false
	hasWindowsRunOnly := false
	hasUnixRunOnly := false
	for _, c := range cmds {
		if !strings.Contains(c.Command, code) {
			t.Fatalf("command for %s does not contain pairing code: %s", c.Platform, c.Command)
		}
		if c.Platform == "Windows (PowerShell，下载并运行)" {
			hasWindowsInstall = true
		}
		if c.Platform == "macOS / Linux（下载并运行）" {
			hasUnixInstall = true
		}
		if c.Platform == "Windows (PowerShell，仅运行)" {
			hasWindowsRunOnly = true
			if strings.Contains(c.Command, "install.ps1") {
				t.Fatalf("run-only windows command should not contain installer: %s", c.Command)
			}
		}
		if c.Platform == "macOS / Linux（仅运行）" {
			hasUnixRunOnly = true
			if strings.Contains(c.Command, "install.sh") || strings.Contains(c.Command, "curl ") {
				t.Fatalf("run-only unix command should not contain installer: %s", c.Command)
			}
		}
	}
	if !hasWindowsInstall {
		t.Fatalf("expected Windows install command")
	}
	if !hasUnixInstall {
		t.Fatalf("expected macOS/Linux install command")
	}
	if !hasWindowsRunOnly {
		t.Fatalf("expected Windows run-only command")
	}
	if !hasUnixRunOnly {
		t.Fatalf("expected macOS/Linux run-only command")
	}
}
