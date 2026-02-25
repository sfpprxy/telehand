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

type EasyTierStartOptions struct {
	IPv4CIDR string
	DevName  string
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

func (et *EasyTier) Start(cfg *Config, opts EasyTierStartOptions) error {
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
		"--network-name", cfg.NetworkName,
		"--network-secret", cfg.NetworkSecret,
		"-l", "tcp://0.0.0.0:0",
		"-l", "udp://0.0.0.0:0",
		"-r", fmt.Sprintf("127.0.0.1:%s", et.rpcPort),
	}
	if strings.TrimSpace(opts.IPv4CIDR) != "" {
		args = append(args, "--ipv4", strings.TrimSpace(opts.IPv4CIDR))
	} else {
		args = append(args, "--dhcp")
	}
	if strings.TrimSpace(opts.DevName) != "" {
		args = append(args, "--dev-name", strings.TrimSpace(opts.DevName))
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
	PeerID   any    `json:"peer_id"`
	IPv4Addr string `json:"ipv4_addr"`
	Hostname string `json:"hostname"`
	Version  string `json:"version"`
}

type rawPeerInfo struct {
	PeerID   string `json:"id"`
	IPv4     string `json:"ipv4"`
	Hostname string `json:"hostname"`
	Cost     string `json:"cost"`
	Protocol string `json:"tunnel_proto"`
	Latency  string `json:"lat_ms"`
	LossRate string `json:"loss_rate"`
	Download string `json:"rx_bytes"`
	Upload   string `json:"tx_bytes"`
	Version  string `json:"version"`
}

type PeerInfo struct {
	VirtualIPv4 string `json:"virtual_ipv4"`
	Hostname    string `json:"hostname"`
	RouteCost   string `json:"route_cost"`
	Protocol    string `json:"protocol"`
	Latency     string `json:"latency"`
	Upload      string `json:"upload"`
	Download    string `json:"download"`
	LossRate    string `json:"loss_rate"`
	Version     string `json:"version"`
	Role        string `json:"role"`
	IsSelf      bool   `json:"is_self"`
	PeerID      string `json:"peer_id,omitempty"`
}

type PeerInfoSnapshot struct {
	UpdatedAt    string     `json:"updated_at"`
	NetworkOwner string     `json:"network_owner,omitempty"`
	NetworkHash  string     `json:"network_hash,omitempty"`
	Peers        []PeerInfo `json:"peers"`
}

type PeerReadiness struct {
	Ready          bool
	TargetIP       string
	NonSelfPresent bool
	PeerClass      string
	PeerID         string
	PeerHostname   string
	PeerIDs        []string
}

const (
	peerClassNone                  = "none"
	peerClassBootstrapOnly         = "bootstrap_only"
	peerClassBusinessPeerWaitingIP = "business_peer_waiting_virtual_ip"
	peerClassEndpointReady         = "endpoint_ready"
)

func (et *EasyTier) WaitForIP(timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := et.detectFatalWaitError(); err != nil {
			return "", err
		}

		ip, err := et.queryIP()
		if ip != "" {
			et.VirtIP = ip
			return ip, nil
		}
		if err != nil && et.onLog != nil {
			et.onLog(fmt.Sprintf("[telehand] queryIP failed: %v", err))
		}

		if err := et.detectFatalWaitError(); err != nil {
			return "", err
		}
		time.Sleep(2 * time.Second)
	}

	if err := et.detectFatalWaitError(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("timeout waiting for EasyTier virtual IP")
}

func (et *EasyTier) detectFatalWaitError() error {
	code := classifyEasyTierError(nil, et.Logs(), "")
	if code == ErrorCodeTUNPermissionDenied {
		return newCodedError(
			ErrorCodeTUNPermissionDenied,
			"TUN permission denied (please run with administrator/root privilege)",
		)
	}
	return nil
}

func (et *EasyTier) queryIP() (string, error) {
	info, err := et.queryNodeInfo()
	if err != nil {
		return "", err
	}
	ip := stripCIDR(info.IPv4Addr)
	if ip != "" && ip != "0.0.0.0" {
		return ip, nil
	}
	return "", nil
}

func (et *EasyTier) queryNodeInfo() (*nodeInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultEasyTierCLIQueryTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, et.cliBin, "-p", fmt.Sprintf("127.0.0.1:%s", et.rpcPort), "-o", "json", "node", "info")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("easytier-cli timed out")
		}
		return nil, fmt.Errorf("easytier-cli error: %v, output=%s", err, strings.TrimSpace(string(out)))
	}
	var info nodeInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return nil, fmt.Errorf("invalid node info json: %v", err)
	}
	return &info, nil
}

