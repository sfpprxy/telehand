package main

import (
	"os/exec"
	"reflect"
	"testing"
)

func mockLookPath(t *testing.T, paths map[string]string) {
	t.Helper()
	old := lookPath
	lookPath = func(name string) (string, error) {
		if path, ok := paths[name]; ok {
			return path, nil
		}
		return "", exec.ErrNotFound
	}
	t.Cleanup(func() {
		lookPath = old
	})
}

func TestBuildOpenBrowserCommandDarwinUsesOpen(t *testing.T) {
	mockLookPath(t, map[string]string{
		"open": "/usr/bin/open",
	})

	cmd, err := buildOpenBrowserCommand("darwin", "http://127.0.0.1:18080", 501, "", "", "")
	if err != nil {
		t.Fatalf("buildOpenBrowserCommand failed: %v", err)
	}
	if cmd.Path != "/usr/bin/open" {
		t.Fatalf("unexpected cmd path: %s", cmd.Path)
	}
	wantArgs := []string{"/usr/bin/open", "http://127.0.0.1:18080"}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("unexpected cmd args: got=%v want=%v", cmd.Args, wantArgs)
	}
}

func TestBuildOpenBrowserCommandDarwinRootUsesSudoUser(t *testing.T) {
	mockLookPath(t, map[string]string{
		"open": "/usr/bin/open",
		"sudo": "/usr/bin/sudo",
	})

	cmd, err := buildOpenBrowserCommand("darwin", "http://127.0.0.1:18080", 0, "joe", "", "")
	if err != nil {
		t.Fatalf("buildOpenBrowserCommand failed: %v", err)
	}
	if cmd.Path != "/usr/bin/sudo" {
		t.Fatalf("unexpected cmd path: %s", cmd.Path)
	}
	wantArgs := []string{"/usr/bin/sudo", "-u", "joe", "/usr/bin/open", "http://127.0.0.1:18080"}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("unexpected cmd args: got=%v want=%v", cmd.Args, wantArgs)
	}
}

func TestBuildOpenBrowserCommandDarwinRootFallsBackWhenSudoMissing(t *testing.T) {
	mockLookPath(t, map[string]string{
		"open": "/usr/bin/open",
	})

	cmd, err := buildOpenBrowserCommand("darwin", "http://127.0.0.1:18080", 0, "joe", "", "")
	if err != nil {
		t.Fatalf("buildOpenBrowserCommand failed: %v", err)
	}
	if cmd.Path != "/usr/bin/open" {
		t.Fatalf("unexpected cmd path: %s", cmd.Path)
	}
	wantArgs := []string{"/usr/bin/open", "http://127.0.0.1:18080"}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("unexpected cmd args: got=%v want=%v", cmd.Args, wantArgs)
	}
}

func TestBuildOpenBrowserCommandLinuxNeedsDisplay(t *testing.T) {
	mockLookPath(t, map[string]string{
		"xdg-open": "/usr/bin/xdg-open",
	})

	_, err := buildOpenBrowserCommand("linux", "http://127.0.0.1:18080", 1000, "", "", "")
	if err == nil || err.Error() != "DISPLAY/WAYLAND_DISPLAY is empty" {
		t.Fatalf("unexpected error: %v", err)
	}
}
