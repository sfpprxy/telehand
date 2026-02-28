//go:build linux && arm64

package main

import _ "embed"

//go:embed easytier-bin/easytier-core-linux-arm64
var embeddedEasyTier []byte

//go:embed easytier-bin/easytier-cli-linux-arm64
var embeddedEasyTierCli []byte

var embeddedPacketDLL []byte
var embeddedWintunDLL []byte
