package main

import (
	"context"
	"sort"
	"strings"
	"time"
)

type EasyTierEventType string

const (
	EasyTierEventTunReady      EasyTierEventType = "tun_ready"
	EasyTierEventPeerAdded     EasyTierEventType = "peer_added"
	EasyTierEventPeerRemoved   EasyTierEventType = "peer_removed"
	EasyTierEventEndpointReady EasyTierEventType = "endpoint_ready"
	EasyTierEventProcessExit   EasyTierEventType = "process_exit"
	EasyTierEventSnapshotError EasyTierEventType = "snapshot_error"
)

type EasyTierEvent struct {
	Type      EasyTierEventType
	At        time.Time
	PeerID    string
	PeerClass string
	Err       error
}

type EasyTierStatePoller struct {
	et       *EasyTier
	interval time.Duration
}

func NewEasyTierStatePoller(et *EasyTier, interval time.Duration) *EasyTierStatePoller {
	if interval <= 0 {
		interval = StatePollInterval
	}
	return &EasyTierStatePoller{et: et, interval: interval}
}

func (p *EasyTierStatePoller) Start(ctx context.Context) (<-chan EasyTierSnapshot, <-chan EasyTierEvent) {
	snapshots := make(chan EasyTierSnapshot, 4)
	events := make(chan EasyTierEvent, 8)

	go func() {
		defer close(snapshots)
		defer close(events)

		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()

		var prev *EasyTierSnapshot
		p.emitSnapshot(ctx, snapshots, events, &prev)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.emitSnapshot(ctx, snapshots, events, &prev)
			}
		}
	}()

	return snapshots, events
}

func (p *EasyTierStatePoller) emitSnapshot(
	ctx context.Context,
	snapshots chan<- EasyTierSnapshot,
	events chan<- EasyTierEvent,
	prev **EasyTierSnapshot,
) {
	snap, err := p.et.QuerySnapshot()
	if err != nil {
		if p.et != nil && p.et.cmd != nil && p.et.cmd.ProcessState != nil && p.et.cmd.ProcessState.Exited() {
			select {
			case events <- EasyTierEvent{Type: EasyTierEventProcessExit, At: time.Now()}:
			default:
			}
		}
		select {
		case events <- EasyTierEvent{Type: EasyTierEventSnapshotError, At: time.Now(), Err: err}:
		default:
		}
		return
	}

	select {
	case snapshots <- snap:
	default:
		// Keep latest snapshot hot; dropping stale one is acceptable.
	}

	for _, e := range diffSnapshots(*prev, &snap) {
		select {
		case <-ctx.Done():
			return
		case events <- e:
		default:
		}
	}
	*prev = &snap
}

func diffSnapshots(prev, curr *EasyTierSnapshot) []EasyTierEvent {
	if curr == nil {
		return nil
	}
	events := make([]EasyTierEvent, 0, 8)
	now := curr.At
	if now.IsZero() {
		now = time.Now()
	}

	if prev == nil {
		if hasSnapshotIP(curr) {
			events = append(events, EasyTierEvent{Type: EasyTierEventTunReady, At: now})
		}
		if curr.Readiness.PeerClass == peerClassEndpointReady {
			events = append(events, EasyTierEvent{Type: EasyTierEventEndpointReady, At: now, PeerID: curr.Readiness.PeerID, PeerClass: curr.Readiness.PeerClass})
		}
		return events
	}

	if !hasSnapshotIP(prev) && hasSnapshotIP(curr) {
		events = append(events, EasyTierEvent{Type: EasyTierEventTunReady, At: now})
	}

	prevIDs := toSortedSet(prev.Readiness.PeerIDs)
	currIDs := toSortedSet(curr.Readiness.PeerIDs)

	for _, id := range currIDs {
		if !containsID(prevIDs, id) {
			events = append(events, EasyTierEvent{Type: EasyTierEventPeerAdded, At: now, PeerID: id, PeerClass: curr.Readiness.PeerClass})
		}
	}
	for _, id := range prevIDs {
		if !containsID(currIDs, id) {
			events = append(events, EasyTierEvent{Type: EasyTierEventPeerRemoved, At: now, PeerID: id})
		}
	}

	if prev.Readiness.PeerClass != peerClassEndpointReady && curr.Readiness.PeerClass == peerClassEndpointReady {
		events = append(events, EasyTierEvent{Type: EasyTierEventEndpointReady, At: now, PeerID: curr.Readiness.PeerID, PeerClass: curr.Readiness.PeerClass})
	}
	return events
}

func hasSnapshotIP(s *EasyTierSnapshot) bool {
	if s == nil || s.Node == nil {
		return false
	}
	ip := stripCIDR(strings.TrimSpace(s.Node.IPv4Addr))
	return ip != "" && ip != "0.0.0.0"
}

func containsID(sortedIDs []string, id string) bool {
	idx := sort.SearchStrings(sortedIDs, id)
	return idx < len(sortedIDs) && sortedIDs[idx] == id
}

func toSortedSet(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		v := strings.TrimSpace(id)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
