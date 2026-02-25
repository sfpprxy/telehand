package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDefaultPeersValue(t *testing.T) {
	const want = "tcp://43.139.65.49:11010"
	if DefaultPeers != want {
		t.Fatalf("unexpected DefaultPeers: got=%q want=%q", DefaultPeers, want)
	}
}

func TestBuildConfigFromInputs(t *testing.T) {
	cfg, err := buildConfigFromInputs(" net ", " secret ", " tcp://1.1.1.1:11010 , tcp://2.2.2.2:11010 ")
	if err != nil {
		t.Fatalf("buildConfigFromInputs failed: %v", err)
	}
	if cfg.NetworkName != "net" {
		t.Fatalf("unexpected network name: %q", cfg.NetworkName)
	}
	if cfg.NetworkSecret != "secret" {
		t.Fatalf("unexpected network secret: %q", cfg.NetworkSecret)
	}
	if len(cfg.Peers) != 2 {
		t.Fatalf("unexpected peers len: %d", len(cfg.Peers))
	}
}

func TestBuildConfigFromInputsValidation(t *testing.T) {
	_, err := buildConfigFromInputs("", "secret", DefaultPeers)
	if err == nil || !strings.Contains(err.Error(), "network name") {
		t.Fatalf("expected network name validation error, got %v", err)
	}

	_, err = buildConfigFromInputs("name", "", DefaultPeers)
	if err == nil || !strings.Contains(err.Error(), "network secret") {
		t.Fatalf("expected network secret validation error, got %v", err)
	}

	_, err = buildConfigFromInputs("name", "secret", " , ")
	if err == nil || !strings.Contains(err.Error(), "peer") {
		t.Fatalf("expected peers validation error, got %v", err)
	}
}

func TestWithDefaultNetworkInputs(t *testing.T) {
	orig := hostnameReader
	hostnameReader = func() (string, error) { return "my-host", nil }
	t.Cleanup(func() { hostnameReader = orig })

	name, secret, peers := withDefaultNetworkInputs("", "", "")
	if name != "telehand:my-host" {
		t.Fatalf("unexpected default name: %q", name)
	}
	if !strings.HasPrefix(secret, "telehand:my-host") {
		t.Fatalf("unexpected default secret: %q", secret)
	}
	if peers != DefaultPeers {
		t.Fatalf("unexpected default peers: %q", peers)
	}
}

func TestDetectHostIdentityFallback(t *testing.T) {
	orig := hostnameReader
	hostnameReader = func() (string, error) { return "", errors.New("fail") }
	t.Cleanup(func() { hostnameReader = orig })

	host := detectHostIdentity()
	if !strings.HasPrefix(host, "host-") {
		t.Fatalf("expected host fallback prefix, got %q", host)
	}
	if len(host) != len("host-0000") {
		t.Fatalf("expected fallback with 4 digits, got %q", host)
	}
}

func TestMaskSecret(t *testing.T) {
	if got := maskSecret("abcd"); got != "****" {
		t.Fatalf("mask short secret failed: %q", got)
	}
	if got := maskSecret("abcdefgh"); got != "ab****gh" {
		t.Fatalf("mask long secret failed: %q", got)
	}
}

func TestDecodeConfigWithValidationExpiry(t *testing.T) {
	payload := map[string]any{
		"network_name":   "n",
		"network_secret": "s",
		"peers":          []string{"tcp://1.1.1.1:11010"},
		"expires_at":     time.Now().Add(-time.Minute).Unix(),
	}
	raw, _ := json.Marshal(payload)
	code := base64.StdEncoding.EncodeToString(raw)

	_, err := decodeConfigWithValidation(code)
	if err == nil {
		t.Fatal("expected expired config error")
	}
	if errorCodeOf(err) != ErrorCodeConfigExpired {
		t.Fatalf("expected error code %q, got %q", ErrorCodeConfigExpired, errorCodeOf(err))
	}
}

func TestDecodeConfigWithValidationKeepsLegacyConfig(t *testing.T) {
	code, err := EncodeConfig(&Config{
		NetworkName:   "n",
		NetworkSecret: "s",
		Peers:         []string{"tcp://1.1.1.1:11010"},
	})
	if err != nil {
		t.Fatalf("EncodeConfig failed: %v", err)
	}
	cfg, err := decodeConfigWithValidation(code)
	if err != nil {
		t.Fatalf("decodeConfigWithValidation failed: %v", err)
	}
	if cfg.NetworkName != "n" {
		t.Fatalf("unexpected network name: %q", cfg.NetworkName)
	}
}

func TestBuildEncodedConfigFromInputsMatchesEncodeConfig(t *testing.T) {
	gotEncoded, gotCfg, err := buildEncodedConfigFromInputs("net", "secret", "tcp://1.1.1.1:11010")
	if err != nil {
		t.Fatalf("buildEncodedConfigFromInputs failed: %v", err)
	}

	wantEncoded, err := EncodeConfig(&Config{
		NetworkName:   "net",
		NetworkSecret: "secret",
		Peers:         []string{"tcp://1.1.1.1:11010"},
	})
	if err != nil {
		t.Fatalf("EncodeConfig failed: %v", err)
	}
	if gotEncoded != wantEncoded {
		t.Fatalf("encoded mismatch got=%q want=%q", gotEncoded, wantEncoded)
	}
	if gotCfg.NetworkName != "net" {
		t.Fatalf("unexpected cfg %+v", gotCfg)
	}
}
