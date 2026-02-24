package main

import (
	"testing"
	"time"
)

func TestWaitForIPReturnsTUNPermissionDeniedEarly(t *testing.T) {
	et := NewEasyTier(nil)
	et.mu.Lock()
	et.logs = append(et.logs, "tun device error: Operation not permitted")
	et.mu.Unlock()

	start := time.Now()
	_, err := et.WaitForIP(5 * time.Second)
	if err == nil {
		t.Fatalf("expected WaitForIP error, got nil")
	}
	if got := errorCodeOf(err); got != ErrorCodeTUNPermissionDenied {
		t.Fatalf("expected error code %q, got %q (err=%v)", ErrorCodeTUNPermissionDenied, got, err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("expected early fail, took too long: %v", time.Since(start))
	}
}
