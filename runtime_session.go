package main

import (
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
	queryPeerReadiness      func(*EasyTier) (PeerReadiness, error)
	routeInterfaceForTarget func(string) (string, error)
	probePeerVirtualIP      func(string, int, time.Duration) error
	shouldCheckRouteOwner   func() bool
	sleep                   func(time.Duration)
}

type candidateCheckConfig struct {
	maxChecks   int
	window      time.Duration
	step        time.Duration
	probeTimout time.Duration
}

type runningGuardConfig struct {
	protectionWindow  time.Duration
	checkInterval     time.Duration
	consecutiveFailed int
	probeTimeout      time.Duration
}

type candidateCheckResult struct {
	peerReady           bool
	probeSuccess        bool
	peerQueryFailures   int
	routeMismatchDetail string
	lastProbeErr        error
}

var (
	defaultSessionDeps = sessionDeps{
		queryPeerReadiness:      func(et *EasyTier) (PeerReadiness, error) { return et.QueryPeerReadiness() },
		routeInterfaceForTarget: routeInterfaceForTarget,
		probePeerVirtualIP:      probePeerVirtualIP,
		shouldCheckRouteOwner:   shouldCheckRouteOwnership,
		sleep:                   time.Sleep,
	}
	defaultCandidateCheckConfig = candidateCheckConfig{
		maxChecks:   3,
		window:      5 * time.Second,
		step:        1500 * time.Millisecond,
		probeTimout: 800 * time.Millisecond,
	}
	defaultRunningGuardConfig = runningGuardConfig{
		protectionWindow:  5 * time.Second,
		checkInterval:     2 * time.Second,
		consecutiveFailed: 3,
		probeTimeout:      800 * time.Millisecond,
	}
)

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

	fmt.Printf("Network ready: name=%s secret=%s peers=%s\n", cfg.NetworkName, maskSecret(cfg.NetworkSecret), strings.Join(cfg.Peers, ","))
	fmt.Printf("State: initializing -> connecting (%s)\n", role)

	networkOwner := networkOwnerFromNetworkName(cfg.NetworkName)
	networkHash := computeNetworkHash(cfg.NetworkName, cfg.NetworkSecret)
	state = gui.GetState()
	state.NetworkOwner = networkOwner
	state.NetworkHash = networkHash
	gui.SetState(state)
	runtimeMu.Lock()
	runtimeNetOwner = networkOwner
	runtimeNetHash = networkHash
	runtimeMu.Unlock()

	usedNets, precheckErr := collectLocalIPv4Nets()
	if precheckErr != nil {
		msg := fmt.Sprintf("[telehand] startup precheck warning: collect local networks failed: %v", precheckErr)
		gui.AddDebugLog(msg)
		if cliOnly {
			fmt.Println(msg)
		}
	}
	candidates := chooseCandidates(networkHash, role, usedNets)
	if len(candidates) == 0 {
		errCode := ErrorCodeRouteConflictDetected
		errMsg := formatConnectError(errCode, fmt.Errorf("no available subnet candidates"))
		setSessionError(gui, apiPort, errCode, errMsg)
		fmt.Fprintln(os.Stderr, errMsg)
		api.Stop()
		gui.Stop()
		return exitCodeFromErrorCode(errCode, ExitCodeNetwork)
	}

	var (
		activeET *EasyTier
		virtIP   string
		baseline SessionBaseline
		lastErr  error
		lastCode string
	)
	deps := defaultSessionDeps
	checkCfg := defaultCandidateCheckConfig

