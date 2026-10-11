//go:build windows

package llm

// exitSignalPart: Windows has no signals — termination is expressed as a
// process exit code only, which exitDescription already reports.
func exitSignalPart(err error) string {
	return ""
}
