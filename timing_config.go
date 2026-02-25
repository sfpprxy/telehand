package main

import "time"

const (
	defaultStateSnapshotPollInterval = 500 * time.Millisecond
	defaultPeerProbeTimeout          = 800 * time.Millisecond
	defaultEasyTierCLIQueryTimeout   = 3 * time.Second
	defaultPeerInfoRefreshInterval   = 3 * time.Second
)
