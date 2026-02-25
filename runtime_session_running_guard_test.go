package main

import (
	"context"
	"testing"
	"time"
)

func TestRunRunningStateGuardReconnectOnPeerRemovedBurst(t *testing.T) {
	origStartPoller := startStatePollerFn
	t.Cleanup(func() { startStatePollerFn = origStartPoller })

	snapshots := make(chan EasyTierSnapshot)
	events := make(chan EasyTierEvent, PeerRemovedBurstCount+1)
	startStatePollerFn = func(et *EasyTier, interval time.Duration, ctx context.Context) (<-chan EasyTierSnapshot, <-chan EasyTierEvent) {
		return snapshots, events
	}

	gui := NewGUIServer(18080)
	reconnect := make(chan string, 1)
	stop := make(chan struct{}, 1)
	deps := defaultSessionDeps
	deps.shouldCheckRouteOwner = func() bool { return false }
	deps.probePeerVirtualIP = func(string, int, time.Duration) error { return nil }

	go runRunningStateGuard(gui, true, &EasyTier{}, "utun9", 8080, defaultRunningGuardConfig, deps, stop, reconnect)

	for i := 0; i < PeerRemovedBurstCount; i++ {
		events <- EasyTierEvent{Type: EasyTierEventPeerRemoved, At: time.Now()}
	}

	select {
	case reason := <-reconnect:
		if reason != "peer_probe_degraded" {
			t.Fatalf("unexpected reconnect reason: %s", reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected reconnect request after peer_removed burst")
	}
}