connectLoop:
	for attempt, candidate := range candidates {
		msg := fmt.Sprintf("[telehand] startup candidate %d/%d subnet=%s local=%s", attempt+1, len(candidates), candidate.SubnetCIDR, candidate.LocalCIDR)
		gui.AddDebugLog(msg)
		if cliOnly {
			fmt.Println(msg)
		}

		activeET = NewEasyTier(func(line string) {
			sanitized := sanitizeSensitiveLog(line, cfg.NetworkSecret)
			gui.AddDebugLog(sanitized)
			if cliOnly {
				fmt.Println(sanitized)
			}
		})
		runtimeMu.Lock()
		runtimeET = activeET
		runtimeMu.Unlock()

		if err := activeET.Start(cfg, EasyTierStartOptions{IPv4CIDR: candidate.LocalCIDR}); err != nil {
			errCode := classifyEasyTierError(err, activeET.Logs(), ErrorCodeEasyTierStartFailed)
			lastErr = err
			lastCode = errCode
			if errCode == ErrorCodeTUNPermissionDenied || errCode == ErrorCodeAuthFailed {
				break
			}
			continue
		}

		ip, err := activeET.WaitForIP(30 * time.Second)
		if err != nil {
			errCode := classifyEasyTierError(err, activeET.Logs(), ErrorCodeEasyTierIPTimeout)
			lastErr = err
			lastCode = errCode
			activeET.Stop()
			if !isRetryableNetworkError(errCode) {
				break
			}
			continue
		}

		virtIP = ip
		tunDevice, devErr := interfaceByIPv4(virtIP)
		if devErr != nil {
			lastErr = devErr
			lastCode = ErrorCodeRouteConflictDetected
			activeET.Stop()
			continue
		}
		baseline = SessionBaseline{
			TunDevice:   tunDevice,
			VirtualCIDR: candidate.SubnetCIDR,
			NetworkHash: networkHash,
		}

		checkResult := evaluateCandidateConnectivity(activeET, tunDevice, apiPort, checkCfg, deps, func(result, reason, detail string) {
			logCandidateDecision(gui, cliOnly, attempt+1, len(candidates), candidate.SubnetCIDR, result, reason, detail)
		})

		if checkResult.peerReady && checkResult.probeSuccess {
			lastErr = nil
			lastCode = ""
			break connectLoop
		}

		if checkResult.routeMismatchDetail != "" && checkResult.peerQueryFailures >= checkCfg.maxChecks {
			lastErr = fmt.Errorf("route conflict evidence: %s, peer_query_failed=%d", checkResult.routeMismatchDetail, checkResult.peerQueryFailures)
			lastCode = ErrorCodeRouteConflictDetected
			logCandidateDecision(gui, cliOnly, attempt+1, len(candidates), candidate.SubnetCIDR, "conflict", "route_conflict_detected", lastErr.Error())
			activeET.Stop()
			continue
		}

		lastCode = ErrorCodePeerUnreachable
		if checkResult.peerReady && checkResult.lastProbeErr != nil {
			lastErr = fmt.Errorf("peer ready but connectivity probe failed: %v", checkResult.lastProbeErr)
		} else if !checkResult.peerReady {
			lastErr = fmt.Errorf("peer not ready within %s", checkCfg.window)
		} else {
			lastErr = fmt.Errorf("peer probe failed without explicit conflict")
		}
		activeET.Stop()
		continue
	}

	if virtIP == "" {
		errCode := lastCode
		if errCode == "" {
			errCode = ErrorCodeRouteConflictDetected
		}
		if errCode != ErrorCodeRouteConflictDetected && len(candidates) > 1 {
			errCode = ErrorCodeRouteConflictDetected
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("subnet candidate exhausted")
		}
		errMsg := formatConnectError(errCode, lastErr)
		setSessionError(gui, apiPort, errCode, errMsg)
		fmt.Fprintln(os.Stderr, errMsg)
		api.Stop()
		gui.Stop()
		return exitCodeFromErrorCode(errCode, ExitCodeNetwork)
	}

	fmt.Printf("EasyTier virtual IP: %s\n", virtIP)
	fmt.Printf("Session baseline: tun_device=%s virtual_subnet=%s network_hash=%s\n", baseline.TunDevice, baseline.VirtualCIDR, baseline.NetworkHash)
	state = gui.GetState()
	state.Phase = "running"
	state.VirtIP = virtIP
	state.TUNDevice = baseline.TunDevice
	state.VirtualSubnet = baseline.VirtualCIDR
	state.Error = ""
	state.ErrorCode = ""
	gui.SetState(state)
	fmt.Printf("State: connecting -> running\n")
	fmt.Printf("API server reachable at http://%s:%d\n", virtIP, apiPort)
	fmt.Printf("State guard: protection_window=%s, threshold=%d consecutive failures\n", defaultRunningGuardConfig.protectionWindow, defaultRunningGuardConfig.consecutiveFailed)

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

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	stopCh := make(chan struct{}, 1)
	guardStop := make(chan struct{}, 1)
	go func() { <-sig; stopCh <- struct{}{} }()
	go func() { gui.WaitForConfig(); stopCh <- struct{}{} }()
	go runRunningStateGuard(gui, cliOnly, activeET, baseline.TunDevice, apiPort, defaultRunningGuardConfig, deps, guardStop, stopCh)

	<-stopCh
	fmt.Println("State: stopping")
	guardStop <- struct{}{}
	stopPeerPrint <- struct{}{}
	api.Stop()
	if activeET != nil {
		activeET.Stop()
	}
	gui.Stop()
	final := gui.GetState()
	if final.Phase == "error" && final.ErrorCode != "" {
		return exitCodeFromErrorCode(final.ErrorCode, ExitCodeNetwork)
	}
	return ExitCodeOK
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
	line := fmt.Sprintf("[telehand] candidate=%d/%d subnet=%s result=%s reason=%s detail=%s", attempt, total, subnet, result, reason, detail)
	gui.AddDebugLog(line)
	if cliOnly {
		fmt.Println(line)
	}
}

