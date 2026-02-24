//go:build darwin

package main

import _ "embed"

//go:embed easytier-bin/easytier-core-darwin
var embeddedEasyTier []byte

//go:embed easytier-bin/easytier-cli-darwin
var embeddedEasyTierCli []byte

var embeddedPacketDLL []byte
var embeddedWintunDLL []byte
