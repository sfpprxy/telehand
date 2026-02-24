package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configStr := fs.String("config", "", "base64 config string to auto-connect")
	noBrowser := fs.Bool("no-browser", false, "do not auto-open browser")
	networkName := fs.String("network-name", "", "network name (used when no pairing code provided)")
	networkSecret := fs.String("network-secret", "", "network secret (used when no pairing code provided)")
	peers := fs.String("peers", "", "comma-separated peers (used when no pairing code provided)")
	if err := fs.Parse(args); err != nil {
		return ExitCodeParam
	}

	if len(fs.Args()) > 1 {
		fmt.Fprintln(os.Stderr, "Usage: telehand serve [pairing-code] [--config <code>] [--network-name ... --network-secret ... --peers ...]")
		return ExitCodeParam
	}

	positionalCode := ""
	if len(fs.Args()) == 1 {
		positionalCode = strings.TrimSpace(fs.Args()[0])
	}

	flagCode := strings.TrimSpace(*configStr)
	if positionalCode != "" && flagCode != "" && positionalCode != flagCode {
		fmt.Fprintln(os.Stderr, "Both positional pairing code and --config are provided but not equal")
		return ExitCodeParam
	}

	encoded := flagCode
	if encoded == "" {
		encoded = positionalCode
	}

	var (
		cfg *Config
		err error
	)

	if encoded == "" {
		encoded, cfg, err = buildEncodedConfigWithDefaults(*networkName, *networkSecret, *peers)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid defaults: %v\n", err)
			return ExitCodeParam
		}
		fmt.Println("No pairing code provided, generated defaults and auto-connect enabled.")
	} else {
		cfg, err = decodeConfigWithValidation(encoded)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid pairing code: %v\n", err)
			return ExitCodeParam
		}
		fmt.Println("Pairing code accepted, auto-connect enabled.")
	}

	fmt.Printf("Serve network: name=%s secret=%s peers=%s\n", cfg.NetworkName, maskSecret(cfg.NetworkSecret), strings.Join(cfg.Peers, ","))
	return runSession(sessionOptions{
		Role:          "server",
		NoBrowser:     *noBrowser,
		EncodedConfig: encoded,
	})
}
