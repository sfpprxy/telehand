package main

import (
	"fmt"
	"os"
)

func main() {
	os.Exit(runMain(os.Args[1:]))
}

func runMain(args []string) int {
	if len(args) == 0 {
		// default: serve mode (for double-click)
		return runServe(nil)
	}

	switch args[0] {
	case "serve":
		return runServe(args[1:])
	case "connect":
		return runConnect(args[1:])
	case "gen-config":
		return runGenConfig(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "Usage:\n  telehand serve [pairing-code]\n  telehand connect [pairing-code]\n  telehand gen-config --network-name NAME --network-secret SECRET --peers PEERS\n")
		return ExitCodeParam
	}
}
