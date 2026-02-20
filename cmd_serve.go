package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"
)

func runServe() {
	gui := NewGUIServer(18080)
	apiPort := 0
	api := NewAPIServer("0.0.0.0", 8080, func(log CmdLog) {
		gui.AddLog(log)
	}, func() HealthResp {
		s := gui.GetState()
		return HealthResp{
			Status:  "ok",
			Phase:   s.Phase,
			VirtIP:  s.VirtIP,
			APIPort: apiPort,
			GUIPort: gui.Port(),
			Error:   s.Error,
		}
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
	openBrowser(guiURL)

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
		gui.SetState(GUIState{
			Phase:   "error",
			APIPort: apiPort,
			Error:   fmt.Sprintf("EasyTier failed: %v", err),
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
		gui.SetState(GUIState{
			Phase:   "error",
			APIPort: apiPort,
			Error:   "Failed to get virtual IP (timeout 30s)",
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
