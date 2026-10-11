// osdisplay.go — v1.2.6 continuation: the MEASURED build → label mapping.
//
// Pure function, shared across platforms (fixture-tested on every OS):
// the Windows display label is DERIVED from the measured build number,
// never from a manual label. The raw build always rides the wire
// (SysInfo.OSBuild) so consumers never have to trust the label alone.
package sysinfo

// WindowsDisplayForBuild derives the honest marketing label from a
// MEASURED Windows build number. Pure function — fixture-tested. The raw
// build always rides along on the wire (OSBuild) so a consumer never has
// to trust the label alone. Mapping per Microsoft's public build→release
// table:
//
//	>= 26000  Windows 11 25H2 era (26200 = 25H2 RTM)
//	>= 22631  Windows 11 23H2
//	>= 22621  Windows 11 22H2
//	>= 22000  Windows 11 21H2
//	>= 19045  Windows 10 22H2
//	>= 19044  Windows 10 21H2
//	>= 19041  Windows 10 20H2/2004 era
//	<  19041  Windows 10 (older) / unknown — the build number is shown
//	          verbatim and never mapped to a specific release.
func WindowsDisplayForBuild(build int) string {
	if build <= 0 {
		return "unknown"
	}

	switch {
	case build >= 27800:
		return "Windows 11 (build " + itoa(build) + ")"
	case build >= 26200:
		return "Windows 11 25H2 (build " + itoa(build) + ")"
	case build >= 26100:
		return "Windows 11 24H2 (build " + itoa(build) + ")"
	case build >= 22631:
		return "Windows 11 23H2 (build " + itoa(build) + ")"
	case build >= 22621:
		return "Windows 11 22H2 (build " + itoa(build) + ")"
	case build >= 22000:
		return "Windows 11 21H2 (build " + itoa(build) + ")"
	case build >= 19045:
		return "Windows 10 22H2 (build " + itoa(build) + ")"
	case build >= 19044:
		return "Windows 10 21H2 (build " + itoa(build) + ")"
	case build >= 19041:
		return "Windows 10 (build " + itoa(build) + ")"
	default:
		return "Windows (build " + itoa(build) + ")"
	}
}

// itoa avoids importing strconv into this already-small file's hot path.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}

	neg := n < 0
	if neg {
		n = -n
	}

	var buf [12]byte
	i := len(buf)

	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}

	if neg {
		i--
		buf[i] = '-'
	}

	return string(buf[i:])
}
