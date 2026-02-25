package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"
)

type sessionOptions struct {
	Role             string
	NoBrowser        bool
	EncodedConfig    string
	Commands         []InstallCommand
	ClipboardCommand string
}

type sessionDeps struct {
	queryPeerReadiness       func(*EasyTier) (PeerReadiness, error)
	querySnapshot            func(*EasyTier) (EasyTierSnapshot, error)
	routeInterfaceForTarget  func(string) (string, error)
	addHostRouteForTarget    func(string, string) error
	removeHostRouteForTarget func(string, string) error
	probePeerVirtualIP       func(string, int, time.Duration) error
	shouldCheckRouteOwner    func() bool
}

type candidateCheckConfig struct {
	maxChecks         int
	pollInterval      time.Duration
	probeTimout       time.Duration
	returnOnBootstrap bool
}

type runningGuardConfig struct {
	consecutiveFailed int
	probeTimeout      time.Duration
}

type candidateCheckResult struct {
	peerReady           bool
	nonSelfPresent      bool
	peerClass           string
	targetIP            string
	probeSuccess        bool
	peerQueryFailures   int
	routeMismatchDetail string
	lastProbeErr        error
	hostRouteInstalled  bool
}

type connectRoundResult struct {
	et                  *EasyTier
	virtIP              string
	baseline            SessionBaseline
	activeHostRoutePeer string
	selectedPeer        string
	orderedPeers        []string
}

var (
	defaultSessionDeps = sessionDeps{
		queryPeerReadiness:       func(et *EasyTier) (PeerReadiness, error) { return et.QueryPeerReadiness() },
		querySnapshot:            func(et *EasyTier) (EasyTierSnapshot, error) { return et.QuerySnapshot() },
		routeInterfaceForTarget:  routeInterfaceForTarget,
		addHostRouteForTarget:    addHostRouteForTarget,
		removeHostRouteForTarget: removeHostRouteForTarget,
		probePeerVirtualIP:       probePeerVirtualIP,
		shouldCheckRouteOwner:    shouldCheckRouteOwnership,
	}
	defaultCandidateCheckConfig = candidateCheckConfig{
		maxChecks:    CandidateMaxChecks,
		pollInterval: StatePollInterval,
		probeTimout:  PeerProbeTimeout,
	}
	defaultRunningGuardConfig = runningGuardConfig{
		consecutiveFailed: RunningConsecutiveFailed,
		probeTimeout:      RunningProbeTimeout,
	}
	candidateLogLimiterMu   sync.Mutex
	candidateLogLimiterSeen = map[string]time.Time{}
	candidateLogLimiterTTL  = CandidateLogLimiterTTL
	bootstrapWaitTimeout    = BootstrapWaitTimeout

	collectLocalIPv4NetsFn          = collectLocalIPv4Nets
	chooseCandidatesFn              = chooseCandidates
	rankPeersByLatencyFn            = rankPeersByLatency
	newEasyTierFn                   = NewEasyTier
	easyTierStartFn                 = func(et *EasyTier, cfg *Config, opts EasyTierStartOptions) error { return et.Start(cfg, opts) }
	easyTierWaitForIPFn             = func(et *EasyTier, timeout time.Duration) (string, error) { return et.WaitForIP(timeout) }
	interfaceByIPv4Fn               = interfaceByIPv4
	evaluateCandidateConnectivityFn = evaluateCandidateConnectivity
	startStatePollerFn              = func(et *EasyTier, interval time.Duration, ctx context.Context) (<-chan EasyTierSnapshot, <-chan EasyTierEvent) {
		poller := NewEasyTierStatePoller(et, interval)
		return poller.Start(ctx)
	}
)

var errSessionInterrupted = errors.New("session interrupted")

