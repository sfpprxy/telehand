package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		path, err := exec.LookPath("open")
		if err != nil {
			return err
		}
		cmd = exec.Command(path, url)
	case "windows":
		path, err := exec.LookPath("cmd")
		if err != nil {
			return err
		}
		cmd = exec.Command(path, "/c", "start", "", url)
	default:
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return errors.New("DISPLAY/WAYLAND_DISPLAY is empty")
		}
		path, err := exec.LookPath("xdg-open")
		if err != nil {
			return err
		}
		cmd = exec.Command(path, url)
	}
	return cmd.Start()
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
	path, err := exec.LookPath(name)
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
