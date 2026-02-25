package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	ExitCodeOK      = 0
	ExitCodeParam   = 2
	ExitCodeNetwork = 3
	ExitCodeService = 4
)

var hostnameReader = os.Hostname

func buildConfigFromInputs(networkName, networkSecret, peers string) (*Config, error) {
	name := strings.TrimSpace(networkName)
	secret := strings.TrimSpace(networkSecret)
	peerList := parsePeers(peers)

	switch {
	case name == "":
		return nil, errors.New("network name is required")
	case secret == "":
		return nil, errors.New("network secret is required")
	case len(peerList) == 0:
		return nil, errors.New("at least one peer is required")
	}

	return &Config{
		NetworkName:   name,
		NetworkSecret: secret,
		Peers:         peerList,
	}, nil
}

func encodeConfigOrErr(cfg *Config) (string, error) {
	if cfg == nil {
		return "", errors.New("config is required")
	}
	return EncodeConfig(cfg)
}

func buildEncodedConfigFromInputs(networkName, networkSecret, peers string) (string, *Config, error) {
	cfg, err := buildConfigFromInputs(networkName, networkSecret, peers)
	if err != nil {
		return "", nil, err
	}
	encoded, err := encodeConfigOrErr(cfg)
	if err != nil {
		return "", nil, err
	}
	return encoded, cfg, nil
}

func buildEncodedConfigWithDefaults(networkName, networkSecret, peers string) (string, *Config, error) {
	name, secret, peerList := withDefaultNetworkInputs(networkName, networkSecret, peers)
	return buildEncodedConfigFromInputs(name, secret, peerList)
}

func submitEncodedConfig(encoded string, submitFn func(string) error) error {
	if submitFn == nil {
		return errors.New("submit function is required")
	}
	code := strings.TrimSpace(encoded)
	if code == "" {
		return errors.New("config code is required")
	}
	return submitFn(code)
}

func withDefaultNetworkInputs(networkName, networkSecret, peers string) (string, string, string) {
	host := detectHostIdentity()
	name := strings.TrimSpace(networkName)
	secret := strings.TrimSpace(networkSecret)
	peerList := strings.TrimSpace(peers)

	if name == "" {
		name = "telehand:" + host
	}
	if secret == "" {
		secret = "telehand:" + host + random4Digits()
	}

	userPool := parsePeers(peerList)
	mergedPool := mergePeerPools(userPool, defaultPeerPool(), MaxPeerCount)
	return name, secret, peerCSV(mergedPool)
}

func detectHostIdentity() string {
	host, err := hostnameReader()
	if err != nil {
		return "host-" + random4Digits()
	}
	host = sanitizeHostToken(host)
	if host == "" {
		return "host-" + random4Digits()
	}
	return host
}

func sanitizeHostToken(host string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(host) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func random4Digits() string {
	n, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		// Fallback to time-derived value when crypto/rand is unavailable.
		return fmt.Sprintf("%04d", time.Now().UnixNano()%10000)
	}
	return fmt.Sprintf("%04d", n.Int64())
}

func maskSecret(secret string) string {
	s := strings.TrimSpace(secret)
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		return strings.Repeat("*", len(s))
	}
	return s[:2] + strings.Repeat("*", len(s)-4) + s[len(s)-2:]
}

func sanitizeSensitiveLog(line string, secrets ...string) string {
	out := line
	for _, secret := range secrets {
		if strings.TrimSpace(secret) == "" {
			continue
		}
		out = strings.ReplaceAll(out, secret, maskSecret(secret))
	}
	return out
}

func decodeConfigWithValidation(encoded string) (*Config, error) {
	code := strings.TrimSpace(encoded)
	if code == "" {
		return nil, errors.New("config code is required")
	}

	raw, err := base64.StdEncoding.DecodeString(code)
	if err != nil {
		return nil, fmt.Errorf("invalid config string: %w", err)
	}

	// Optional expiration fields allow forward compatibility while keeping
	// legacy config format unchanged.
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("invalid config format: %w", err)
	}
	if err := validateConfigExpiry(envelope); err != nil {
		return nil, err
	}

	cfg, err := DecodeConfig(code)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func validateConfigExpiry(envelope map[string]any) error {
	if envelope == nil {
		return nil
	}
	for _, key := range []string{"expires_at", "expire_at", "exp"} {
		v, ok := envelope[key]
		if !ok {
			continue
		}
		exp, ok := parseExpiryValue(v)
		if !ok {
			return fmt.Errorf("invalid %s in config", key)
		}
		if time.Now().After(exp) {
			return newCodedError(ErrorCodeConfigExpired, "config code has expired")
		}
	}
	return nil
}

func parseExpiryValue(v any) (time.Time, bool) {
	switch t := v.(type) {
	case float64:
		return time.Unix(int64(t), 0), true
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return time.Time{}, false
		}
		if unix, err := strconv.ParseInt(s, 10, 64); err == nil {
			return time.Unix(unix, 0), true
		}
		if ts, err := time.Parse(time.RFC3339, s); err == nil {
			return ts, true
		}
		return time.Time{}, false
	default:
		return time.Time{}, false
	}
}
