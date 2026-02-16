package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func runGenConfig(args []string) {
	fs := flag.NewFlagSet("gen-config", flag.ExitOnError)
	networkName := fs.String("network-name", "", "EasyTier network name")
	networkSecret := fs.String("network-secret", "", "EasyTier network secret")
	peers := fs.String("peers", "", "Comma-separated peer addresses (e.g. tcp://1.2.3.4:11010)")
	fs.Parse(args)

	if *networkName == "" || *networkSecret == "" || *peers == "" {
		fmt.Fprintln(os.Stderr, "Usage: agent gen-config --network-name NAME --network-secret SECRET --peers PEERS")
		os.Exit(1)
	}

	c := &Config{
		NetworkName:   *networkName,
		NetworkSecret: *networkSecret,
		Peers:         strings.Split(*peers, ","),
	}
	encoded, err := EncodeConfig(c)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(encoded)
}