func runSession(opts sessionOptions) int {
	role := strings.ToLower(strings.TrimSpace(opts.Role))
	if role == "" {
		role = "server"
	}

	gui := NewGUIServer(18080)
	gui.SetState(GUIState{
		Phase:            "config",
		Role:             role,
		Commands:         opts.Commands,
		ClipboardCommand: opts.ClipboardCommand,
	})

	var (
		runtimeMu       sync.RWMutex
		runtimeET       *EasyTier
		runtimeNetOwner string
		runtimeNetHash  string
	)
	gui.SetPeerInfoProvider(func() (PeerInfoSnapshot, error) {
		runtimeMu.RLock()
		et := runtimeET
		networkOwner := runtimeNetOwner
		networkHash := runtimeNetHash
		runtimeMu.RUnlock()
		if et == nil {
			return PeerInfoSnapshot{
				UpdatedAt:    time.Now().Format(time.RFC3339),
				NetworkOwner: networkOwner,
				NetworkHash:  networkHash,
				Peers:        []PeerInfo{},
			}, nil
		}
		snapshot, err := et.QueryPeerInfo(role)
		if err != nil {
			return snapshot, err
		}
		snapshot.NetworkOwner = networkOwner
		snapshot.NetworkHash = networkHash
		return snapshot, nil
	})

	submitFn := func(encoded string) error {
		return submitEncodedConfig(encoded, gui.SubmitConfigEncoded)
	}

	apiPort := 0
	api := NewAPIServer("0.0.0.0", 8080, func(log CmdLog) {
		gui.AddLog(log)
	}, func() HealthResp {
		s := gui.GetState()
		return HealthResp{
			Status:    "ok",
			Phase:     s.Phase,
			Role:      s.Role,
			VirtIP:    s.VirtIP,
			APIPort:   apiPort,
			GUIPort:   gui.Port(),
			Error:     s.Error,
			ErrorCode: s.ErrorCode,
		}
	}, submitFn)
	if err := api.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start API server: %v\n", err)
		return ExitCodeService
	}
	apiPort = api.Port()
	fmt.Printf("API server started at http://0.0.0.0:%d\n", apiPort)

	if err := gui.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start GUI: %v\n", err)
		api.Stop()
		return ExitCodeService
	}
	guiURL := fmt.Sprintf("http://127.0.0.1:%d", gui.Port())
	fmt.Printf("GUI started at %s\n", guiURL)

	state := gui.GetState()
	state.APIPort = apiPort
	gui.SetState(state)

	cliOnly := opts.NoBrowser
	if !opts.NoBrowser {
		if err := openBrowser(guiURL); err != nil {
			fmt.Fprintf(os.Stderr, "No browser detected, fallback to CLI mode: %v\n", err)
			cliOnly = true
		}
	} else {
		fmt.Println("Browser auto-open disabled; running in CLI mode.")
	}
	if cliOnly {
		fmt.Println("CLI mode: state/debug information will be printed to stdout/stderr.")
	}

	if strings.TrimSpace(opts.EncodedConfig) != "" {
		if err := submitFn(opts.EncodedConfig); err != nil {
			fmt.Fprintf(os.Stderr, "Invalid config code: %v\n", err)
			api.Stop()
			gui.Stop()
			return ExitCodeParam
		}
	}

	cfg := gui.WaitForConfig()
	if cfg == nil {
		fmt.Println("Stopped by user.")
		api.Stop()
		gui.Stop()
		return ExitCodeOK
	}

	cfg.Peers = runtimePeerPool(cfg.Peers)
	if len(cfg.Peers) == 0 {
		errCode := ErrorCodePeerUnreachable
		errMsg := formatConnectError(errCode, fmt.Errorf("no available peers after normalization"))
		setSessionError(gui, apiPort, errCode, errMsg)
		fmt.Fprintln(os.Stderr, errMsg)
		api.Stop()
		gui.Stop()
		return exitCodeFromErrorCode(errCode, ExitCodeNetwork)
	}

	fmt.Printf("Network ready: name=%s secret=%s peers=%s\n", cfg.NetworkName, maskSecret(cfg.NetworkSecret), strings.Join(cfg.Peers, ","))
	fmt.Printf("State: initializing -> connecting (%s)\n", role)

	networkOwner := networkOwnerFromNetworkName(cfg.NetworkName)
	networkHash := computeNetworkHash(cfg.NetworkName, cfg.NetworkSecret)
	state = gui.GetState()
	state.NetworkOwner = networkOwner
	state.NetworkHash = networkHash
	state.BusinessEndpoint = "正在连接业务端..."
	gui.SetState(state)
	runtimeMu.Lock()
	runtimeNetOwner = networkOwner
	runtimeNetHash = networkHash
	runtimeMu.Unlock()

	deps := defaultSessionDeps
	checkCfg := defaultCandidateCheckConfig
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sig)
	stopCh := make(chan struct{})
	var stopOnce sync.Once
	requestStop := func() {
		stopOnce.Do(func() {
			close(stopCh)
		})
	}
	go func() { <-sig; requestStop() }()
	go func() {
		next := gui.WaitForConfig()
		if next == nil {
			requestStop()
		}
	}()

	var (
		activeET              *EasyTier
		activeHostRouteTarget string
		baseline              SessionBaseline
		lastSwitchReason      string
		preferredSubnet       string
	)

	setRuntimeET := func(et *EasyTier) {
		runtimeMu.Lock()
		runtimeET = et
		runtimeMu.Unlock()
	}

	for {
		if isStopRequested(stopCh) {
			fmt.Println("State: stopping")
			api.Stop()
			setRuntimeET(nil)
			gui.Stop()
			return ExitCodeOK
		}

		result, errCode, connectErr := connectWithPeerFallback(
			gui,
			cliOnly,
			cfg,
			role,
			networkHash,
			apiPort,
			checkCfg,
			deps,
			setRuntimeET,
			preferredSubnet,
			stopCh,
		)
		if connectErr != nil {
			if errors.Is(connectErr, errSessionInterrupted) {
				fmt.Println("State: stopping")
				api.Stop()
				setRuntimeET(nil)
				gui.Stop()
				return ExitCodeOK
			}
			errMsg := formatConnectError(errCode, connectErr)
			setSessionError(gui, apiPort, errCode, errMsg)
			fmt.Fprintln(os.Stderr, errMsg)
			api.Stop()
			gui.Stop()
			return exitCodeFromErrorCode(errCode, ExitCodeNetwork)
		}

		activeET = result.et
		activeHostRouteTarget = result.activeHostRoutePeer
		baseline = result.baseline
		preferredSubnet = ""

		fmt.Printf("EasyTier virtual IP: %s\n", result.virtIP)
		fmt.Printf("Session baseline: tun_device=%s virtual_subnet=%s network_hash=%s\n", baseline.TunDevice, baseline.VirtualCIDR, baseline.NetworkHash)
		state = gui.GetState()
		state.Phase = "running"
		state.VirtIP = result.virtIP
		state.TUNDevice = baseline.TunDevice
		state.VirtualSubnet = baseline.VirtualCIDR
		state.CurrentPeer = result.selectedPeer
		state.LastSwitchReason = lastSwitchReason
		state.BusinessEndpoint = "已连接"
		state.Error = ""
		state.ErrorCode = ""
		gui.SetState(state)
		fmt.Printf("State: connecting -> running\n")
		fmt.Printf("API server reachable at http://%s:%d\n", result.virtIP, apiPort)
		fmt.Printf("State guard: threshold=%d consecutive failures\n", defaultRunningGuardConfig.consecutiveFailed)

		stopPeerPrint := make(chan struct{}, 1)
		go printPeerInfoLoop(stopPeerPrint, func() (PeerInfoSnapshot, error) {
			runtimeMu.RLock()
			et := runtimeET
			networkHash := runtimeNetHash
			runtimeMu.RUnlock()
			if et == nil {
				return PeerInfoSnapshot{}, errors.New("peer info unavailable")
			}
			snapshot, err := et.QueryPeerInfo(role)
			if err != nil {
				return snapshot, err
			}
			snapshot.NetworkOwner = networkOwner
			snapshot.NetworkHash = networkHash
			return snapshot, nil
		})

		guardStop := make(chan struct{}, 1)
		reconnectCh := make(chan string, 1)
		go runRunningStateGuard(gui, cliOnly, activeET, baseline.TunDevice, apiPort, defaultRunningGuardConfig, deps, guardStop, reconnectCh)

		reconnectReason := ""
		select {
		case <-stopCh:
			fmt.Println("State: stopping")
			guardStop <- struct{}{}
			stopPeerPrint <- struct{}{}
			api.Stop()
			if activeET != nil {
				if deps.removeHostRouteForTarget != nil && activeHostRouteTarget != "" {
					_ = deps.removeHostRouteForTarget(activeHostRouteTarget, baseline.TunDevice)
				}
				activeET.Stop()
			}
			runtimeMu.Lock()
			runtimeET = nil
			runtimeMu.Unlock()
			gui.Stop()
			final := gui.GetState()
			if final.Phase == "error" && final.ErrorCode != "" {
				return exitCodeFromErrorCode(final.ErrorCode, ExitCodeNetwork)
			}
			return ExitCodeOK
		case reconnectReason = <-reconnectCh:
		}

		guardStop <- struct{}{}
		stopPeerPrint <- struct{}{}
		if activeET != nil {
			if deps.removeHostRouteForTarget != nil && activeHostRouteTarget != "" {
				_ = deps.removeHostRouteForTarget(activeHostRouteTarget, baseline.TunDevice)
			}
			activeET.Stop()
		}
		setRuntimeET(nil)

		if strings.TrimSpace(reconnectReason) == "" {
			reconnectReason = "peer_probe_degraded"
		}
		lastSwitchReason = reconnectReason
		preferredSubnet = baseline.VirtualCIDR
		state = gui.GetState()
		state.Phase = "connecting"
		state.VirtIP = ""
		state.CurrentPeer = ""
		state.LastSwitchReason = lastSwitchReason
		state.BusinessEndpoint = "连接降级，重连中..."
		state.Error = ""
		state.ErrorCode = ""
		gui.SetState(state)
		msg := fmt.Sprintf("[telehand] reconnect requested reason=%s; fallback peers first then subnet", reconnectReason)
		gui.AddDebugLog(msg)
		fmt.Println(msg)
	}
}

