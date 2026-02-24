package main

import "testing"

func TestDisplayedRoleForPeer(t *testing.T) {
	if got := displayedRoleForPeer(false, "client"); got != "-" {
		t.Fatalf("expected non-self role '-', got %q", got)
	}
	if got := displayedRoleForPeer(true, "client"); got != "Client" {
		t.Fatalf("expected self client role, got %q", got)
	}
	if got := displayedRoleForPeer(true, "server"); got != "Server" {
		t.Fatalf("expected self server role, got %q", got)
	}
}
