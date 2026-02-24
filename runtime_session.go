package main

import (
	"errors"
	"fmt"
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
		runtimeMu      sync.RWMutex
		runtimeET      *EasyTier
		runtimeNetHash string
	)
	gui.SetPeerInfoProvider(func() (PeerInfoSnapshot, error) {
		runtimeMu.RLock()
		et := runtimeET
		networkHash := runtimeNetHash
		runtimeMu.RUnlock()
		if et == nil {
			return PeerInfoSnapshot{
				UpdatedAt:   time.Now().Format(time.RFC3339),
				NetworkHash: networkHash,
				Peers:       []PeerInfo{},
			}, nil
		}
		snapshot, err := et.QueryPeerInfo(role)
		if err != nil {
			return snapshot, err
		}
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

	networkHash := computeNetworkHash(cfg.NetworkName, cfg.NetworkSecret)
	state = gui.GetState()
	state.NetworkHash = networkHash
	gui.SetState(state)
	runtimeMu.Lock()
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

		if shouldCheckRouteOwnership() {
			routeIface, routeErr := routeInterfaceForTarget(candidate.ExpectedPeerIP)
			if routeErr != nil {
				lastErr = routeErr
				lastCode = ErrorCodeRouteConflictDetected
				activeET.Stop()
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(routeIface), strings.TrimSpace(tunDevice)) {
				lastErr = fmt.Errorf("route mismatch: target=%s route_if=%s tun_if=%s", candidate.ExpectedPeerIP, routeIface, tunDevice)
				lastCode = ErrorCodeRouteConflictDetected
				activeET.Stop()
				continue
			}
		}

		lastErr = nil
		lastCode = ""
		break
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
	fmt.Printf("Session baseline: tun=%s subnet=%s network_hash=%s\n", baseline.TunDevice, baseline.VirtualCIDR, baseline.NetworkHash)
	state = gui.GetState()
	state.Phase = "running"
	state.VirtIP = virtIP
	state.Error = ""
	state.ErrorCode = ""
	gui.SetState(state)
	fmt.Printf("State: connecting -> running\n")
	fmt.Printf("API server reachable at http://%s:%d\n", virtIP, apiPort)

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
		snapshot.NetworkHash = networkHash
		return snapshot, nil
	})

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	stopCh := make(chan struct{}, 1)
	go func() { <-sig; stopCh <- struct{}{} }()
	go func() { gui.WaitForConfig(); stopCh <- struct{}{} }()

	<-stopCh
	fmt.Println("State: stopping")
	stopPeerPrint <- struct{}{}
	api.Stop()
	if activeET != nil {
		activeET.Stop()
	}
	gui.Stop()
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
	if strings.TrimSpace(snapshot.NetworkHash) != "" {
		title += fmt.Sprintf(" (网络名称:%s)", snapshot.NetworkHash)
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
