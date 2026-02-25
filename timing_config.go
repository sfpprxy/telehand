package main

import "time"

const (
	CandidateMaxChecks      = 3
	StatePollInterval       = 500 * time.Millisecond
	PeerProbeSampleCount    = 1
	PeerProbeTimeout        = 800 * time.Millisecond
	PeerProbeConcurrency    = 4
	PeerProbeUnreachableRTT = 9999 * time.Millisecond
	EasyTierWaitIPTimeout   = 16 * time.Second

	RunningConsecutiveFailed = 3
	RunningProbeTimeout      = 500 * time.Millisecond

	PeerRemovedBurstCount  = 3
	PeerRemovedBurstWindow = 10 * time.Second
	CandidateLogLimiterTTL = 10 * time.Second
	BootstrapWaitTimeout   = 12 * time.Second

	SubnetCandidateCount = 8

	defaultEasyTierCLIQueryTimeout = 3 * time.Second
	defaultPeerInfoRefreshInterval = 3 * time.Second
)
