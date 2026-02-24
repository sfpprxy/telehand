package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
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
	return &EasyTier{onLog: onLog, rpcPort: allocateRPCPort()}
}

func allocateRPCPort() string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "18899"
	}
	defer ln.Close()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil || port == "" {
		return "18899"
	}
	return port
}

func ensureWindowsRuntimeDLLs(dir string) error {
	if runtime.GOOS != "windows" {
		return nil
	}

	deps := []struct {
		name string
		data []byte
	}{
		{name: "Packet.dll", data: embeddedPacketDLL},
		{name: "wintun.dll", data: embeddedWintunDLL},
	}

	for _, dep := range deps {
		if len(dep.data) == 0 {
			return fmt.Errorf("%s embedded payload is empty", dep.name)
		}
		dst := filepath.Join(dir, dep.name)
		if err := os.WriteFile(dst, dep.data, 0644); err != nil {
			return fmt.Errorf("write %s failed: %w", dep.name, err)
		}
	}
	return nil
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

	if err := ensureWindowsRuntimeDLLs(dir); err != nil {
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
	if et.onLog != nil {
		et.onLog(fmt.Sprintf("[telehand] easytier rpc=%s", et.rpcPort))
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
		ip, err := et.queryIP()
		if ip != "" {
			et.VirtIP = ip
			return ip, nil
		}
		if err != nil && et.onLog != nil {
			et.onLog(fmt.Sprintf("[telehand] queryIP failed: %v", err))
		}
		time.Sleep(2 * time.Second)
	}
	return "", fmt.Errorf("timeout waiting for EasyTier virtual IP")
}

func (et *EasyTier) queryIP() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, et.cliBin, "-p", fmt.Sprintf("127.0.0.1:%s", et.rpcPort), "-o", "json", "node", "info")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("easytier-cli timed out")
		}
		return "", fmt.Errorf("easytier-cli error: %v, output=%s", err, strings.TrimSpace(string(out)))
	}
	var info nodeInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return "", fmt.Errorf("invalid node info json: %v", err)
	}
	// ipv4_addr is like "10.126.126.2/24", strip the mask
	ip := strings.Split(info.IPv4Addr, "/")[0]
	if ip != "" && ip != "0.0.0.0" {
		return ip, nil
	}
	return "", nil
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
