package main

import (
	"reflect"
	"testing"
)

func TestParsePeersNormalizeDedupAndDropInvalid(t *testing.T) {
	got := parsePeers(" tcp://1.1.1.1:11010, udp://2.2.2.2:22020, tcp://1.1.1.1:11010, http://bad:80, bad , tcp://3.3.3.3:abc ")
	want := []string{
		"tcp://1.1.1.1:11010",
		"udp://2.2.2.2:22020",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsePeers mismatch: got=%v want=%v", got, want)
	}
}

func TestMergePeerPoolsPrefersUserOrder(t *testing.T) {
	preferred := []string{
		"tcp://10.0.0.1:11010",
		"tcp://10.0.0.2:11010",
	}
	fallback := []string{
		"tcp://10.0.0.2:11010",
		"tcp://10.0.0.3:11010",
	}
	got := mergePeerPools(preferred, fallback, 3)
	want := []string{
		"tcp://10.0.0.1:11010",
		"tcp://10.0.0.2:11010",
		"tcp://10.0.0.3:11010",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergePeerPools mismatch: got=%v want=%v", got, want)
	}
}

func TestRuntimePeerPoolIncludesDefaults(t *testing.T) {
	input := []string{"tcp://8.8.8.8:11010"}
	got := runtimePeerPool(input)
	if len(got) < 3 {
		t.Fatalf("runtimePeerPool should include user peer + default peers, got=%v", got)
	}
	if got[0] != "tcp://8.8.8.8:11010" {
		t.Fatalf("runtimePeerPool first peer should keep user priority, got=%v", got)
	}
}

func TestNormalizePeerAddressRejectsInvalidScheme(t *testing.T) {
	if _, ok := normalizePeerAddress("http://1.1.1.1:80"); ok {
		t.Fatal("expected http peer to be rejected")
	}
}
