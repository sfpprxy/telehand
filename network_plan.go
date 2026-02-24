package main

import (
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"sort"
	"strings"
)

const (
	defaultSubnetCandidateCount = 16
)

type IPv4Candidate struct {
	LocalCIDR      string
	LocalIP        string
	SubnetCIDR     string
	ExpectedPeerIP string
}

type SessionBaseline struct {
	TunDevice   string
	VirtualCIDR string
	NetworkHash string
}

func computeNetworkHash(networkName, networkSecret string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(networkName) + "\n" + strings.TrimSpace(networkSecret)))
	encoded := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:]))
	if len(encoded) < 10 {
		return encoded
	}
	return encoded[:10]
}

func buildIPv4Candidates(networkHash, role string, count int) []IPv4Candidate {
	if count <= 0 {
		count = defaultSubnetCandidateCount
	}

	hostLocal := 2
	hostPeer := 1
	if strings.EqualFold(strings.TrimSpace(role), "client") {
		hostLocal = 1
		hostPeer = 2
	}

	sum := sha256.Sum256([]byte(strings.TrimSpace(networkHash)))
	seed := int(sum[0])<<8 | int(sum[1])

	out := make([]IPv4Candidate, 0, count)
	seen := make(map[string]struct{}, count)
	for i := 0; len(out) < count && i < 65536; i++ {
		idx := (seed + i) % 65536
		oct2 := idx / 256
		oct3 := idx % 256

		subnet := fmt.Sprintf("10.%d.%d.0/24", oct2, oct3)
		if _, exists := seen[subnet]; exists {
			continue
		}
		seen[subnet] = struct{}{}

		localIP := fmt.Sprintf("10.%d.%d.%d", oct2, oct3, hostLocal)
		peerIP := fmt.Sprintf("10.%d.%d.%d", oct2, oct3, hostPeer)
		out = append(out, IPv4Candidate{
			LocalCIDR:      fmt.Sprintf("%s/24", localIP),
			LocalIP:        localIP,
			SubnetCIDR:     subnet,
			ExpectedPeerIP: peerIP,
		})
	}
	return out
}

func collectLocalIPv4Nets() ([]*net.IPNet, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	out := make([]*net.IPNet, 0, 16)
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet == nil {
				continue
			}
			v4 := ipNet.IP.To4()
			if v4 == nil {
				continue
			}
			masked := v4.Mask(ipNet.Mask)
			ipCopy := make(net.IP, len(masked))
			copy(ipCopy, masked)
			maskCopy := make(net.IPMask, len(ipNet.Mask))
			copy(maskCopy, ipNet.Mask)
			out = append(out, &net.IPNet{IP: ipCopy, Mask: maskCopy})
		}
	}
	return out, nil
}

func filterNonConflictingCandidates(candidates []IPv4Candidate, usedNets []*net.IPNet) []IPv4Candidate {
	if len(candidates) == 0 {
		return nil
	}
	if len(usedNets) == 0 {
		return candidates
	}

	out := make([]IPv4Candidate, 0, len(candidates))
	for _, c := range candidates {
		_, candidateNet, err := net.ParseCIDR(c.SubnetCIDR)
		if err != nil {
			continue
		}
		if !overlapsAny(candidateNet, usedNets) {
			out = append(out, c)
		}
	}
	return out
}

func overlapsAny(candidate *net.IPNet, others []*net.IPNet) bool {
	for _, other := range others {
		if other == nil {
			continue
		}
		if candidate.Contains(other.IP) || other.Contains(candidate.IP) {
			return true
		}
	}
	return false
}

func routeInterfaceForTarget(targetIP string) (string, error) {
	target := strings.TrimSpace(targetIP)
	if net.ParseIP(target) == nil {
		return "", fmt.Errorf("invalid target ip: %q", targetIP)
	}

	switch runtime.GOOS {
	case "darwin":
		cmd := exec.Command("route", "-n", "get", target)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("route get failed: %v (%s)", err, strings.TrimSpace(string(out)))
		}
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "interface:") {
				return strings.TrimSpace(strings.TrimPrefix(line, "interface:")), nil
			}
		}
		return "", fmt.Errorf("route interface not found")
	case "windows":
		script := fmt.Sprintf("$r=Get-NetRoute -DestinationPrefix '%s/32' -ErrorAction SilentlyContinue | Sort-Object -Property RouteMetric,ifMetric | Select-Object -First 1 -ExpandProperty InterfaceAlias; if($null -ne $r){$r}", target)
		cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("Get-NetRoute failed: %v (%s)", err, strings.TrimSpace(string(out)))
		}
		iface := strings.TrimSpace(string(out))
		if iface == "" {
			return "", fmt.Errorf("route interface not found")
		}
		return iface, nil
	default:
		return "", fmt.Errorf("route interface check unsupported on %s", runtime.GOOS)
	}
}

func interfaceByIPv4(ipv4 string) (string, error) {
	target := net.ParseIP(strings.TrimSpace(ipv4)).To4()
	if target == nil {
		return "", fmt.Errorf("invalid ipv4: %q", ipv4)
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			n, ok := addr.(*net.IPNet)
			if !ok || n == nil {
				continue
			}
			if n.IP.To4() != nil && n.IP.Equal(target) {
				return iface.Name, nil
			}
		}
	}
	return "", fmt.Errorf("no interface owns ip %s", ipv4)
}

func chooseCandidates(networkHash, role string, usedNets []*net.IPNet) []IPv4Candidate {
	candidates := buildIPv4Candidates(networkHash, role, defaultSubnetCandidateCount)
	filtered := filterNonConflictingCandidates(candidates, usedNets)
	if len(filtered) > 0 {
		return filtered
	}
	return candidates
}

func normalizeUsedNets(nets []*net.IPNet) []string {
	out := make([]string, 0, len(nets))
	for _, n := range nets {
		if n == nil {
			continue
		}
		out = append(out, n.String())
	}
	sort.Strings(out)
	return out
}