func connectWithPeerFallback(
	gui *GUIServer,
	cliOnly bool,
	cfg *Config,
	role string,
	networkHash string,
	apiPort int,
	checkCfg candidateCheckConfig,
	deps sessionDeps,
	setRuntimeET func(*EasyTier),
	preferredSubnet string,
	stop <-chan struct{},
) (connectRoundResult, string, error) {
	result := connectRoundResult{}
	if isStopRequested(stop) {
		return result, "", errSessionInterrupted
	}
	if cfg == nil {
		return result, ErrorCodePeerUnreachable, fmt.Errorf("config is required")
	}
	if setRuntimeET == nil {
		setRuntimeET = func(*EasyTier) {}
	}
	if deps.queryPeerReadiness == nil {
		deps = defaultSessionDeps
	}

	peerPool := runtimePeerPool(cfg.Peers)
	if len(peerPool) == 0 {
		return result, ErrorCodePeerUnreachable, fmt.Errorf("no available peers after normalization")
	}
	selection := rankPeersByLatencyFn(peerPool)
	orderedPeers := selection.Ordered
	if len(orderedPeers) == 0 {
		return result, ErrorCodePeerUnreachable, fmt.Errorf("all peers unavailable after probe")
	}
	result.orderedPeers = append([]string(nil), orderedPeers...)
	updateConnectingState(gui, func(state *GUIState) {
		state.BusinessEndpoint = "正在探测可用 peer..."
	})

	peerOrderLine := fmt.Sprintf("[telehand] peer_order=%s", formatPeerSelectionForLog(selection.Results, true))
	gui.AddDebugLog(peerOrderLine)
	fmt.Println(peerOrderLine)

	usedNets, precheckErr := collectLocalIPv4NetsFn()
	if precheckErr != nil {
		msg := fmt.Sprintf("[telehand] startup precheck warning: collect local networks failed: %v", precheckErr)
		gui.AddDebugLog(msg)
		fmt.Println(msg)
	}
	candidates := chooseCandidatesFn(networkHash, role, usedNets)
	if len(candidates) == 0 {
		return result, ErrorCodeRouteConflictDetected, fmt.Errorf("no available subnet candidates")
	}
	candidates = reorderCandidatesByPreferredSubnet(candidates, preferredSubnet)

	var (
		lastErr          error
		lastCode         = ErrorCodePeerUnreachable
		sawRouteConflict bool
	)

	for attempt, candidate := range candidates {
		if isStopRequested(stop) {
			return result, "", errSessionInterrupted
		}
		candidateMsg := fmt.Sprintf("[telehand] startup candidate %d/%d subnet=%s local=%s", attempt+1, len(candidates), candidate.SubnetCIDR, candidate.LocalCIDR)
		gui.AddDebugLog(candidateMsg)
		fmt.Println(candidateMsg)
		updateConnectingState(gui, func(state *GUIState) {
			state.VirtualSubnet = candidate.SubnetCIDR
			state.BusinessEndpoint = "业务端未连接，继续探测中..."
		})

		subnetShouldSwitch := false
		for peerIdx, peer := range orderedPeers {
			if isStopRequested(stop) {
				return result, "", errSessionInterrupted
			}
			peerAttemptMsg := fmt.Sprintf("[telehand] peer attempt=%d/%d peer=%s subnet=%s", peerIdx+1, len(orderedPeers), maskPeerAddress(peer), candidate.SubnetCIDR)
			gui.AddDebugLog(peerAttemptMsg)
			fmt.Println(peerAttemptMsg)

			peerCfg := *cfg
			peerCfg.Peers = []string{peer}
			updateConnectingState(gui, func(state *GUIState) {
				state.CurrentPeer = peer
				state.VirtualSubnet = candidate.SubnetCIDR
				state.BusinessEndpoint = fmt.Sprintf("正在连接 peer %d/%d...", peerIdx+1, len(orderedPeers))
			})

			activeET := newEasyTierFn(func(line string) {
				sanitized := sanitizeSensitiveLog(line, cfg.NetworkSecret)
				gui.AddDebugLog(sanitized)
				fmt.Println(sanitized)
				if strings.Contains(sanitized, "peer connection removed.") {
					extra := "[telehand] event_peer_connection_removed source=easytier-core detail=core reported peer transport removed; waiting snapshot/event reconciliation"
					gui.AddDebugLog(extra)
					fmt.Println(extra)
				}
			})
			setRuntimeET(activeET)

			startErr := easyTierStartFn(activeET, &peerCfg, EasyTierStartOptions{IPv4CIDR: candidate.LocalCIDR})
			if startErr != nil {
				errCode := classifyEasyTierError(startErr, activeET.Logs(), ErrorCodeEasyTierStartFailed)
				lastErr = startErr
				lastCode = errCode
				if errCode == ErrorCodeTUNPermissionDenied || errCode == ErrorCodeAuthFailed {
					return result, errCode, startErr
				}
				logCandidateDecision(gui, cliOnly, attempt+1, len(candidates), candidate.SubnetCIDR, "warn", "peer_fallback_next", fmt.Sprintf("peer=%s start failed: %v", maskPeerAddress(peer), startErr))
				updateConnectingReason(gui, "peer_fallback_next")
				activeET.Stop()
				setRuntimeET(nil)
				continue
			}

			virtIP, waitErr := waitForEasyTierIP(stop, activeET, EasyTierWaitIPTimeout)
			if waitErr != nil {
				if errors.Is(waitErr, errSessionInterrupted) {
					activeET.Stop()
					setRuntimeET(nil)
					return result, "", errSessionInterrupted
				}
				errCode := classifyEasyTierError(waitErr, activeET.Logs(), ErrorCodeEasyTierIPTimeout)
				lastErr = waitErr
				lastCode = errCode
				activeET.Stop()
				setRuntimeET(nil)
				if !isRetryableNetworkError(errCode) {
					return result, errCode, waitErr
				}
				logCandidateDecision(gui, cliOnly, attempt+1, len(candidates), candidate.SubnetCIDR, "warn", "peer_fallback_next", fmt.Sprintf("peer=%s wait ip failed: %v", maskPeerAddress(peer), waitErr))
				updateConnectingReason(gui, "peer_fallback_next")
				continue
			}

			tunDevice, devErr := interfaceByIPv4Fn(virtIP)
			if devErr != nil {
				lastErr = devErr
				lastCode = ErrorCodeRouteConflictDetected
				activeET.Stop()
				setRuntimeET(nil)
				logCandidateDecision(gui, cliOnly, attempt+1, len(candidates), candidate.SubnetCIDR, "warn", "peer_fallback_next", fmt.Sprintf("peer=%s tun detect failed: %v", maskPeerAddress(peer), devErr))
				updateConnectingReason(gui, "peer_fallback_next")
				continue
			}

			peerWaitDeadline := time.Time{}
			if bootstrapWaitTimeout > 0 {
				peerWaitDeadline = time.Now().Add(bootstrapWaitTimeout)
			}

		peerEvaluateLoop:
			for {
				checkResult := evaluateCandidateConnectivityFn(activeET, tunDevice, apiPort, checkCfg, deps, stop, func(check, reason, detail string) {
					logCandidateDecision(gui, cliOnly, attempt+1, len(candidates), candidate.SubnetCIDR, check, reason, detail)
					updateConnectingReason(gui, reason)
				})
				if errors.Is(checkResult.lastProbeErr, errSessionInterrupted) {
					if deps.removeHostRouteForTarget != nil && checkResult.hostRouteInstalled && checkResult.targetIP != "" {
						_ = deps.removeHostRouteForTarget(checkResult.targetIP, tunDevice)
					}
					activeET.Stop()
					setRuntimeET(nil)
					return result, "", errSessionInterrupted
				}
				logCandidateDecision(
					gui,
					cliOnly,
					attempt+1,
					len(candidates),
					candidate.SubnetCIDR,
					"warn",
					"candidate_eval",
					fmt.Sprintf(
						"peer=%s peer_ready=%t probe_success=%t non_self_present=%t peer_class=%s peer_query_failures=%d target=%s",
						maskPeerAddress(peer),
						checkResult.peerReady,
						checkResult.probeSuccess,
						checkResult.nonSelfPresent,
						checkResult.peerClass,
						checkResult.peerQueryFailures,
						strings.TrimSpace(checkResult.targetIP),
					),
				)

				if checkResult.peerReady && checkResult.probeSuccess {
					updateConnectingState(gui, func(state *GUIState) {
						state.BusinessEndpoint = "业务端已连接，准备进入 running..."
						state.LastSwitchReason = "peer_ready"
					})
					result.et = activeET
					result.virtIP = virtIP
					result.baseline = SessionBaseline{
						TunDevice:   tunDevice,
						VirtualCIDR: candidate.SubnetCIDR,
						NetworkHash: networkHash,
					}
					result.activeHostRoutePeer = checkResult.targetIP
					result.selectedPeer = peer
					return result, "", nil
				}

				if checkResult.routeMismatchDetail != "" && checkResult.peerQueryFailures >= checkCfg.maxChecks {
					sawRouteConflict = true
					lastErr = fmt.Errorf("route conflict evidence: %s, peer_query_failed=%d", checkResult.routeMismatchDetail, checkResult.peerQueryFailures)
					lastCode = ErrorCodeRouteConflictDetected
					logCandidateDecision(gui, cliOnly, attempt+1, len(candidates), candidate.SubnetCIDR, "conflict", "route_conflict_detected", lastErr.Error())
					updateConnectingReason(gui, "route_conflict_detected")
					if deps.removeHostRouteForTarget != nil && checkResult.hostRouteInstalled && checkResult.targetIP != "" {
						_ = deps.removeHostRouteForTarget(checkResult.targetIP, tunDevice)
					}
					activeET.Stop()
					setRuntimeET(nil)
					subnetShouldSwitch = true
					break peerEvaluateLoop
				}

				keepCurrentPeerWaiting := !checkResult.peerReady &&
					checkResult.nonSelfPresent &&
					checkResult.peerQueryFailures == 0 &&
					(checkResult.peerClass == peerClassBootstrapOnly || checkResult.peerClass == peerClassBusinessPeerWaitingIP)
				if keepCurrentPeerWaiting {
					if !peerWaitDeadline.IsZero() && time.Now().After(peerWaitDeadline) {
						lastCode = ErrorCodePeerUnreachable
						lastErr = fmt.Errorf("business endpoint still not ready after %s", bootstrapWaitTimeout.String())
						if deps.removeHostRouteForTarget != nil && checkResult.hostRouteInstalled && checkResult.targetIP != "" {
							_ = deps.removeHostRouteForTarget(checkResult.targetIP, tunDevice)
						}
						activeET.Stop()
						setRuntimeET(nil)
						logCandidateDecision(
							gui,
							cliOnly,
							attempt+1,
							len(candidates),
							candidate.SubnetCIDR,
							"warn",
							"peer_fallback_next",
							fmt.Sprintf("peer=%s bootstrap wait timeout=%s", maskPeerAddress(peer), bootstrapWaitTimeout.String()),
						)
						updateConnectingReason(gui, "peer_fallback_next")
						break peerEvaluateLoop
					}
					logCandidateDecision(
						gui,
						cliOnly,
						attempt+1,
						len(candidates),
						candidate.SubnetCIDR,
						"warn",
						"peer_wait_business_endpoint",
						fmt.Sprintf("peer=%s class=%s keep current peer and wait", maskPeerAddress(peer), checkResult.peerClass),
					)
					updateConnectingReason(gui, "peer_wait_business_endpoint")
					if !sleepWithStop(stop, checkCfg.pollInterval) {
						if deps.removeHostRouteForTarget != nil && checkResult.hostRouteInstalled && checkResult.targetIP != "" {
							_ = deps.removeHostRouteForTarget(checkResult.targetIP, tunDevice)
						}
						activeET.Stop()
						setRuntimeET(nil)
						return result, "", errSessionInterrupted
					}
					continue peerEvaluateLoop
				}

				lastCode = ErrorCodePeerUnreachable
				if checkResult.peerReady && checkResult.lastProbeErr != nil {
					lastErr = fmt.Errorf("peer ready but connectivity probe failed: %v", checkResult.lastProbeErr)
				} else if !checkResult.peerReady {
					lastErr = fmt.Errorf("peer not ready")
				} else {
					lastErr = fmt.Errorf("peer probe failed without explicit conflict")
				}
				if deps.removeHostRouteForTarget != nil && checkResult.hostRouteInstalled && checkResult.targetIP != "" {
					_ = deps.removeHostRouteForTarget(checkResult.targetIP, tunDevice)
				}
				activeET.Stop()
				setRuntimeET(nil)
				logCandidateDecision(gui, cliOnly, attempt+1, len(candidates), candidate.SubnetCIDR, "warn", "peer_fallback_next", fmt.Sprintf("peer=%s %v", maskPeerAddress(peer), lastErr))
				updateConnectingReason(gui, "peer_fallback_next")
				break peerEvaluateLoop
			}
			if subnetShouldSwitch {
				break
			}
		}

		logCandidateDecision(gui, cliOnly, attempt+1, len(candidates), candidate.SubnetCIDR, "warn", "peer_all_failed_switch_subnet", fmt.Sprintf("peer_count=%d", len(orderedPeers)))
		updateConnectingReason(gui, "peer_all_failed_switch_subnet")
		if subnetShouldSwitch {
			continue
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("subnet candidate exhausted")
	}
	if sawRouteConflict && lastCode == "" {
		lastCode = ErrorCodeRouteConflictDetected
	}
	if lastCode == "" {
		lastCode = ErrorCodePeerUnreachable
	}
	return result, lastCode, lastErr
}

func isStopRequested(stop <-chan struct{}) bool {
	if stop == nil {
		return false
	}
	select {
	case <-stop:
		return true
	default:
		return false
	}
}

func sleepWithStop(stop <-chan struct{}, d time.Duration) bool {
	if d <= 0 {
		return !isStopRequested(stop)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-stop:
		return false
	}
}

func waitForEasyTierIP(stop <-chan struct{}, et *EasyTier, timeout time.Duration) (string, error) {
	type waitResult struct {
		ip  string
		err error
	}
	ch := make(chan waitResult, 1)
	go func() {
		ip, err := easyTierWaitForIPFn(et, timeout)
		ch <- waitResult{ip: ip, err: err}
	}()

	select {
	case <-stop:
		if et != nil {
			et.Stop()
		}
		return "", errSessionInterrupted
	case res := <-ch:
		return res.ip, res.err
	}
}

func candidateEvalWindow(cfg candidateCheckConfig) time.Duration {
	checks := cfg.maxChecks
	if checks <= 0 {
		checks = 1
	}
	interval := cfg.pollInterval
	if interval <= 0 {
		interval = StatePollInterval
	}
	window := time.Duration(checks) * interval
	if cfg.probeTimout > 0 {
		window += time.Duration(checks) * cfg.probeTimout
	}
	if window <= 0 {
		return interval
	}
	return window
}

func reorderCandidatesByPreferredSubnet(candidates []IPv4Candidate, preferred string) []IPv4Candidate {
	target := strings.TrimSpace(preferred)
	if target == "" || len(candidates) <= 1 {
		return candidates
	}
	idx := -1
	for i, c := range candidates {
		if strings.EqualFold(strings.TrimSpace(c.SubnetCIDR), target) {
			idx = i
			break
		}
	}
	if idx <= 0 {
		return candidates
	}
	out := make([]IPv4Candidate, 0, len(candidates))
	out = append(out, candidates[idx])
	out = append(out, candidates[:idx]...)
	out = append(out, candidates[idx+1:]...)
	return out
}

func updateConnectingState(gui *GUIServer, apply func(*GUIState)) {
	if gui == nil || apply == nil {
		return
	}
	state := gui.GetState()
	if state.Phase != "connecting" {
		return
	}
	apply(&state)
	gui.SetState(state)
}

func updateConnectingReason(gui *GUIServer, reason string) {
	status := connectingStatusFromReason(reason)
	if status == "" {
		return
	}
	updateConnectingState(gui, func(state *GUIState) {
		state.LastSwitchReason = strings.TrimSpace(reason)
		state.BusinessEndpoint = status
	})
}

func connectingStatusFromReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case "bootstrap_connected":
		return "已连接 bootstrap，保持当前连接等待业务端..."
	case "business_endpoint_waiting":
		return "业务端还未连接（等待对端）"
	case "peer_not_ready":
		return "等待对端业务端连接..."
	case "peer_wait_business_endpoint":
		return "已连接 bootstrap，保持当前连接等待业务端..."
	case "peer_fallback_next":
		return "当前 peer 不可用，切换下一个..."
	case "peer_all_failed_switch_subnet":
		return "当前子网无可用 peer，切换子网..."
	case "route_conflict_detected":
		return "检测到路由冲突，切换子网..."
	case "peer_ready":
		return "业务端已连接"
	default:
		return ""
	}
}

