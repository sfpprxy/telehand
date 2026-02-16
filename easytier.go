package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

type EasyTier struct {
	cmd     *exec.Cmd
	tmpDir  string
	VirtIP  string
	mu      sync.Mutex
	logs    []string
	onIP    func(string)
}

func NewEasyTier(cfg *Config, onIP func(string)) *EasyTier {
	return &EasyTier{onIP: onIP}
}

func (et *EasyTier) Start(cfg *Config) error {
	dir, err := os.MkdirTemp("", "remote-assist-et-")
	if err != nil {
		return err
	}
	et.tmpDir = dir

	binName := "easytier-core"
	if runtime.GOOS == "windows" {
		binName = "easytier-core.exe"
	}
	binPath := filepath.Join(dir, binName)
	if err := os.WriteFile(binPath, embeddedEasyTier, 0755); err != nil {
		return err
	}

	args := []string{
		"--dhcp",
		"--network-name", cfg.NetworkName,
		"--network-secret", cfg.NetworkSecret,
	}
	for _, p := range cfg.Peers {
		args = append(args, "--peers", p)
	}

	et.cmd = exec.Command(binPath, args...)
	et.cmd.Dir = dir

	stdout, err := et.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	et.cmd.Stderr = et.cmd.Stdout

	if err := et.cmd.Start(); err != nil {
		return err
	}

	ipRe := regexp.MustCompile(`ipv4_addr=(\d+\.\d+\.\d+\.\d+)`)
	dhcpRe := regexp.MustCompile(`dhcp.*?(\d+\.\d+\.\d+\.\d+)`)

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			et.mu.Lock()
			et.logs = append(et.logs, line)
			et.mu.Unlock()

			if et.VirtIP == "" {
				if m := ipRe.FindStringSubmatch(line); len(m) > 1 {
					et.VirtIP = m[1]
					if et.onIP != nil {
						et.onIP(et.VirtIP)
					}
				} else if m := dhcpRe.FindStringSubmatch(line); len(m) > 1 {
					et.VirtIP = m[1]
					if et.onIP != nil {
						et.onIP(et.VirtIP)
					}
				}
			}
		}
	}()

	return nil
}

func (et *EasyTier) WaitForIP(timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if et.VirtIP != "" {
			return et.VirtIP, nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	// fallback: try to find EasyTier interface IP
	if ip := et.findEasyTierIP(); ip != "" {
		et.VirtIP = ip
		return ip, nil
	}

	return "", fmt.Errorf("timeout waiting for EasyTier virtual IP")
}

func (et *EasyTier) findEasyTierIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		name := strings.ToLower(iface.Name)
		if strings.Contains(name, "easytier") || strings.Contains(name, "tun") {
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
					return ipnet.IP.String()
				}
			}
		}
	}
	return ""
}

func (et *EasyTier) Logs() []string {
	et.mu.Lock()
	defer et.mu.Unlock()
	cp := make([]string, len(et.logs))
	copy(cp, et.logs)
	return cp
}

func (et *EasyTier) Stop() {
	if et.cmd != nil && et.cmd.Process != nil {
		et.cmd.Process.Kill()
		et.cmd.Wait()
	}
	if et.tmpDir != "" {
		os.RemoveAll(et.tmpDir)
	}
}
