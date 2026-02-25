package main

import (
	"net"
	"strings"
	"testing"
)

func TestComputeNetworkHashStable(t *testing.T) {
	h1 := computeNetworkHash("telehand:abc", "telehand:abc1234")
	h2 := computeNetworkHash("telehand:abc", "telehand:abc1234")
	h3 := computeNetworkHash("telehand:abc", "telehand:abc5678")

	if h1 != h2 {
		t.Fatalf("hash should be stable: %q != %q", h1, h2)
	}
	if h1 == h3 {
		t.Fatalf("hash should differ for different secret")
	}
	if len(h1) != 10 {
		t.Fatalf("hash length should be 10, got %d", len(h1))
	}
	if strings.ToLower(h1) != h1 {
		t.Fatalf("hash should be lowercase: %q", h1)
	}
}

func TestBuildIPv4CandidatesByRole(t *testing.T) {
	client := buildIPv4Candidates("aaaaaaaaaa", "client", 2)
	server := buildIPv4Candidates("aaaaaaaaaa", "server", 2)
	if len(client) < 1 || len(server) < 1 {
		t.Fatalf("expected non-empty candidates")
	}
	if !strings.HasSuffix(client[0].LocalCIDR, ".1/24") {
		t.Fatalf("client local cidr should end with .1/24, got %q", client[0].LocalCIDR)
	}
	if !strings.HasSuffix(server[0].LocalCIDR, ".2/24") {
		t.Fatalf("server local cidr should end with .2/24, got %q", server[0].LocalCIDR)
	}
	if !strings.HasSuffix(client[0].ExpectedPeerIP, ".2") {
		t.Fatalf("client expected peer ip should end with .2, got %q", client[0].ExpectedPeerIP)
	}
	if !strings.HasSuffix(server[0].ExpectedPeerIP, ".1") {
		t.Fatalf("server expected peer ip should end with .1, got %q", server[0].ExpectedPeerIP)
	}
}

func TestFilterNonConflictingCandidates(t *testing.T) {
	candidates := []IPv4Candidate{
		{SubnetCIDR: "10.1.2.0/24", LocalCIDR: "10.1.2.1/24"},
		{SubnetCIDR: "10.3.4.0/24", LocalCIDR: "10.3.4.1/24"},
	}
	_, used1, _ := net.ParseCIDR("10.1.0.0/16")
	_, used2, _ := net.ParseCIDR("192.168.1.0/24")
	filtered := filterNonConflictingCandidates(candidates, []*net.IPNet{used1, used2})
	if len(filtered) != 1 {
		t.Fatalf("expected 1 non-conflicting candidate, got %d", len(filtered))
	}
	if filtered[0].SubnetCIDR != "10.3.4.0/24" {
		t.Fatalf("unexpected candidate kept: %+v", filtered[0])
	}
}

func TestParseRouteNetsFromLines(t *testing.T) {
	text := `
Destination        Gateway            Flags
default            192.168.1.1        UGSc
10.206.246.0/24    10.0.0.1           UGSc
10.8.0.1           link#6             UHWI
`
	nets := parseRouteNetsFromLines(text)
	got := normalizeUsedNets(nets)
	if len(got) == 0 {
		t.Fatalf("expected parsed route nets, got empty")
	}
	hasSubnet := false
	hasHost := false
	for _, n := range got {
		if n == "10.206.246.0/24" {
			hasSubnet = true
		}
		if n == "10.8.0.1/32" {
			hasHost = true
		}
	}
	if !hasSubnet || !hasHost {
		t.Fatalf("missing expected nets, got %v", got)
	}
}