func setSessionError(gui *GUIServer, apiPort int, code, msg string) {
	state := gui.GetState()
	state.Phase = "error"
	state.APIPort = apiPort
	state.Error = msg
	state.ErrorCode = code
	gui.SetState(state)
}

func formatConnectError(code string, err error) string {
	base := fmt.Sprintf("Failed to connect: %v", err)
	switch code {
	case ErrorCodeAuthFailed:
		return "Failed to connect: authentication failed (network name/secret mismatch)"
	case ErrorCodePeerUnreachable:
		return "Failed to connect: peer unreachable"
	case ErrorCodeTUNPermissionDenied:
		return "Failed to connect: TUN permission denied (please run with administrator/root privilege)"
	case ErrorCodeEasyTierIPTimeout:
		return "Failed to connect: timeout waiting for virtual IP"
	case ErrorCodeRouteConflictDetected:
		return "Failed to connect: route/subnet conflict detected before running"
	default:
		return base
	}
}

func isRetryableNetworkError(code string) bool {
	switch code {
	case ErrorCodeEasyTierIPTimeout, ErrorCodePeerUnreachable:
		return true
	default:
		return false
	}
}

func exitCodeFromErrorCode(code string, fallback int) int {
	switch code {
	case ErrorCodeEasyTierIPTimeout, ErrorCodeAuthFailed, ErrorCodePeerUnreachable, ErrorCodeWindowsFirewallBlocked, ErrorCodeRouteConflictDetected:
		return ExitCodeNetwork
	case ErrorCodeEasyTierStartFailed, ErrorCodeWindowsTUNInitFailed, ErrorCodeWindowsAdminCheckFail, ErrorCodeWindowsNotAdmin, ErrorCodeTUNPermissionDenied:
		return ExitCodeService
	default:
		return fallback
	}
}

