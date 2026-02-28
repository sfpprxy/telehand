//go:build linux && amd64

package main

import _ "embed"

//go:embed easytier-bin/easytier-core-linux-amd64
var embeddedEasyTier []byte

//go:embed easytier-bin/easytier-cli-linux-amd64
var embeddedEasyTierCli []byte

var embeddedPacketDLL []byte
var embeddedWintunDLL []byte
