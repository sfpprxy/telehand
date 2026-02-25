package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRankPeersByLatencyWithProbeOrdersReachableFirst(t *testing.T) {
	peers := []string{
		"tcp://1.1.1.1:11010",
		"tcp://2.2.2.2:11010",
		"tcp://3.3.3.3:11010",
	}
	probe := func(peer string, timeout time.Duration, sampleCount int) (time.Duration, error) {
		switch peer {
		case "tcp://1.1.1.1:11010":
			return 30 * time.Millisecond, nil
		case "tcp://2.2.2.2:11010":
			return 10 * time.Millisecond, nil
		default:
			return 0, errors.New("timeout")
		}
	}

	selection := rankPeersByLatencyWithProbe(peers, 800*time.Millisecond, 4, 1, probe)
	if len(selection.Ordered) != 3 {
		t.Fatalf("unexpected ordered size: %d", len(selection.Ordered))
	}
	wantOrder := []string{
		"tcp://2.2.2.2:11010",
		"tcp://1.1.1.1:11010",
		"tcp://3.3.3.3:11010",
	}
	for i := range wantOrder {
		if selection.Ordered[i] != wantOrder[i] {
			t.Fatalf("unexpected order at %d: got=%s want=%s", i, selection.Ordered[i], wantOrder[i])
		}
	}
	last := selection.Results[len(selection.Results)-1]
	if last.Reachable {
		t.Fatalf("expected last peer unreachable, got %+v", last)
	}
	if last.Latency != PeerProbeUnreachableRTT {
		t.Fatalf("unexpected unreachable latency: %s", last.Latency)
	}
}

func TestFormatPeerSelectionForLog(t *testing.T) {
	line := formatPeerSelectionForLog([]PeerProbeResult{
		{Peer: "tcp://1.2.3.4:11010", Latency: 12 * time.Millisecond, Reachable: true},
		{Peer: "tcp://5.6.7.8:11010", Reachable: false, Err: errors.New("timeout")},
	}, true)
	if !strings.Contains(line, "12ms") {
		t.Fatalf("expected latency text in log line, got=%q", line)
	}
	if !strings.Contains(line, "unreachable") {
		t.Fatalf("expected unreachable marker in log line, got=%q", line)
	}
	if strings.Contains(line, "1.2.3.4") {
		t.Fatalf("expected masked output, got=%q", line)
	}
}

func TestPeerDialTargetRejectsInvalidPeer(t *testing.T) {
	if _, _, err := peerDialTarget("bad-peer"); err == nil {
		t.Fatal("expected peerDialTarget to fail on invalid peer")
	}
}