func shouldCheckRouteOwnership() bool {
	return runtime.GOOS == "darwin" || runtime.GOOS == "windows"
}

func logCandidateDecision(gui *GUIServer, cliOnly bool, attempt, total int, subnet, result, reason, detail string) {
	_ = cliOnly
	bypassLimiter := strings.HasPrefix(strings.TrimSpace(reason), "event_")
	key := strings.TrimSpace(subnet) + "|" + strings.TrimSpace(result) + "|" + strings.TrimSpace(reason) + "|" + strings.TrimSpace(detail)
	now := time.Now()
	if !bypassLimiter {
		candidateLogLimiterMu.Lock()
		last := candidateLogLimiterSeen[key]
		if !last.IsZero() && now.Sub(last) < candidateLogLimiterTTL {
			candidateLogLimiterMu.Unlock()
			return
		}
		candidateLogLimiterSeen[key] = now
		candidateLogLimiterMu.Unlock()
	}

	line := fmt.Sprintf("[telehand] candidate=%d/%d subnet=%s result=%s reason=%s detail=%s", attempt, total, subnet, result, reason, detail)
	gui.AddDebugLog(line)
	fmt.Println(line)
}

func formatReadinessContext(readiness PeerReadiness) string {
	class := strings.TrimSpace(readiness.PeerClass)
	if class == "" {
		class = peerClassNone
	}
	peerIDs := make([]string, 0, len(readiness.PeerIDs))
	for _, id := range readiness.PeerIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			peerIDs = append(peerIDs, id)
		}
	}
	return fmt.Sprintf(
		"ready=%t class=%s non_self=%t peer_id=%s peer_host=%s target_ip=%s peer_ids=%s",
		readiness.Ready,
		class,
		readiness.NonSelfPresent,
		valueOrDash(strings.TrimSpace(readiness.PeerID)),
		valueOrDash(strings.TrimSpace(readiness.PeerHostname)),
		valueOrDash(strings.TrimSpace(readiness.TargetIP)),
		valueOrDash(strings.Join(peerIDs, ",")),
	)
}

