package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"
)

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configStr := fs.String("config", "", "base64 config string to auto-connect")
	noBrowser := fs.Bool("no-browser", false, "do not auto-open browser")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	gui := NewGUIServer(18080)
	apiPort := 0
	api := NewAPIServer("0.0.0.0", 8080, func(log CmdLog) {
		gui.AddLog(log)
	}, func() HealthResp {
		s := gui.GetState()
		return HealthResp{
			Status:    "ok",
			Phase:     s.Phase,
			VirtIP:    s.VirtIP,
			APIPort:   apiPort,
			GUIPort:   gui.Port(),
			Error:     s.Error,
			ErrorCode: s.ErrorCode,
		}
	}, func(cfg string) error {
		return gui.SubmitConfigEncoded(cfg)
	})
	if err := api.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start API server: %v\n", err)
		os.Exit(1)
	}
	apiPort = api.Port()
	fmt.Printf("API server started at http://0.0.0.0:%d\n", apiPort)

	if err := gui.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start GUI: %v\n", err)
		api.Stop()
		os.Exit(1)
	}
	gui.SetState(GUIState{Phase: "config", APIPort: apiPort})
	guiURL := fmt.Sprintf("http://127.0.0.1:%d", gui.Port())
	fmt.Printf("GUI started at %s\n", guiURL)

	if !*noBrowser {
		openBrowser(guiURL)
	}

	if *configStr != "" {
		if err := gui.SubmitConfigEncoded(*configStr); err != nil {
			fmt.Fprintf(os.Stderr, "Invalid --config: %v\n", err)
			api.Stop()
			gui.Stop()
			os.Exit(1)
		}
	}

	cfg := gui.WaitForConfig()
	if cfg == nil {
		fmt.Println("Stopped by user.")
		api.Stop()
		gui.Stop()
		return
	}

	fmt.Println("Starting EasyTier...")
	et := NewEasyTier(func(line string) {
		gui.AddDebugLog(line)
	})
	if err := et.Start(cfg); err != nil {
		errCode := classifyEasyTierError(err, et.Logs(), ErrorCodeEasyTierStartFailed)
		gui.SetState(GUIState{
			Phase:     "error",
			APIPort:   apiPort,
			Error:     fmt.Sprintf("EasyTier failed: %v", err),
			ErrorCode: errCode,
		})
		fmt.Fprintf(os.Stderr, "EasyTier failed: %v\n", err)
		waitForSignal()
		et.Stop()
		api.Stop()
		gui.Stop()
		return
	}

	ip, err := et.WaitForIP(30 * time.Second)
	if err != nil {
		errCode := classifyEasyTierError(err, et.Logs(), ErrorCodeEasyTierIPTimeout)
		errMsg := "Failed to get virtual IP (timeout 30s)"
		switch errCode {
		case ErrorCodeWindowsTUNInitFailed:
			errMsg = "Failed to initialize Windows virtual adapter (TUN)"
		case ErrorCodeWindowsFirewallBlocked:
			errMsg = "EasyTier traffic may be blocked by Windows firewall policy"
		}
		gui.SetState(GUIState{
			Phase:     "error",
			APIPort:   apiPort,
			Error:     fmt.Sprintf("%s: %v", errMsg, err),
			ErrorCode: errCode,
		})
		fmt.Fprintf(os.Stderr, "Failed to get virtual IP: %v\n", err)
		waitForSignal()
		et.Stop()
		api.Stop()
		gui.Stop()
		return
	}

	fmt.Printf("EasyTier virtual IP: %s\n", ip)

	gui.SetState(GUIState{
		Phase:   "running",
		VirtIP:  ip,
		APIPort: apiPort,
	})
	fmt.Printf("API server reachable at http://%s:%d\n", ip, apiPort)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	stopCh := make(chan struct{}, 1)
	go func() { <-sig; stopCh <- struct{}{} }()
	go func() { gui.WaitForConfig(); stopCh <- struct{}{} }() // nil from stop button

	<-stopCh
	fmt.Println("Shutting down...")
	api.Stop()
	et.Stop()
	gui.Stop()
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}

func waitForSignal() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
}
