package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestEvaluateCandidateConnectivityRouteMismatchButReachable(t *testing.T) {
	var logs []string
	cfg := candidateCheckConfig{
		maxChecks:    3,
		pollInterval: time.Millisecond,
		probeTimout:  5 * time.Millisecond,
	}
	deps := sessionDeps{
		queryPeerReadiness: func(*EasyTier) (PeerReadiness, error) {
			return PeerReadiness{Ready: true, TargetIP: "10.77.0.2"}, nil
		},
		routeInterfaceForTarget: func(string) (string, error) { return "en0", nil },
		probePeerVirtualIP:      func(string, int, time.Duration) error { return nil },
		shouldCheckRouteOwner:   func() bool { return true },
	}

	got := evaluateCandidateConnectivity(nil, "utun9", 8080, cfg, deps, nil, func(result, reason, detail string) {
		logs = append(logs, result+":"+reason+":"+detail)
	})

	if !got.peerReady || !got.probeSuccess {
		t.Fatalf("expected peer ready and probe success, got %+v", got)
	}
	if got.routeMismatchDetail == "" {
		t.Fatalf("expected route mismatch detail to be recorded")
	}
	if got.peerQueryFailures != 0 {
		t.Fatalf("expected no peer query failures, got %d", got.peerQueryFailures)
	}

	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "warn:route_mismatch:") {
		t.Fatalf("expected route_mismatch warning in logs, got %q", joined)
	}
	if !strings.Contains(joined, "pass:peer_ready:") {
		t.Fatalf("expected peer_ready pass in logs, got %q", joined)
	}
}

func TestEvaluateCandidateConnectivityExplicitConflictEvidence(t *testing.T) {
	var calls int
	cfg := candidateCheckConfig{
		maxChecks:    3,
		pollInterval: 5 * time.Millisecond,
		probeTimout:  5 * time.Millisecond,
	}
	deps := sessionDeps{
		queryPeerReadiness: func(*EasyTier) (PeerReadiness, error) {
			calls++
			if calls == 1 {
				return PeerReadiness{Ready: true, TargetIP: "10.88.0.2"}, nil
			}
			return PeerReadiness{}, errors.New("peer list query failed")
		},
		routeInterfaceForTarget: func(string) (string, error) { return "en0", nil },
		probePeerVirtualIP:      func(string, int, time.Duration) error { return errors.New("dial timeout") },
		shouldCheckRouteOwner:   func() bool { return true },
	}

	got := evaluateCandidateConnectivity(nil, "utun8", 8080, cfg, deps, nil, func(string, string, string) {})

	if got.routeMismatchDetail == "" {
		t.Fatalf("expected route mismatch detail, got %+v", got)
	}
	if got.peerQueryFailures < cfg.maxChecks-1 {
		t.Fatalf("expected peer query failures >= %d, got %d", cfg.maxChecks-1, got.peerQueryFailures)
	}
	if got.probeSuccess {
		t.Fatalf("expected no probe success, got %+v", got)
	}
}

func TestEvaluateCandidateConnectivityRetryExhausted(t *testing.T) {
	var logs []string
	cfg := candidateCheckConfig{
		maxChecks:    3,
		pollInterval: 5 * time.Millisecond,
		probeTimout:  5 * time.Millisecond,
	}
	deps := sessionDeps{
		queryPeerReadiness: func(*EasyTier) (PeerReadiness, error) {
			return PeerReadiness{Ready: false}, nil
		},
		routeInterfaceForTarget: func(string) (string, error) { return "", nil },
		probePeerVirtualIP:      func(string, int, time.Duration) error { return nil },
		shouldCheckRouteOwner:   func() bool { return true },
	}

	got := evaluateCandidateConnectivity(nil, "utun1", 8080, cfg, deps, nil, func(result, reason, detail string) {
		logs = append(logs, result+":"+reason+":"+detail)
	})

	if got.peerReady {
		t.Fatalf("expected peer not ready, got %+v", got)
	}
	if got.probeSuccess {
		t.Fatalf("expected probe not successful, got %+v", got)
	}
	if got.routeMismatchDetail != "" {
		t.Fatalf("unexpected route mismatch detail: %+v", got)
	}
	if got.peerQueryFailures != 0 {
		t.Fatalf("unexpected peer query failures: %+v", got)
	}

	if !strings.Contains(strings.Join(logs, "\n"), "warn:peer_not_ready:") {
		t.Fatalf("expected peer_not_ready warning, logs=%v", logs)
	}
}

