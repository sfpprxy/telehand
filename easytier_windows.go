//go:build windows

package main

import _ "embed"

//go:embed easytier-bin/easytier-core.exe
var embeddedEasyTier []byte
