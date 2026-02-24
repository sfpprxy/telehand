package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

var lookPath = exec.LookPath

func openBrowser(url string) error {
	cmd, err := buildOpenBrowserCommand(
		runtime.GOOS,
		url,
		os.Geteuid(),
		os.Getenv("SUDO_USER"),
		os.Getenv("DISPLAY"),
		os.Getenv("WAYLAND_DISPLAY"),
	)
	if err != nil {
		return err
	}
	return cmd.Start()
}

func buildOpenBrowserCommand(goos, url string, euid int, sudoUser, display, waylandDisplay string) (*exec.Cmd, error) {
	var cmd *exec.Cmd
	switch goos {
	case "darwin":
		openPath, err := lookPath("open")
		if err != nil {
			return nil, err
		}
		if euid == 0 && strings.TrimSpace(sudoUser) != "" {
			// When telehand is started via sudo on macOS, run `open` as the original user
			// so LaunchServices resolves the same default browser seen in System Settings.
			sudoPath, sudoErr := lookPath("sudo")
			if sudoErr == nil {
				cmd = exec.Command(sudoPath, "-u", strings.TrimSpace(sudoUser), openPath, url)
				return cmd, nil
			}
		}
		cmd = exec.Command(openPath, url)
	case "windows":
		path, err := lookPath("cmd")
		if err != nil {
			return nil, err
		}
		cmd = exec.Command(path, "/c", "start", "", url)
	default:
		if display == "" && waylandDisplay == "" {
			return nil, errors.New("DISPLAY/WAYLAND_DISPLAY is empty")
		}
		path, err := lookPath("xdg-open")
		if err != nil {
			return nil, err
		}
		cmd = exec.Command(path, url)
	}
	return cmd, nil
}

func copyToClipboard(text string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("empty text")
	}

	switch runtime.GOOS {
	case "darwin":
		return runClipboardCommand("pbcopy", nil, text)
	case "windows":
		return runClipboardCommand("powershell", []string{"-NoProfile", "-Command", "Set-Clipboard -Value $input"}, text)
	default:
		candidates := [][]string{
			{"wl-copy"},
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
		}
		var lastErr error
		for _, candidate := range candidates {
			if err := runClipboardCommand(candidate[0], candidate[1:], text); err == nil {
				return nil
			} else {
				lastErr = err
			}
		}
		if lastErr == nil {
			lastErr = errors.New("no clipboard command available")
		}
		return lastErr
	}
}

func runClipboardCommand(name string, args []string, input string) error {
	path, err := lookPath(name)
	if err != nil {
		return err
	}
	cmd := exec.Command(path, args...)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s failed: %v (%s)", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}
