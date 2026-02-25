package main

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

const (
	DefaultPeerCount = 8
	MaxPeerCount     = 32
	InvalidPeerDrop  = true
)

var DefaultPeers = []string{
	"tcp://43.139.65.49:11010",
	"tcp://39.108.52.138:11010",
}

func parsePeers(peers string) []string {
	items := strings.Split(peers, ",")
	raw := make([]string, 0, len(items))
	for _, item := range items {
		raw = append(raw, strings.TrimSpace(item))
	}
	return normalizePeerPool(raw, MaxPeerCount, InvalidPeerDrop)
}

func defaultPeerPool() []string {
	limit := DefaultPeerCount
	if limit <= 0 {
		limit = MaxPeerCount
	}
	return normalizePeerPool(DefaultPeers, limit, InvalidPeerDrop)
}

func mergePeerPools(preferred, fallback []string, maxCount int) []string {
	combined := make([]string, 0, len(preferred)+len(fallback))
	combined = append(combined, preferred...)
	combined = append(combined, fallback...)
	return normalizePeerPool(combined, maxCount, InvalidPeerDrop)
}

func runtimePeerPool(configPeers []string) []string {
	return mergePeerPools(configPeers, defaultPeerPool(), MaxPeerCount)
}

func peerCSV(peers []string) string {
	return strings.Join(peers, ",")
}

func normalizePeerPool(raw []string, maxCount int, dropInvalid bool) []string {
	limit := maxCount
	if limit <= 0 || limit > MaxPeerCount {
		limit = MaxPeerCount
	}

	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		s := strings.TrimSpace(item)
		if s == "" {
			continue
		}
		normalized, ok := normalizePeerAddress(s)
		if !ok {
			if dropInvalid {
				continue
			}
			normalized = s
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func normalizePeerAddress(peer string) (string, bool) {
	value := strings.TrimSpace(peer)
	if value == "" {
		return "", false
	}
	u, err := url.Parse(value)
	if err != nil {
		return "", false
	}
	if u == nil || strings.TrimSpace(u.Scheme) == "" || strings.TrimSpace(u.Host) == "" {
		return "", false
	}
	scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
	if scheme != "tcp" && scheme != "udp" {
		return "", false
	}
	if strings.TrimSpace(u.Path) != "" && strings.TrimSpace(u.Path) != "/" {
		return "", false
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		return "", false
	}
	host = strings.TrimSpace(host)
	port = strings.TrimSpace(port)
	if host == "" || port == "" {
		return "", false
	}
	if _, err := net.LookupPort(scheme, port); err != nil {
		return "", false
	}
	return fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(host, port)), true
}

func maskPeerAddress(peer string) string {
	normalized, ok := normalizePeerAddress(peer)
	if !ok {
		return "***"
	}
	u, err := url.Parse(normalized)
	if err != nil {
		return "***"
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		return "***"
	}
	maskedHost := maskHostToken(host)
	return fmt.Sprintf("%s://%s", u.Scheme, net.JoinHostPort(maskedHost, port))
}

func maskHostToken(host string) string {
	ip := net.ParseIP(host)
	if ip4 := ip.To4(); ip4 != nil {
		return fmt.Sprintf("%d.%d.*.*", ip4[0], ip4[1])
	}
	value := strings.TrimSpace(host)
	if value == "" {
		return "***"
	}
	if len(value) <= 3 {
		return value[:1] + strings.Repeat("*", len(value)-1)
	}
	return value[:2] + strings.Repeat("*", len(value)-2)
}

