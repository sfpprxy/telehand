//go:build windows

package main

import _ "embed"

//go:embed easytier-bin/easytier-core.exe
var embeddedEasyTier []byte

//go:embed easytier-bin/easytier-cli.exe
var embeddedEasyTierCli []byte

//go:embed easytier-bin/Packet.dll
var embeddedPacketDLL []byte

//go:embed easytier-bin/wintun.dll
var embeddedWintunDLL []byte
