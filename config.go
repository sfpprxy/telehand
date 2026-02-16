package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

type Config struct {
	NetworkName   string   `json:"network_name"`
	NetworkSecret string   `json:"network_secret"`
	Peers         []string `json:"peers"`
}

func EncodeConfig(c *Config) (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func DecodeConfig(s string) (*Config, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid config string: %w", err)
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("invalid config format: %w", err)
	}
	if c.NetworkName == "" || c.NetworkSecret == "" || len(c.Peers) == 0 {
		return nil, fmt.Errorf("config missing required fields: network_name, network_secret, peers")
	}
	return &c, nil
}