func evaluateCandidateConnectivity(
	et *EasyTier,
	tunDevice string,
	apiPort int,
	cfg candidateCheckConfig,
	deps sessionDeps,
	logFn func(result, reason, detail string),
) candidateCheckResult {
	result := candidateCheckResult{}
	deadline := time.Now().Add(cfg.window)

	for check := 1; time.Now().Before(deadline); check++ {
		if check > 64 {
			break
		}
		readiness, readyErr := deps.queryPeerReadiness(et)
		if readyErr != nil {
			result.peerQueryFailures++
			logFn("warn", "peer_query_failed", readyErr.Error())
			if cfg.step > 0 {
				deps.sleep(cfg.step)
			}
			continue
		}

		result.peerReady = readiness.Ready
		if !result.peerReady {
			logFn("warn", "peer_not_ready", "peer list empty")
			if cfg.step > 0 {
				deps.sleep(cfg.step)
			}
			continue
		}

		targetIP := strings.TrimSpace(readiness.TargetIP)
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
			if cfg.step > 0 {
				deps.sleep(cfg.step)
			}
			continue
		}

		if probeErr := deps.probePeerVirtualIP(targetIP, apiPort, cfg.probeTimout); probeErr == nil {
			result.probeSuccess = true
			logFn("pass", "peer_ready", fmt.Sprintf("target=%s", targetIP))
			break
		} else {
			result.lastProbeErr = probeErr
			logFn("warn", "probe_timeout", probeErr.Error())
		}
		if cfg.step > 0 {
			deps.sleep(cfg.step)
		}
	}
	return result
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
	sessionStop chan<- struct{},
) {
	startedAt := time.Now()
	failures := 0
	ticker := time.NewTicker(cfg.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}

		readiness, err := deps.queryPeerReadiness(et)
		failed := false
		reason := ""
		detail := ""
		targetIP := ""

		if err != nil {
			failed = true
			reason = "peer_query_failed"
			detail = err.Error()
		} else if !readiness.Ready {
			failed = true
			reason = "peer_query_failed"
			detail = "peer list empty"
		} else {
			targetIP = strings.TrimSpace(readiness.TargetIP)
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
			continue
		}

		if time.Since(startedAt) < cfg.protectionWindow {
			failures = 0
			logCandidateDecision(gui, cliOnly, 1, 1, "running", "warn", reason, fmt.Sprintf("within_protection_window: %s", detail))
			continue
		}

		failures++
		logCandidateDecision(gui, cliOnly, 1, 1, "running", "warn", reason, fmt.Sprintf("consecutive_failures=%d/%d %s", failures, cfg.consecutiveFailed, detail))
		if failures < cfg.consecutiveFailed {
			continue
		}

		errMsg := formatConnectError(ErrorCodePeerUnreachable, fmt.Errorf("running health degraded: %s", detail))
		setSessionError(gui, apiPort, ErrorCodePeerUnreachable, errMsg)
		select {
		case sessionStop <- struct{}{}:
		default:
		}
		return
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
	ticker := time.NewTicker(5 * time.Second)
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
