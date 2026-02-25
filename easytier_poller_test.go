package main

import (
	"testing"
	"time"
)

func TestDiffSnapshotsDetectsTunReadyAndEndpointReady(t *testing.T) {
	prev := &EasyTierSnapshot{
		At:   time.Now(),
		Node: &nodeInfo{IPv4Addr: "0.0.0.0/24"},
		Readiness: PeerReadiness{
			Ready:          false,
			NonSelfPresent: false,
			PeerClass:      peerClassNone,
		},
	}
	curr := &EasyTierSnapshot{
		At:   time.Now(),
		Node: &nodeInfo{IPv4Addr: "10.1.2.1/24"},
		Readiness: PeerReadiness{
			Ready:          true,
			TargetIP:       "10.1.2.2",
			NonSelfPresent: true,
			PeerClass:      peerClassEndpointReady,
			PeerID:         "42",
			PeerIDs:        []string{"42"},
		},
	}

	evts := diffSnapshots(prev, curr)
	if len(evts) < 2 {
		t.Fatalf("expected at least 2 events, got %d: %#v", len(evts), evts)
	}
	hasTun := false
	hasEndpoint := false
	for _, e := range evts {
		if e.Type == EasyTierEventTunReady {
			hasTun = true
		}
		if e.Type == EasyTierEventEndpointReady {
			hasEndpoint = true
		}
	}
	if !hasTun || !hasEndpoint {
		t.Fatalf("expected tun_ready and endpoint_ready events, got %#v", evts)
	}
}

func TestDiffSnapshotsDetectsPeerAddedAndRemoved(t *testing.T) {
	prev := &EasyTierSnapshot{
		At: time.Now(),
		Readiness: PeerReadiness{
			PeerIDs: []string{"a", "b"},
		},
	}
	curr := &EasyTierSnapshot{
		At: time.Now(),
		Readiness: PeerReadiness{
			PeerIDs: []string{"b", "c"},
		},
	}
	evts := diffSnapshots(prev, curr)
	addedC := false
	removedA := false
	for _, e := range evts {
		if e.Type == EasyTierEventPeerAdded && e.PeerID == "c" {
			addedC = true
		}
		if e.Type == EasyTierEventPeerRemoved && e.PeerID == "a" {
			removedA = true
		}
	}
	if !addedC || !removedA {
		t.Fatalf("expected peer add/remove events, got %#v", evts)
	}
}
