//go:build !windows && !linux && !darwin

// sysinfo_fast_otherunix.go — fast RAM facts on other Unix platforms:
// no in-process RAM probe is linked for these platforms, so the honest
// unknown (empty RAMInfo) is returned. Values are never fabricated.
package sysinfo

func fastRAM() RAMInfo {
	return RAMInfo{}
}
