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
	if err := gui.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start GUI: %v\n", err)
		os.Exit(1)
	}
	guiURL := fmt.Sprintf("http://127.0.0.1:%d", gui.Port())
	fmt.Printf("GUI started at %s\n", guiURL)
	openBrowser(guiURL)

	cfg := gui.WaitForConfig()
	if cfg == nil {
		fmt.Println("Stopped by user.")
		gui.Stop()
		return
	}

	fmt.Println("Starting EasyTier...")
	et := NewEasyTier(cfg, nil)
	if err := et.Start(cfg); err != nil {
		gui.SetState(GUIState{Phase: "error", Error: fmt.Sprintf("EasyTier failed: %v", err)})
		fmt.Fprintf(os.Stderr, "EasyTier failed: %v\n", err)
		waitForSignal()
		et.Stop()
		gui.Stop()
		return
	}

	ip, err := et.WaitForIP(30 * time.Second)
	if err != nil {
		gui.SetState(GUIState{Phase: "error", Error: "Failed to get virtual IP (timeout 30s)"})
		fmt.Fprintf(os.Stderr, "Failed to get virtual IP: %v\n", err)
		waitForSignal()
		et.Stop()
		gui.Stop()
		return
	}

	fmt.Printf("EasyTier virtual IP: %s\n", ip)

	api := NewAPIServer(ip, 8080, func(log CmdLog) {
		gui.AddLog(log)
	})
	if err := api.Start(); err != nil {
		gui.SetState(GUIState{Phase: "error", Error: fmt.Sprintf("API server failed: %v", err)})
		fmt.Fprintf(os.Stderr, "API server failed: %v\n", err)
		waitForSignal()
		et.Stop()
		gui.Stop()
		return
	}

	gui.SetState(GUIState{
		Phase:   "running",
		VirtIP:  ip,
		APIPort: api.Port(),
	})
	fmt.Printf("API server listening on %s:%d\n", ip, api.Port())

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
