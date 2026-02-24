//go:build windows

package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

func isCurrentUserAdmin() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(
		ctx,
		"powershell",
		"-NoProfile",
		"-NonInteractive",
		"-Command",
		"([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)",
	)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return false, fmt.Errorf("administrator check timed out")
	}
	if err != nil {
		return false, fmt.Errorf("administrator check failed: %v", err)
	}

	result := strings.ToLower(strings.TrimSpace(string(out)))
	switch result {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("unexpected administrator check output: %q", result)
	}
}