func evaluateCandidateConnectivity(
	et *EasyTier,
	tunDevice string,
	apiPort int,
	cfg candidateCheckConfig,
	deps sessionDeps,
	stop <-chan struct{},
	logFn func(result, reason, detail string),
) candidateCheckResult {
	result := candidateCheckResult{}
	if et == nil {
		checks := cfg.maxChecks
		if checks <= 0 {
			checks = 1
		}
		for i := 0; i < checks; i++ {
			if isStopRequested(stop) {
				result.lastProbeErr = errSessionInterrupted
				return result
			}
			readiness, readyErr := deps.queryPeerReadiness(et)
			if readyErr != nil {
				result.peerQueryFailures++
				logFn("warn", "peer_query_failed", readyErr.Error())
				if !sleepWithStop(stop, cfg.pollInterval) {
					result.lastProbeErr = errSessionInterrupted
					return result
				}
				continue
			}
			result.peerReady = readiness.Ready
			result.nonSelfPresent = result.nonSelfPresent || readiness.NonSelfPresent
			result.targetIP = strings.TrimSpace(readiness.TargetIP)
			if readiness.PeerClass != "" && readiness.PeerClass != peerClassNone {
				result.peerClass = readiness.PeerClass
			}
			if !readiness.Ready {
				context := formatReadinessContext(readiness)
				if readiness.PeerClass == peerClassBootstrapOnly {
					logFn("warn", "bootstrap_connected", "bootstrap peer connected, business endpoint not ready; "+context)
					if cfg.returnOnBootstrap && readiness.NonSelfPresent {
						return result
					}
				} else if readiness.PeerClass == peerClassBusinessPeerWaitingIP || readiness.NonSelfPresent {
					logFn("warn", "business_endpoint_waiting", "peer connected but virtual ip not ready; "+context)
					if cfg.returnOnBootstrap && readiness.NonSelfPresent {
						return result
					}
				} else {
					logFn("warn", "peer_not_ready", "peer list empty; "+context)
				}
				if !sleepWithStop(stop, cfg.pollInterval) {
					result.lastProbeErr = errSessionInterrupted
					return result
				}
				continue
			}
			targetIP := result.targetIP
			if deps.shouldCheckRouteOwner() && targetIP != "" {
				routeIface, routeErr := deps.routeInterfaceForTarget(targetIP)
				if routeErr != nil {
					logFn("warn", "route_check_failed", routeErr.Error())
				} else if !strings.EqualFold(strings.TrimSpace(routeIface), strings.TrimSpace(tunDevice)) {
					result.routeMismatchDetail = fmt.Sprintf("target=%s route_if=%s tun_if=%s", targetIP, routeIface, tunDevice)
					logFn("warn", "route_mismatch", result.routeMismatchDetail)
				}
			}
			if targetIP == "" {
				result.lastProbeErr = fmt.Errorf("target peer virtual ip is empty")
				logFn("warn", "probe_timeout", result.lastProbeErr.Error())
				if !sleepWithStop(stop, cfg.pollInterval) {
					result.lastProbeErr = errSessionInterrupted
					return result
				}
				continue
			}
			if probeErr := deps.probePeerVirtualIP(targetIP, apiPort, cfg.probeTimout); probeErr == nil {
				result.probeSuccess = true
				logFn("pass", "peer_ready", fmt.Sprintf("target=%s", targetIP))
				return result
			} else {
				result.lastProbeErr = probeErr
				logFn("warn", "probe_timeout", probeErr.Error())
			}
			if !sleepWithStop(stop, cfg.pollInterval) {
				result.lastProbeErr = errSessionInterrupted
				return result
			}
		}
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), candidateEvalWindow(cfg))
	defer cancel()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-stop:
			cancel()
		case <-done:
		}
	}()

	snapshots, events := startStatePollerFn(et, cfg.pollInterval, ctx)
	maxChecks := cfg.maxChecks
	if maxChecks <= 0 {
		maxChecks = 1
	}

	applySnapshot := func(snap EasyTierSnapshot) bool {
		readiness := snap.Readiness
		result.peerReady = readiness.Ready
		result.nonSelfPresent = result.nonSelfPresent || readiness.NonSelfPresent
		result.targetIP = strings.TrimSpace(readiness.TargetIP)
		if readiness.PeerClass != "" && readiness.PeerClass != peerClassNone {
			result.peerClass = readiness.PeerClass
		}
		if !result.peerReady {
			context := formatReadinessContext(readiness)
			if readiness.PeerClass == peerClassBootstrapOnly {
				logFn("warn", "bootstrap_connected", "bootstrap peer connected, business endpoint not ready; "+context)
				if cfg.returnOnBootstrap && readiness.NonSelfPresent {
					return true
				}
			} else if readiness.PeerClass == peerClassBusinessPeerWaitingIP {
				logFn("warn", "business_endpoint_waiting", "peer connected but virtual ip not ready; "+context)
				if cfg.returnOnBootstrap && readiness.NonSelfPresent {
					return true
				}
			} else if readiness.NonSelfPresent {
				logFn("warn", "business_endpoint_waiting", "non-self peer present but virtual ip not ready; "+context)
				if cfg.returnOnBootstrap {
					return true
				}
			} else {
				logFn("warn", "peer_not_ready", "peer list empty; "+context)
			}
			return false
		}

		targetIP := result.targetIP
		if deps.shouldCheckRouteOwner() && targetIP != "" {
			routeIface, routeErr := deps.routeInterfaceForTarget(targetIP)
			if routeErr != nil {
				logFn("warn", "route_check_failed", routeErr.Error())
			} else if !strings.EqualFold(strings.TrimSpace(routeIface), strings.TrimSpace(tunDevice)) {
				result.routeMismatchDetail = fmt.Sprintf("target=%s route_if=%s tun_if=%s", targetIP, routeIface, tunDevice)
				logFn("warn", "route_mismatch", result.routeMismatchDetail)
			}
		}

		if targetIP == "" {
			result.lastProbeErr = fmt.Errorf("target peer virtual ip is empty")
			logFn("warn", "probe_timeout", result.lastProbeErr.Error())
			return false
		}

		if !result.hostRouteInstalled {
			if deps.addHostRouteForTarget == nil {
				// Route pinning disabled in tests or custom dependency wiring.
			} else if err := deps.addHostRouteForTarget(targetIP, tunDevice); err != nil {
				logFn("warn", "route_host_add_failed", err.Error())
			} else {
				result.hostRouteInstalled = true
				logFn("pass", "route_host_bound", fmt.Sprintf("target=%s tun_if=%s", targetIP, tunDevice))
			}
		}

		if probeErr := deps.probePeerVirtualIP(targetIP, apiPort, cfg.probeTimout); probeErr == nil {
			result.probeSuccess = true
			logFn("pass", "peer_ready", fmt.Sprintf("target=%s", targetIP))
			return true
		} else {
			result.lastProbeErr = probeErr
			logFn("warn", "probe_timeout", probeErr.Error())
			return false
		}
	}

	for {
		select {
		case <-ctx.Done():
			if isStopRequested(stop) {
				result.lastProbeErr = errSessionInterrupted
			}
			return result
		case evt, ok := <-events:
			if !ok {
				return result
			}
			if evt.Type == EasyTierEventProcessExit {
				result.lastProbeErr = fmt.Errorf("easytier process exited before candidate became ready")
				logFn("warn", "event_process_exit", result.lastProbeErr.Error())
				return result
			}
			if evt.Type == EasyTierEventPeerAdded {
				result.nonSelfPresent = true
				if result.peerClass == "" || result.peerClass == peerClassNone {
					if evt.PeerClass != "" && evt.PeerClass != peerClassNone {
						result.peerClass = evt.PeerClass
					} else {
						result.peerClass = peerClassBusinessPeerWaitingIP
					}
				}
				logFn("warn", "event_peer_added", fmt.Sprintf("source=diff-engine peer_id=%s peer_class=%s", valueOrDash(evt.PeerID), valueOrDash(evt.PeerClass)))
				if cfg.returnOnBootstrap {
					logFn("warn", "business_endpoint_waiting", "non-self peer observed via event; virtual ip not ready")
					return result
				}
			}
			if evt.Type == EasyTierEventPeerRemoved {
				logFn("warn", "event_peer_removed", fmt.Sprintf("source=diff-engine peer_id=%s peer_class=%s", valueOrDash(evt.PeerID), valueOrDash(evt.PeerClass)))
			}
			if evt.Type == EasyTierEventSnapshotError && evt.Err != nil {
				result.peerQueryFailures++
				logFn("warn", "event_snapshot_error", evt.Err.Error())
				if result.peerQueryFailures >= maxChecks {
					return result
				}
			}
		case snap, ok := <-snapshots:
			if !ok {
				return result
			}
			if applySnapshot(snap) {
				return result
			}
		}
	}
}