func TestEvaluateCandidateConnectivityNonSelfPeerWithoutVirtualIP(t *testing.T) {
	var logs []string
	cfg := candidateCheckConfig{
		maxChecks:    3,
		pollInterval: 5 * time.Millisecond,
		probeTimout:  5 * time.Millisecond,
	}
	deps := sessionDeps{
		queryPeerReadiness: func(*EasyTier) (PeerReadiness, error) {
			return PeerReadiness{
				Ready:          false,
				NonSelfPresent: true,
				PeerClass:      peerClassBootstrapOnly,
				PeerID:         "123",
				PeerHostname:   "PublicServer_Test",
			}, nil
		},
		routeInterfaceForTarget: func(string) (string, error) { return "", nil },
		probePeerVirtualIP:      func(string, int, time.Duration) error { return nil },
		shouldCheckRouteOwner:   func() bool { return true },
	}

	got := evaluateCandidateConnectivity(nil, "utun1", 8080, cfg, deps, nil, func(result, reason, detail string) {
		logs = append(logs, result+":"+reason+":"+detail)
	})
	if !got.nonSelfPresent {
		t.Fatalf("expected nonSelfPresent=true, got %+v", got)
	}
	if got.peerClass != peerClassBootstrapOnly {
		t.Fatalf("expected bootstrap_only class, got %+v", got)
	}
	if got.peerReady {
		t.Fatalf("expected peerReady=false when no target virtual ip, got %+v", got)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "warn:bootstrap_connected:") {
		t.Fatalf("expected bootstrap_connected warning, logs=%v", logs)
	}
}

func TestEvaluateCandidateConnectivityBusinessEndpointWaitingVirtualIP(t *testing.T) {
	var logs []string
	cfg := candidateCheckConfig{
		maxChecks:    3,
		pollInterval: 5 * time.Millisecond,
		probeTimout:  5 * time.Millisecond,
	}
	deps := sessionDeps{
		queryPeerReadiness: func(*EasyTier) (PeerReadiness, error) {
			return PeerReadiness{
				Ready:          false,
				NonSelfPresent: true,
				PeerClass:      peerClassBusinessPeerWaitingIP,
			}, nil
		},
		routeInterfaceForTarget: func(string) (string, error) { return "", nil },
		probePeerVirtualIP:      func(string, int, time.Duration) error { return nil },
		shouldCheckRouteOwner:   func() bool { return true },
	}

	got := evaluateCandidateConnectivity(nil, "utun1", 8080, cfg, deps, nil, func(result, reason, detail string) {
		logs = append(logs, result+":"+reason+":"+detail)
	})
	if !got.nonSelfPresent {
		t.Fatalf("expected nonSelfPresent=true, got %+v", got)
	}
	if got.peerClass != peerClassBusinessPeerWaitingIP {
		t.Fatalf("expected business waiting class, got %+v", got)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "warn:business_endpoint_waiting:") {
		t.Fatalf("expected business_endpoint_waiting warning, logs=%v", logs)
	}
}

func TestEvaluateCandidateConnectivityInterruptedByStop(t *testing.T) {
	stop := make(chan struct{})
	close(stop)
	cfg := candidateCheckConfig{
		maxChecks:    3,
		pollInterval: 5 * time.Millisecond,
		probeTimout:  5 * time.Millisecond,
	}
	deps := sessionDeps{
		queryPeerReadiness: func(*EasyTier) (PeerReadiness, error) {
			return PeerReadiness{Ready: false}, nil
		},
		probePeerVirtualIP: func(string, int, time.Duration) error { return nil },
		shouldCheckRouteOwner: func() bool { return false },
	}

	got := evaluateCandidateConnectivity(nil, "utun1", 8080, cfg, deps, stop, func(string, string, string) {})
	if !errors.Is(got.lastProbeErr, errSessionInterrupted) {
		t.Fatalf("expected interrupted error, got %+v", got)
	}
}

func TestEvaluateCandidateConnectivitySnapshotErrorStopsAfterBudget(t *testing.T) {
	origStartPoller := startStatePollerFn
	t.Cleanup(func() { startStatePollerFn = origStartPoller })

	snapshots := make(chan EasyTierSnapshot)
	events := make(chan EasyTierEvent, 8)
	startStatePollerFn = func(et *EasyTier, interval time.Duration, ctx context.Context) (<-chan EasyTierSnapshot, <-chan EasyTierEvent) {
		return snapshots, events
	}

	cfg := candidateCheckConfig{
		maxChecks:    3,
		pollInterval: 5 * time.Millisecond,
		probeTimout:  5 * time.Millisecond,
	}
	deps := sessionDeps{
		probePeerVirtualIP:    func(string, int, time.Duration) error { return nil },
		shouldCheckRouteOwner: func() bool { return false },
	}

	for i := 0; i < cfg.maxChecks; i++ {
		events <- EasyTierEvent{Type: EasyTierEventSnapshotError, Err: errors.New("snapshot failed")}
	}

	got := evaluateCandidateConnectivity(&EasyTier{}, "utun1", 8080, cfg, deps, nil, func(string, string, string) {})
	if got.peerQueryFailures < cfg.maxChecks {
		t.Fatalf("expected peerQueryFailures >= %d, got %+v", cfg.maxChecks, got)
	}
}