func (et *EasyTier) QueryPeerInfo(role string) (PeerInfoSnapshot, error) {
	node, err := et.queryNodeInfo()
	if err != nil {
		return PeerInfoSnapshot{}, err
	}

	raw, err := et.queryRawPeerList()
	if err != nil {
		return PeerInfoSnapshot{}, err
	}

	peerRole := normalizeRoleLabel(role)

	selfID := peerIDToString(node.PeerID)
	selfIP := stripCIDR(node.IPv4Addr)
	peers := make([]PeerInfo, 0, len(raw))
	for _, p := range raw {
		virtualIP := strings.TrimSpace(p.IPv4)
		if virtualIP == "" && p.PeerID == selfID {
			virtualIP = selfIP
		}
		if virtualIP == "" && strings.EqualFold(strings.TrimSpace(p.Hostname), strings.TrimSpace(node.Hostname)) {
			virtualIP = selfIP
		}
		isSelf := p.PeerID != "" && p.PeerID == selfID
		peers = append(peers, PeerInfo{
			VirtualIPv4: valueOrDash(virtualIP),
			Hostname:    valueOrDash(p.Hostname),
			RouteCost:   valueOrDash(p.Cost),
			Protocol:    valueOrDash(p.Protocol),
			Latency:     valueOrDash(p.Latency),
			Upload:      valueOrDash(p.Upload),
			Download:    valueOrDash(p.Download),
			LossRate:    valueOrDash(p.LossRate),
			Version:     valueOrDash(p.Version),
			Role:        displayedRoleForPeer(isSelf, peerRole),
			IsSelf:      isSelf,
			PeerID:      p.PeerID,
		})
	}

	if len(peers) == 0 {
		peers = append(peers, PeerInfo{
			VirtualIPv4: valueOrDash(selfIP),
			Hostname:    valueOrDash(node.Hostname),
			RouteCost:   "Local",
			Protocol:    "-",
			Latency:     "-",
			Upload:      "-",
			Download:    "-",
			LossRate:    "-",
			Version:     valueOrDash(node.Version),
			Role:        peerRole,
			IsSelf:      true,
			PeerID:      selfID,
		})
	}

	return PeerInfoSnapshot{
		UpdatedAt: time.Now().Format(time.RFC3339),
		Peers:     peers,
	}, nil
}

func (et *EasyTier) queryRawPeerList() ([]rawPeerInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultEasyTierCLIQueryTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, et.cliBin, "-p", fmt.Sprintf("127.0.0.1:%s", et.rpcPort), "-o", "json", "peer", "list")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("easytier-cli peer list timed out")
		}
		return nil, fmt.Errorf("easytier-cli peer list error: %v, output=%s", err, strings.TrimSpace(string(out)))
	}
	var raw []rawPeerInfo
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("invalid peer list json: %v", err)
	}
	return raw, nil
}

func (et *EasyTier) QueryPeerReadiness() (PeerReadiness, error) {
	node, err := et.queryNodeInfo()
	if err != nil {
		return PeerReadiness{}, err
	}
	raw, err := et.queryRawPeerList()
	if err != nil {
		return PeerReadiness{}, err
	}
	return classifyPeerReadiness(node, raw), nil
}

