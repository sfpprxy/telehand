package main

import (
	"errors"
	"testing"
)

func TestClassifyEasyTierErrorByOS(t *testing.T) {
	tests := []struct {
		name     string
		goos     string
		err      error
		logs     []string
		fallback string
		want     string
	}{
		{
			name:     "coded error keeps original code",
			goos:     "windows",
			err:      newCodedError(ErrorCodeWindowsNotAdmin, "administrator privileges required"),
			fallback: ErrorCodeEasyTierStartFailed,
			want:     ErrorCodeWindowsNotAdmin,
		},
		{
			name:     "tun permission denied maps to tun_permission_denied",
			goos:     "darwin",
			err:      errors.New("timeout waiting for EasyTier virtual IP"),
			logs:     []string{"tun device error: Operation not permitted"},
			fallback: ErrorCodeEasyTierIPTimeout,
			want:     ErrorCodeTUNPermissionDenied,
		},
		{
			name:     "auth keyword maps to auth_failed",
			goos:     "darwin",
			err:      errors.New("authentication failed: invalid network secret"),
			fallback: ErrorCodeEasyTierIPTimeout,
			want:     ErrorCodeAuthFailed,
		},
		{
			name:     "peer unreachable keyword maps to peer_unreachable",
			goos:     "linux",
			err:      errors.New("peer unreachable: connection refused"),
			fallback: ErrorCodeEasyTierIPTimeout,
			want:     ErrorCodePeerUnreachable,
		},
		{
			name:     "windows firewall keyword from error",
			goos:     "windows",
			err:      errors.New("peer connection blocked by firewall policy"),
			fallback: ErrorCodeEasyTierIPTimeout,
			want:     ErrorCodeWindowsFirewallBlocked,
		},
		{
			name:     "windows tun keyword from logs",
			goos:     "windows",
			err:      errors.New("timeout waiting for EasyTier virtual IP"),
			logs:     []string{"[ERROR] failed to create adapter via Wintun"},
			fallback: ErrorCodeEasyTierIPTimeout,
			want:     ErrorCodeWindowsTUNInitFailed,
		},
		{
			name:     "fallback when no keyword",
			goos:     "windows",
			err:      errors.New("unknown startup failure"),
			fallback: ErrorCodeEasyTierStartFailed,
			want:     ErrorCodeEasyTierStartFailed,
		},
		{
			name:     "non windows ignores windows keywords",
			goos:     "darwin",
			err:      errors.New("wintun initialization failed"),
			fallback: ErrorCodeEasyTierIPTimeout,
			want:     ErrorCodeEasyTierIPTimeout,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyEasyTierErrorByOS(tc.goos, tc.err, tc.logs, tc.fallback)
			if got != tc.want {
				t.Fatalf("classifyEasyTierErrorByOS()=%q want=%q", got, tc.want)
			}
		})
	}
}
