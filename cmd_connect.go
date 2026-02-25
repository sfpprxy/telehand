package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func runConnect(args []string) int {
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	networkName := fs.String("network-name", "", "default network name when pairing code is not provided")
	networkSecret := fs.String("network-secret", "", "default network secret when pairing code is not provided")
	peers := fs.String("peers", "", "comma-separated peer pool when pairing code is not provided (latency-first fallback)")
	noBrowser := fs.Bool("no-browser", false, "do not auto-open browser")
	if err := fs.Parse(args); err != nil {
		return ExitCodeParam
	}
	if len(fs.Args()) > 1 {
		fmt.Fprintln(os.Stderr, "Usage: telehand connect [pairing-code] [--network-name ... --network-secret ... --peers ...]")
		return ExitCodeParam
	}

	pairingCode := ""
	if len(fs.Args()) == 1 {
		pairingCode = strings.TrimSpace(fs.Args()[0])
	}

	var (
		cfg *Config
		err error
	)

	if pairingCode != "" {
		if strings.TrimSpace(*networkName) != "" || strings.TrimSpace(*networkSecret) != "" || strings.TrimSpace(*peers) != "" {
			fmt.Println("Pairing code provided; --network-name/--network-secret/--peers are ignored.")
		}
		cfg, err = decodeConfigWithValidation(pairingCode)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid pairing code: %v\n", err)
			return ExitCodeParam
		}
	} else {
		pairingCode, cfg, err = buildEncodedConfigWithDefaults(*networkName, *networkSecret, *peers)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid connect params: %v\n", err)
			return ExitCodeParam
		}
	}

	fmt.Printf("Connect network: name=%s secret=%s peers=%s\n", cfg.NetworkName, maskSecret(cfg.NetworkSecret), strings.Join(cfg.Peers, ","))
	fmt.Println("Peer strategy: latency-first ordering + fallback (details in debug logs).")

	commands := buildRemoteInstallCommands(pairingCode)
	fmt.Println("Run one of the following commands on the remote machine:")
	for _, c := range commands {
		fmt.Printf("  [%s] %s\n", c.Platform, c.Command)
	}

	clipboard := ""
	if len(commands) > 0 {
		clipboard = commands[0].Command
		if err := copyToClipboard(clipboard); err != nil {
			fmt.Fprintf(os.Stderr, "Copy command to clipboard failed: %v\n", err)
		} else {
			fmt.Println("Remote command copied to clipboard.")
		}
	}

	return runSession(sessionOptions{
		Role:             "client",
		NoBrowser:        *noBrowser,
		EncodedConfig:    pairingCode,
		Commands:         commands,
		ClipboardCommand: clipboard,
	})
}

func buildRemoteInstallCommands(pairingCode string) []InstallCommand {
	code := strings.TrimSpace(pairingCode)
	return []InstallCommand{
		{
			Platform: "Windows (PowerShell，下载并运行)",
			Command:  fmt.Sprintf("iwr -useb https://ghfast.top/https://raw.githubusercontent.com/sfpprxy/telehand/main/install.ps1 | iex; .\\telehand.exe serve '%s'", code),
		},
		{
			Platform: "macOS / Linux（下载并运行）",
			Command:  fmt.Sprintf("curl -fsSL https://raw.githubusercontent.com/sfpprxy/telehand/main/install.sh | bash && sudo ./telehand serve '%s'", code),
		},
		{
			Platform: "Windows (PowerShell，仅运行)",
			Command:  fmt.Sprintf(".\\telehand.exe serve '%s'", code),
		},
		{
			Platform: "macOS / Linux（仅运行）",
			Command:  fmt.Sprintf("sudo ./telehand serve '%s'", code),
		},
	}
}