func runRunningStateGuard(
	gui *GUIServer,
	cliOnly bool,
	et *EasyTier,
	tunDevice string,
	apiPort int,
	cfg runningGuardConfig,
	deps sessionDeps,
	stop <-chan struct{},
	reconnect chan<- string,
) {
	failures := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	snapshots, events := startStatePollerFn(et, StatePollInterval, ctx)
	removedAt := make([]time.Time, 0, PeerRemovedBurstCount+2)

	activeHostRouteTarget := ""
	requestReconnect := func(reason, detail string) bool {
		logCandidateDecision(gui, cliOnly, 1, 1, "running", "warn", "peer_probe_degraded", fmt.Sprintf("reason=%s detail=%s", reason, detail))
		select {
		case reconnect <- strings.TrimSpace(reason):
			return true
		default:
			return false
		}
	}
	evaluate := func(readiness PeerReadiness) bool {
		failed := false
		reason := ""
		detail := ""
		targetIP := strings.TrimSpace(readiness.TargetIP)

		if !readiness.Ready {
			if readiness.PeerClass == peerClassBootstrapOnly {
				failures = 0
				logCandidateDecision(gui, cliOnly, 1, 1, "running", "warn", "bootstrap_connected", fmt.Sprintf("bootstrap peer connected id=%s hostname=%s, business endpoint not ready", readiness.PeerID, readiness.PeerHostname))
				return false
			}
			if readiness.PeerClass == peerClassBusinessPeerWaitingIP || readiness.NonSelfPresent {
				failures = 0
				logCandidateDecision(gui, cliOnly, 1, 1, "running", "warn", "business_endpoint_waiting", "peer connected but virtual ip not ready")
				return false
			}
			failed = true
			reason = "peer_query_failed"
			detail = "peer list empty"
		} else {
			if targetIP != "" && targetIP != activeHostRouteTarget {
				if deps.removeHostRouteForTarget != nil && activeHostRouteTarget != "" {
					_ = deps.removeHostRouteForTarget(activeHostRouteTarget, tunDevice)
					activeHostRouteTarget = ""
				}
				if deps.addHostRouteForTarget == nil {
					// noop
				} else if err := deps.addHostRouteForTarget(targetIP, tunDevice); err != nil {
					logCandidateDecision(gui, cliOnly, 1, 1, "running", "warn", "route_host_add_failed", err.Error())
				} else {
					activeHostRouteTarget = targetIP
				}
			}

			if deps.shouldCheckRouteOwner() && targetIP != "" {
				iface, routeErr := deps.routeInterfaceForTarget(targetIP)
				if routeErr != nil {
					logCandidateDecision(gui, cliOnly, 1, 1, "running", "warn", "route_check_failed", routeErr.Error())
				} else if !strings.EqualFold(strings.TrimSpace(iface), strings.TrimSpace(tunDevice)) {
					logCandidateDecision(gui, cliOnly, 1, 1, "running", "warn", "route_mismatch", fmt.Sprintf("target=%s route_if=%s tun_if=%s", targetIP, iface, tunDevice))
				}
			}
			if targetIP == "" {
				failed = true
				reason = "probe_timeout"
				detail = "target peer virtual ip is empty"
			} else if probeErr := deps.probePeerVirtualIP(targetIP, apiPort, cfg.probeTimeout); probeErr != nil {
				failed = true
				reason = "probe_timeout"
				detail = probeErr.Error()
			}
		}

		if !failed {
			failures = 0
			logCandidateDecision(gui, cliOnly, 1, 1, "running", "pass", "peer_ready", fmt.Sprintf("target=%s", targetIP))
			return false
		}

		failures++
		logCandidateDecision(gui, cliOnly, 1, 1, "running", "warn", reason, fmt.Sprintf("consecutive_failures=%d/%d %s", failures, cfg.consecutiveFailed, detail))
		if failures < cfg.consecutiveFailed {
			return false
		}

		return requestReconnect("peer_probe_degraded", detail)
	}

	for {
		select {
		case <-stop:
			if deps.removeHostRouteForTarget != nil && activeHostRouteTarget != "" {
				_ = deps.removeHostRouteForTarget(activeHostRouteTarget, tunDevice)
			}
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			if evt.Type == EasyTierEventProcessExit {
				_ = requestReconnect("peer_probe_degraded", "easytier process exited")
				return
			}
			if evt.Type == EasyTierEventPeerRemoved {
				now := time.Now()
				removedAt = append(removedAt, now)
				cutoff := now.Add(-PeerRemovedBurstWindow)
				write := 0
				for _, t := range removedAt {
					if t.After(cutoff) {
						removedAt[write] = t
						write++
					}
				}
				removedAt = removedAt[:write]
				if len(removedAt) >= PeerRemovedBurstCount {
					detail := fmt.Sprintf("peer_removed burst=%d window=%s", len(removedAt), PeerRemovedBurstWindow.String())
					_ = requestReconnect("peer_probe_degraded", detail)
					return
				}
			}
			if evt.Type == EasyTierEventSnapshotError && evt.Err != nil {
				if evaluate(PeerReadiness{Ready: false, NonSelfPresent: false, PeerClass: peerClassNone}) {
					return
				}
			}
		case snap, ok := <-snapshots:
			if !ok {
				return
			}
			if evaluate(snap.Readiness) {
				return
			}
		}
	}
}

