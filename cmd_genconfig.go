package main

import (
	"flag"
	"fmt"
	"os"
)

func runGenConfig(args []string) int {
	fs := flag.NewFlagSet("gen-config", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	networkName := fs.String("network-name", "", "EasyTier network name")
	networkSecret := fs.String("network-secret", "", "EasyTier network secret")
	peers := fs.String("peers", "", "Comma-separated peer addresses (e.g. tcp://1.2.3.4:11010)")
	if err := fs.Parse(args); err != nil {
		return ExitCodeParam
	}
	if len(fs.Args()) > 0 {
		fmt.Fprintln(os.Stderr, "Usage: telehand gen-config --network-name NAME --network-secret SECRET --peers PEERS")
		return ExitCodeParam
	}

	encoded, _, err := buildEncodedConfigFromInputs(*networkName, *networkSecret, *peers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Usage: telehand gen-config --network-name NAME --network-secret SECRET --peers PEERS\nError: %v\n", err)
		return ExitCodeParam
	}
	fmt.Println(encoded)
	return ExitCodeOK
}
