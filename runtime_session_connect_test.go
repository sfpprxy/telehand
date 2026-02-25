package main

import (
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func resetConnectFallbackHooks(t *testing.T) {
	t.Helper()
	origCollect := collectLocalIPv4NetsFn
	origChoose := chooseCandidatesFn
	origRank := rankPeersByLatencyFn
	origNewET := newEasyTierFn
	origStart := easyTierStartFn
	origWaitIP := easyTierWaitForIPFn
	origIface := interfaceByIPv4Fn
	origEvaluate := evaluateCandidateConnectivityFn
	origBootstrapWaitTimeout := bootstrapWaitTimeout
	t.Cleanup(func() {
		collectLocalIPv4NetsFn = origCollect
		chooseCandidatesFn = origChoose
		rankPeersByLatencyFn = origRank
		newEasyTierFn = origNewET
		easyTierStartFn = origStart
		easyTierWaitForIPFn = origWaitIP
		interfaceByIPv4Fn = origIface
		evaluateCandidateConnectivityFn = origEvaluate
		bootstrapWaitTimeout = origBootstrapWaitTimeout
	})
}

func TestConnectWithPeerFallbackFirstPeerFailsThenSecondSucceeds(t *testing.T) {
	resetConnectFallbackHooks(t)

	firstPeer := "tcp://1.1.1.1:11010"
	secondPeer := "tcp://2.2.2.2:11010"
	cfg := &Config{
		NetworkName:   "n",
		NetworkSecret: "s",
		Peers:         []string{firstPeer, secondPeer},
	}
	gui := NewGUIServer(18080)

	collectLocalIPv4NetsFn = func() ([]*net.IPNet, error) { return nil, nil }
	chooseCandidatesFn = func(networkHash, role string, usedNets []*net.IPNet) []IPv4Candidate {
		return []IPv4Candidate{{LocalCIDR: "10.10.10.1/24", SubnetCIDR: "10.10.10.0/24"}}
	}
	rankPeersByLatencyFn = func(peers []string) PeerSelection {
		return PeerSelection{
			Ordered: []string{firstPeer, secondPeer},
			Results: []PeerProbeResult{
				{Peer: firstPeer, Latency: 10 * time.Millisecond, Reachable: true},
				{Peer: secondPeer, Latency: 20 * time.Millisecond, Reachable: true},
			},
		}
	}
	newEasyTierFn = func(onLog func(string)) *EasyTier { return &EasyTier{} }

	var (
		currentHead string
		startPeers  []string
	)
	easyTierStartFn = func(et *EasyTier, startCfg *Config, opts EasyTierStartOptions) error {
		if len(startCfg.Peers) == 0 {
			t.Fatal("start config peers should not be empty")
		}
		currentHead = startCfg.Peers[0]
		startPeers = append(startPeers, strings.Join(startCfg.Peers, ","))
		if currentHead == firstPeer {
			return errors.New("start failed")
		}
		return nil
	}
	easyTierWaitForIPFn = func(et *EasyTier, timeout time.Duration) (string, error) {
		return "10.10.10.1", nil
	}
	interfaceByIPv4Fn = func(ipv4 string) (string, error) { return "utun9", nil }
	evaluateCandidateConnectivityFn = func(et *EasyTier, tunDevice string, apiPort int, checkCfg candidateCheckConfig, deps sessionDeps, stop <-chan struct{}, logFn func(result, reason, detail string)) candidateCheckResult {
		if currentHead == secondPeer {
			return candidateCheckResult{peerReady: true, probeSuccess: true, targetIP: "10.10.10.2"}
		}
		return candidateCheckResult{peerReady: false, lastProbeErr: errors.New("probe failed")}
	}

	result, code, err := connectWithPeerFallback(gui, true, cfg, "client", "hash", 8080, defaultCandidateCheckConfig, defaultSessionDeps, nil, "", nil)
	if err != nil || code != "" {
		t.Fatalf("connectWithPeerFallback should succeed, code=%q err=%v", code, err)
	}
	if result.selectedPeer != secondPeer {
		t.Fatalf("unexpected selected peer: got=%s want=%s", result.selectedPeer, secondPeer)
	}
	if len(startPeers) != 2 {
		t.Fatalf("expected 2 start attempts, got=%d (%v)", len(startPeers), startPeers)
	}
	if startPeers[0] != firstPeer {
		t.Fatalf("unexpected first start peer: %s", startPeers[0])
	}
	if startPeers[1] != secondPeer {
		t.Fatalf("unexpected second start peer: %s", startPeers[1])
	}
}

func TestConnectWithPeerFallbackAllPeersFail(t *testing.T) {
	resetConnectFallbackHooks(t)

	peerA := "tcp://1.1.1.1:11010"
	peerB := "tcp://2.2.2.2:11010"
	cfg := &Config{NetworkName: "n", NetworkSecret: "s", Peers: []string{peerA, peerB}}
	gui := NewGUIServer(18080)

	collectLocalIPv4NetsFn = func() ([]*net.IPNet, error) { return nil, nil }
	chooseCandidatesFn = func(networkHash, role string, usedNets []*net.IPNet) []IPv4Candidate {
		return []IPv4Candidate{{LocalCIDR: "10.10.20.1/24", SubnetCIDR: "10.10.20.0/24"}}
	}
	rankPeersByLatencyFn = func(peers []string) PeerSelection {
		return PeerSelection{Ordered: []string{peerA, peerB}, Results: []PeerProbeResult{{Peer: peerA, Reachable: true}, {Peer: peerB, Reachable: true}}}
	}
	newEasyTierFn = func(onLog func(string)) *EasyTier { return &EasyTier{} }
	easyTierStartFn = func(et *EasyTier, startCfg *Config, opts EasyTierStartOptions) error { return nil }
	easyTierWaitForIPFn = func(et *EasyTier, timeout time.Duration) (string, error) { return "10.10.20.1", nil }
	interfaceByIPv4Fn = func(ipv4 string) (string, error) { return "utun8", nil }
	evaluateCandidateConnectivityFn = func(et *EasyTier, tunDevice string, apiPort int, checkCfg candidateCheckConfig, deps sessionDeps, stop <-chan struct{}, logFn func(result, reason, detail string)) candidateCheckResult {
		return candidateCheckResult{peerReady: false, lastProbeErr: errors.New("probe timeout")}
	}

	result, code, err := connectWithPeerFallback(gui, true, cfg, "client", "hash", 8080, defaultCandidateCheckConfig, defaultSessionDeps, nil, "", nil)
	if err == nil {
		t.Fatal("expected connectWithPeerFallback to fail")
	}
	if code != ErrorCodePeerUnreachable {
		t.Fatalf("unexpected error code: got=%s want=%s", code, ErrorCodePeerUnreachable)
	}
	if result.selectedPeer != "" {
		t.Fatalf("expected no selected peer on failure, got=%s", result.selectedPeer)
	}
}

func TestConnectWithPeerFallbackRouteConflictSwitchesSubnet(t *testing.T) {
	resetConnectFallbackHooks(t)

	peer := "tcp://1.1.1.1:11010"
	cfg := &Config{NetworkName: "n", NetworkSecret: "s", Peers: []string{peer}}
	gui := NewGUIServer(18080)

	collectLocalIPv4NetsFn = func() ([]*net.IPNet, error) { return nil, nil }
	chooseCandidatesFn = func(networkHash, role string, usedNets []*net.IPNet) []IPv4Candidate {
		return []IPv4Candidate{
			{LocalCIDR: "10.30.0.1/24", SubnetCIDR: "10.30.0.0/24"},
			{LocalCIDR: "10.31.0.1/24", SubnetCIDR: "10.31.0.0/24"},
		}
	}
	rankPeersByLatencyFn = func(peers []string) PeerSelection {
		return PeerSelection{Ordered: []string{peer}, Results: []PeerProbeResult{{Peer: peer, Reachable: true, Latency: time.Millisecond}}}
	}
	newEasyTierFn = func(onLog func(string)) *EasyTier { return &EasyTier{} }

	var currentCIDR string
	easyTierStartFn = func(et *EasyTier, startCfg *Config, opts EasyTierStartOptions) error {
		currentCIDR = opts.IPv4CIDR
		return nil
	}
	easyTierWaitForIPFn = func(et *EasyTier, timeout time.Duration) (string, error) {
		if strings.HasPrefix(currentCIDR, "10.30.") {
			return "10.30.0.1", nil
		}
		return "10.31.0.1", nil
	}
	interfaceByIPv4Fn = func(ipv4 string) (string, error) { return "utun7", nil }
	evaluateCandidateConnectivityFn = func(et *EasyTier, tunDevice string, apiPort int, checkCfg candidateCheckConfig, deps sessionDeps, stop <-chan struct{}, logFn func(result, reason, detail string)) candidateCheckResult {
		if strings.HasPrefix(currentCIDR, "10.30.") {
			return candidateCheckResult{routeMismatchDetail: "target=10.30.0.2 route_if=en0 tun_if=utun7", peerQueryFailures: checkCfg.maxChecks}
		}
		return candidateCheckResult{peerReady: true, probeSuccess: true, targetIP: "10.31.0.2"}
	}

	result, code, err := connectWithPeerFallback(gui, true, cfg, "client", "hash", 8080, defaultCandidateCheckConfig, defaultSessionDeps, nil, "", nil)
	if err != nil || code != "" {
		t.Fatalf("expected conflict fallback to second subnet success, code=%q err=%v", code, err)
	}
	if result.baseline.VirtualCIDR != "10.31.0.0/24" {
		t.Fatalf("unexpected final subnet: %s", result.baseline.VirtualCIDR)
	}
}

func TestConnectWithPeerFallbackKeepsCurrentPeerWhenBootstrapWaiting(t *testing.T) {
	resetConnectFallbackHooks(t)

	firstPeer := "tcp://1.1.1.1:11010"
	secondPeer := "tcp://2.2.2.2:11010"
	cfg := &Config{NetworkName: "n", NetworkSecret: "s", Peers: []string{firstPeer, secondPeer}}
	gui := NewGUIServer(18080)

	collectLocalIPv4NetsFn = func() ([]*net.IPNet, error) { return nil, nil }
	chooseCandidatesFn = func(networkHash, role string, usedNets []*net.IPNet) []IPv4Candidate {
		return []IPv4Candidate{{LocalCIDR: "10.50.0.1/24", SubnetCIDR: "10.50.0.0/24"}}
	}
	rankPeersByLatencyFn = func(peers []string) PeerSelection {
		return PeerSelection{
			Ordered: []string{firstPeer, secondPeer},
			Results: []PeerProbeResult{
				{Peer: firstPeer, Latency: 5 * time.Millisecond, Reachable: true},
				{Peer: secondPeer, Latency: 8 * time.Millisecond, Reachable: true},
			},
		}
	}
	newEasyTierFn = func(onLog func(string)) *EasyTier { return &EasyTier{} }

	var startPeers []string
	easyTierStartFn = func(et *EasyTier, startCfg *Config, opts EasyTierStartOptions) error {
		startPeers = append(startPeers, strings.Join(startCfg.Peers, ","))
		return nil
	}
	easyTierWaitForIPFn = func(et *EasyTier, timeout time.Duration) (string, error) { return "10.50.0.1", nil }
	interfaceByIPv4Fn = func(ipv4 string) (string, error) { return "utun9", nil }

	evalCalls := 0
	evaluateCandidateConnectivityFn = func(et *EasyTier, tunDevice string, apiPort int, checkCfg candidateCheckConfig, deps sessionDeps, stop <-chan struct{}, logFn func(result, reason, detail string)) candidateCheckResult {
		evalCalls++
		if evalCalls == 1 {
			return candidateCheckResult{
				peerReady:         false,
				nonSelfPresent:    true,
				peerClass:         peerClassBootstrapOnly,
				peerQueryFailures: 0,
			}
		}
		return candidateCheckResult{
			peerReady:    true,
			probeSuccess: true,
			targetIP:     "10.50.0.2",
		}
	}

	checkCfg := defaultCandidateCheckConfig
	checkCfg.pollInterval = time.Millisecond

	result, code, err := connectWithPeerFallback(gui, true, cfg, "client", "hash", 8080, checkCfg, defaultSessionDeps, nil, "", nil)
	if err != nil || code != "" {
		t.Fatalf("connectWithPeerFallback should succeed, code=%q err=%v", code, err)
	}
	if result.selectedPeer != firstPeer {
		t.Fatalf("unexpected selected peer: got=%s want=%s", result.selectedPeer, firstPeer)
	}
	if len(startPeers) != 1 || startPeers[0] != firstPeer {
		t.Fatalf("expected single-peer start on first peer, got=%v", startPeers)
	}
	if evalCalls < 2 {
		t.Fatalf("expected repeated evaluation on same peer, got evalCalls=%d", evalCalls)
	}
}

func TestConnectWithPeerFallbackInterrupted(t *testing.T) {
	cfg := &Config{
		NetworkName:   "n",
		NetworkSecret: "s",
		Peers:         []string{"tcp://1.1.1.1:11010"},
	}
	gui := NewGUIServer(18080)
	stop := make(chan struct{})
	close(stop)

	_, code, err := connectWithPeerFallback(gui, true, cfg, "client", "hash", 8080, defaultCandidateCheckConfig, defaultSessionDeps, nil, "", stop)
	if !errors.Is(err, errSessionInterrupted) {
		t.Fatalf("expected interrupted error, got code=%q err=%v", code, err)
	}
	if code != "" {
		t.Fatalf("expected empty error code on interruption, got %q", code)
	}
}

func TestConnectWithPeerFallbackBootstrapWaitTimeoutSwitchesPeer(t *testing.T) {
	resetConnectFallbackHooks(t)

	firstPeer := "tcp://1.1.1.1:11010"
	secondPeer := "tcp://2.2.2.2:11010"
	cfg := &Config{NetworkName: "n", NetworkSecret: "s", Peers: []string{firstPeer, secondPeer}}
	gui := NewGUIServer(18080)

	collectLocalIPv4NetsFn = func() ([]*net.IPNet, error) { return nil, nil }
	chooseCandidatesFn = func(networkHash, role string, usedNets []*net.IPNet) []IPv4Candidate {
		return []IPv4Candidate{{LocalCIDR: "10.60.0.1/24", SubnetCIDR: "10.60.0.0/24"}}
	}
	rankPeersByLatencyFn = func(peers []string) PeerSelection {
		return PeerSelection{
			Ordered: []string{firstPeer, secondPeer},
			Results: []PeerProbeResult{
				{Peer: firstPeer, Latency: 5 * time.Millisecond, Reachable: true},
				{Peer: secondPeer, Latency: 6 * time.Millisecond, Reachable: true},
			},
		}
	}
	newEasyTierFn = func(onLog func(string)) *EasyTier { return &EasyTier{} }

	var startPeers []string
	easyTierStartFn = func(et *EasyTier, startCfg *Config, opts EasyTierStartOptions) error {
		startPeers = append(startPeers, strings.Join(startCfg.Peers, ","))
		return nil
	}
	easyTierWaitForIPFn = func(et *EasyTier, timeout time.Duration) (string, error) { return "10.60.0.1", nil }
	interfaceByIPv4Fn = func(ipv4 string) (string, error) { return "utun9", nil }
	evaluateCandidateConnectivityFn = func(et *EasyTier, tunDevice string, apiPort int, checkCfg candidateCheckConfig, deps sessionDeps, stop <-chan struct{}, logFn func(result, reason, detail string)) candidateCheckResult {
		return candidateCheckResult{
			peerReady:         false,
			nonSelfPresent:    true,
			peerClass:         peerClassBootstrapOnly,
			peerQueryFailures: 0,
		}
	}

	bootstrapWaitTimeout = 3 * time.Millisecond
	checkCfg := defaultCandidateCheckConfig
	checkCfg.pollInterval = time.Millisecond

	_, code, err := connectWithPeerFallback(gui, true, cfg, "client", "hash", 8080, checkCfg, defaultSessionDeps, nil, "", nil)
	if err == nil {
		t.Fatalf("expected fallback round to fail eventually, got code=%q", code)
	}
	if len(startPeers) < 2 {
		t.Fatalf("expected to switch to next peer after bootstrap wait timeout, startPeers=%v", startPeers)
	}
	if startPeers[0] != firstPeer || startPeers[1] != secondPeer {
		t.Fatalf("unexpected peer switch order after timeout, startPeers=%v", startPeers[:2])
	}
}
