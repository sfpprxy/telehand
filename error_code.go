package main

import (
	"errors"
	"runtime"
	"strings"
)

const (
	ErrorCodeWindowsNotAdmin        = "windows_not_admin"
	ErrorCodeWindowsAdminCheckFail  = "windows_admin_check_failed"
	ErrorCodeWindowsTUNInitFailed   = "windows_tun_init_failed"
	ErrorCodeWindowsFirewallBlocked = "windows_firewall_blocked"
	ErrorCodeEasyTierStartFailed    = "easytier_start_failed"
	ErrorCodeEasyTierIPTimeout      = "easytier_ip_timeout"
)

type codedError struct {
	code string
	msg  string
}

func (e *codedError) Error() string { return e.msg }

func (e *codedError) Code() string { return e.code }

func newCodedError(code, msg string) error {
	return &codedError{
		code: code,
		msg:  msg,
	}
}

func errorCodeOf(err error) string {
	if err == nil {
		return ""
	}
	var withCode interface{ Code() string }
	if errors.As(err, &withCode) {
		return withCode.Code()
	}
	return ""
}

func classifyEasyTierError(err error, logs []string, fallback string) string {
	return classifyEasyTierErrorByOS(runtime.GOOS, err, logs, fallback)
}

func classifyEasyTierErrorByOS(goos string, err error, logs []string, fallback string) string {
	if code := errorCodeOf(err); code != "" {
		return code
	}

	evidence := strings.ToLower(joinErrorEvidence(err, logs))
	if goos == "windows" {
		if containsAnyFold(evidence,
			"firewall",
			"windows filtering platform",
			"wfp",
			"blocked by policy",
			"administratively prohibited",
		) {
			return ErrorCodeWindowsFirewallBlocked
		}
		if containsAnyFold(evidence,
			"wintun",
			"packet.dll",
			"npcap",
			"tap-windows",
			"virtual adapter",
			"create adapter",
			"tun device",
		) {
			return ErrorCodeWindowsTUNInitFailed
		}
	}

	return fallback
}

func joinErrorEvidence(err error, logs []string) string {
	var parts []string
	if err != nil {
		msg := strings.TrimSpace(err.Error())
		if msg != "" {
			parts = append(parts, msg)
		}
	}
	for _, line := range logs {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts = append(parts, line)
	}
	return strings.Join(parts, "\n")
}

func containsAnyFold(s string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(s, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}
