package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSubmitConfigReturns409WhenPending(t *testing.T) {
	g := NewGUIServer(18080)

	// Fill the buffered channel to simulate an unconsumed config.
	g.configCh <- &Config{NetworkName: "n", NetworkSecret: "s"}

	body, _ := json.Marshal(map[string]string{"config": "not-base64"})
	req := httptest.NewRequest(http.MethodPost, "/api/submit-config", bytes.NewReader(body))
	w := httptest.NewRecorder()
	g.handleSubmitConfig(w, req)
	if w.Code != 400 {
		t.Fatalf("expected decode error first, got %d body=%s", w.Code, w.Body.String())
	}

	// Use a valid config and assert pending conflict.
	cfg, err := EncodeConfig(&Config{NetworkName: "n", NetworkSecret: "s", Peers: []string{"tcp://1.1.1.1:11010"}})
	if err != nil {
		t.Fatalf("encode config failed: %v", err)
	}
	body, _ = json.Marshal(map[string]string{"config": cfg})
	req = httptest.NewRequest(http.MethodPost, "/api/submit-config", bytes.NewReader(body))
	w = httptest.NewRecorder()
	g.handleSubmitConfig(w, req)
	if w.Code != 409 {
		t.Fatalf("expected 409 when config already pending, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPeerInfoEndpoint(t *testing.T) {
	g := NewGUIServer(18080)
	g.SetPeerInfoProvider(func() (PeerInfoSnapshot, error) {
		return PeerInfoSnapshot{
			UpdatedAt: "2026-02-24T00:00:00Z",
			Peers: []PeerInfo{
				{
					VirtualIPv4: "10.0.0.2",
					Hostname:    "host-a",
					RouteCost:   "Local",
					Protocol:    "-",
					Latency:     "-",
					Upload:      "1 KB",
					Download:    "2 KB",
					LossRate:    "0%",
					Version:     "v1",
					Role:        "Client",
					IsSelf:      true,
				},
			},
		}, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/api/peer-info", nil)
	w := httptest.NewRecorder()
	g.handlePeerInfo(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp PeerInfoSnapshot
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal failed: %v body=%s", err, w.Body.String())
	}
	if len(resp.Peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(resp.Peers))
	}
	if !resp.Peers[0].IsSelf {
		t.Fatalf("expected peer is_self=true")
	}
}