func probePeerVirtualIP(ip string, port int, timeout time.Duration) error {
	target := strings.TrimSpace(ip)
	if net.ParseIP(target) == nil {
		return fmt.Errorf("invalid peer ip: %q", ip)
	}
	addr := fmt.Sprintf("%s:%d", target, port)
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

func printPeerInfoLoop(stop <-chan struct{}, fetch func() (PeerInfoSnapshot, error)) {
	printSnapshot := func() {
		snapshot, err := fetch()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Peer info update failed: %v\n", err)
			return
		}
		if len(snapshot.Peers) == 0 {
			return
		}
		printPeerSnapshot(snapshot)
	}

	printSnapshot()
	ticker := time.NewTicker(defaultPeerInfoRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			printSnapshot()
		}
	}
}

func printPeerSnapshot(snapshot PeerInfoSnapshot) {
	title := "Peer Info"
	owner := strings.TrimSpace(snapshot.NetworkOwner)
	hash := strings.TrimSpace(snapshot.NetworkHash)
	if owner != "" && hash != "" {
		title += fmt.Sprintf(" (%s:%s)", owner, hash)
	} else if hash != "" {
		title += fmt.Sprintf(" (%s)", hash)
	}
	fmt.Printf("\n%s (%s)\n", title, snapshot.UpdatedAt)
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "Virtual IPv4\tHostname\tRoute Cost\tProtocol\tLatency\tUpload\tDownload\tLoss Rate\tVersion\tRole\tLocal")
	for _, p := range snapshot.Peers {
		local := ""
		if p.IsSelf {
			local = "*"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			p.VirtualIPv4,
			p.Hostname,
			p.RouteCost,
			p.Protocol,
			p.Latency,
			p.Upload,
			p.Download,
			p.LossRate,
			p.Version,
			p.Role,
			local,
		)
	}
	w.Flush()
}

func networkOwnerFromNetworkName(networkName string) string {
	name := strings.TrimSpace(networkName)
	if name == "" {
		return ""
	}
	parts := strings.Split(name, ":")
	if len(parts) < 2 {
		return name
	}
	owner := strings.TrimSpace(parts[len(parts)-1])
	if owner == "" {
		return name
	}
	return owner
}
