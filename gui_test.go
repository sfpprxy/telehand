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
