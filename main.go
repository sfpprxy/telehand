package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "gen-config" {
		runGenConfig(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		runServe(os.Args[2:])
		return
	}
	// default: serve mode (for double-click)
	if len(os.Args) == 1 {
		runServe(nil)
		return
	}
	fmt.Fprintf(os.Stderr, "Usage:\n  agent serve          Start remote assist agent (default)\n  agent gen-config     Generate config string\n")
	os.Exit(1)
}
