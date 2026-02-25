package main

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

type PeerProbeResult struct {
	Peer      string
	Latency   time.Duration
	Reachable bool
	Err       error
}

type PeerSelection struct {
	Ordered []string
	Results []PeerProbeResult
}

type peerProbeFunc func(peer string, timeout time.Duration, sampleCount int) (time.Duration, error)

func rankPeersByLatency(peers []string) PeerSelection {
	return rankPeersByLatencyWithProbe(
		peers,
		PeerProbeTimeout,
		PeerProbeConcurrency,
		PeerProbeSampleCount,
		probePeerLatency,
	)
}

func rankPeersByLatencyWithProbe(
	peers []string,
	timeout time.Duration,
	concurrency int,
	sampleCount int,
	probe peerProbeFunc,
) PeerSelection {
	if len(peers) == 0 {
		return PeerSelection{}
	}
	if timeout <= 0 {
		timeout = PeerProbeTimeout
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	if sampleCount <= 0 {
		sampleCount = 1
	}
	if probe == nil {
		probe = probePeerLatency
	}

	type task struct {
		idx  int
		peer string
	}
	results := make([]PeerProbeResult, len(peers))
	workCh := make(chan task, len(peers))
	var wg sync.WaitGroup
	workerCount := concurrency
	if workerCount > len(peers) {
		workerCount = len(peers)
	}
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range workCh {
				latency, err := probe(t.peer, timeout, sampleCount)
				reachable := err == nil
				if !reachable {
					latency = PeerProbeUnreachableRTT
				}
				results[t.idx] = PeerProbeResult{
					Peer:      t.peer,
					Latency:   latency,
					Reachable: reachable,
					Err:       err,
				}
			}
		}()
	}

	for idx, peer := range peers {
		workCh <- task{idx: idx, peer: peer}
	}
	close(workCh)
	wg.Wait()

	orderedResults := append([]PeerProbeResult(nil), results...)
	sort.SliceStable(orderedResults, func(i, j int) bool {
		if orderedResults[i].Reachable != orderedResults[j].Reachable {
			return orderedResults[i].Reachable
		}
		if orderedResults[i].Latency != orderedResults[j].Latency {
			return orderedResults[i].Latency < orderedResults[j].Latency
		}
		return false
	})
	ordered := make([]string, 0, len(orderedResults))
	for _, item := range orderedResults {
		ordered = append(ordered, item.Peer)
	}
	return PeerSelection{
		Ordered: ordered,
		Results: orderedResults,
	}
}

func formatPeerSelectionForLog(results []PeerProbeResult, masked bool) string {
	if len(results) == 0 {
		return "-"
	}
	items := make([]string, 0, len(results))
	for _, res := range results {
		peer := strings.TrimSpace(res.Peer)
		if masked {
			peer = maskPeerAddress(peer)
		}
		if !res.Reachable {
			items = append(items, fmt.Sprintf("%s(unreachable)", peer))
			continue
		}
		items = append(items, fmt.Sprintf("%s(%dms)", peer, res.Latency.Milliseconds()))
	}
	return strings.Join(items, ", ")
}

func probePeerLatency(peer string, timeout time.Duration, sampleCount int) (time.Duration, error) {
	if sampleCount <= 0 {
		sampleCount = 1
	}
	network, address, err := peerDialTarget(peer)
	if err != nil {
		return 0, err
	}
	total := time.Duration(0)
	for i := 0; i < sampleCount; i++ {
		start := time.Now()
		conn, dialErr := net.DialTimeout(network, address, timeout)
		if dialErr != nil {
			return 0, dialErr
		}
		_ = conn.Close()
		total += time.Since(start)
	}
	return total / time.Duration(sampleCount), nil
}

func peerDialTarget(peer string) (string, string, error) {
	normalized, ok := normalizePeerAddress(peer)
	if !ok {
		return "", "", errors.New("invalid peer address")
	}
	u, err := url.Parse(normalized)
	if err != nil || u == nil {
		return "", "", errors.New("invalid peer address")
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		return "", "", errors.New("invalid peer host:port")
	}
	scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
	switch scheme {
	case "tcp", "udp":
		return scheme, net.JoinHostPort(host, port), nil
	default:
		return "", "", fmt.Errorf("unsupported peer scheme: %s", scheme)
	}
}