func classifyPeerReadiness(node *nodeInfo, raw []rawPeerInfo) PeerReadiness {
	if node == nil {
		return PeerReadiness{Ready: false, NonSelfPresent: false, PeerClass: peerClassNone}
	}
	selfID := peerIDToString(node.PeerID)
	selfIP := stripCIDR(node.IPv4Addr)
	nonSelfPresent := false
	bootstrapPeerID := ""
	bootstrapPeerHost := ""
	peerIDs := make([]string, 0, len(raw))
	for _, p := range raw {
		peerID := strings.TrimSpace(p.PeerID)
		ip := stripCIDR(p.IPv4)
		hostname := strings.TrimSpace(p.Hostname)
		if peerID != "" && selfID != "" && peerID == selfID {
			continue
		}
		if peerID != "" {
			peerIDs = append(peerIDs, peerID)
		}
		if peerID != "" || hostname != "" {
			if hostname == "" || !strings.EqualFold(hostname, strings.TrimSpace(node.Hostname)) {
				nonSelfPresent = true
			}
		}
		if ip == "" || ip == "0.0.0.0" || ip == selfIP {
			if isBootstrapPeerHost(hostname) {
				bootstrapPeerID = peerID
				bootstrapPeerHost = hostname
			}
			continue
		}
		return PeerReadiness{
			Ready:          true,
			TargetIP:       ip,
			NonSelfPresent: true,
			PeerClass:      peerClassEndpointReady,
			PeerID:         peerID,
			PeerHostname:   hostname,
			PeerIDs:        peerIDs,
		}
	}
	if bootstrapPeerID != "" || bootstrapPeerHost != "" {
		return PeerReadiness{
			Ready:          false,
			NonSelfPresent: true,
			PeerClass:      peerClassBootstrapOnly,
			PeerID:         bootstrapPeerID,
			PeerHostname:   bootstrapPeerHost,
			PeerIDs:        peerIDs,
		}
	}
	if nonSelfPresent {
		return PeerReadiness{
			Ready:          false,
			NonSelfPresent: true,
			PeerClass:      peerClassBusinessPeerWaitingIP,
			PeerIDs:        peerIDs,
		}
	}
	return PeerReadiness{Ready: false, NonSelfPresent: false, PeerClass: peerClassNone, PeerIDs: peerIDs}
}

type EasyTierSnapshot struct {
	At        time.Time
	Node      *nodeInfo
	Peers     []rawPeerInfo
	Readiness PeerReadiness
}

func (et *EasyTier) QuerySnapshot() (EasyTierSnapshot, error) {
	node, err := et.queryNodeInfo()
	if err != nil {
		return EasyTierSnapshot{}, err
	}
	peers, err := et.queryRawPeerList()
	if err != nil {
		return EasyTierSnapshot{}, err
	}
	return EasyTierSnapshot{
		At:        time.Now(),
		Node:      node,
		Peers:     peers,
		Readiness: classifyPeerReadiness(node, peers),
	}, nil
}

func isBootstrapPeerHost(hostname string) bool {
	name := strings.TrimSpace(hostname)
	if name == "" {
		return false
	}
	return strings.HasPrefix(name, "PublicServer_")
}

func valueOrDash(v string) string {
	s := strings.TrimSpace(v)
	if s == "" {
		return "-"
	}
	return s
}

func stripCIDR(v string) string {
	return strings.Split(strings.TrimSpace(v), "/")[0]
}

func peerIDToString(v any) string {
	switch t := v.(type) {
	case float64:
		return fmt.Sprintf("%.0f", t)
	case string:
		return strings.TrimSpace(t)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", t))
	}
}

func normalizeRoleLabel(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "client":
		return "Client"
	case "server":
		return "Server"
	default:
		if strings.TrimSpace(role) == "" {
			return "Unknown"
		}
		return strings.TrimSpace(role)
	}
}

func displayedRoleForPeer(isSelf bool, sessionRole string) string {
	if !isSelf {
		return "-"
	}
	return normalizeRoleLabel(sessionRole)
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
