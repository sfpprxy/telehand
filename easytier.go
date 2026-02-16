package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type EasyTier struct {
	cmd     *exec.Cmd
	tmpDir  string
	VirtIP  string
	rpcPort string
	cliBin  string
	mu      sync.Mutex
	logs    []string
	onLog   func(string)
}

func NewEasyTier(onLog func(string)) *EasyTier {
	return &EasyTier{onLog: onLog, rpcPort: "18899"}
}

func (et *EasyTier) Start(cfg *Config) error {
	dir, err := os.MkdirTemp("", "telehand-et-")
	if err != nil {
		return err
	}
	et.tmpDir = dir

	coreName := "easytier-core"
	cliName := "easytier-cli"
	if runtime.GOOS == "windows" {
		coreName = "easytier-core.exe"
		cliName = "easytier-cli.exe"
	}

	corePath := filepath.Join(dir, coreName)
	if err := os.WriteFile(corePath, embeddedEasyTier, 0755); err != nil {
		return err
	}

	et.cliBin = filepath.Join(dir, cliName)
	if err := os.WriteFile(et.cliBin, embeddedEasyTierCli, 0755); err != nil {
		return err
	}

	args := []string{
		"--dhcp",
		"--network-name", cfg.NetworkName,
		"--network-secret", cfg.NetworkSecret,
		"-l", "tcp://0.0.0.0:0",
		"-l", "udp://0.0.0.0:0",
		"-r", fmt.Sprintf("127.0.0.1:%s", et.rpcPort),
	}
	for _, p := range cfg.Peers {
		args = append(args, "--peers", p)
	}

	et.cmd = exec.Command(corePath, args...)
	et.cmd.Dir = dir

	stdout, err := et.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	et.cmd.Stderr = et.cmd.Stdout

	if err := et.cmd.Start(); err != nil {
		return err
	}

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			et.mu.Lock()
			et.logs = append(et.logs, line)
			et.mu.Unlock()
			if et.onLog != nil {
				et.onLog(line)
			}
		}
	}()

	return nil
}

type nodeInfo struct {
	IPv4Addr string `json:"ipv4_addr"`
}

func (et *EasyTier) WaitForIP(timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ip := et.queryIP()
		if ip != "" {
			et.VirtIP = ip
			return ip, nil
		}
		time.Sleep(2 * time.Second)
	}
	return "", fmt.Errorf("timeout waiting for EasyTier virtual IP")
}

func (et *EasyTier) queryIP() string {
	cmd := exec.Command(et.cliBin, "-p", fmt.Sprintf("127.0.0.1:%s", et.rpcPort), "-o", "json", "node", "info")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	var info nodeInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return ""
	}
	// ipv4_addr is like "10.126.126.2/24", strip the mask
	ip := strings.Split(info.IPv4Addr, "/")[0]
	if ip != "" && ip != "0.0.0.0" {
		return ip
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
