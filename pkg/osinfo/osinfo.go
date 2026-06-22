// Package osinfo reports a human-readable operating-system version string.
// It is the single shared implementation behind the telemetry OS field and the
// doctor report support bundle, so neither has to import the other's
// internals.
package osinfo

import (
	"os"
	"runtime"
	"strings"
)

// kernelVersionField is the index of the kernel version token in the
// "/proc/version" line: "Linux version <kernel> ...".
const kernelVersionField = 2

// Version returns a human-readable OS version string. On Linux it reports the
// kernel version from /proc/version; on every other platform — and whenever
// /proc/version cannot be read — it falls back to runtime.GOOS.
func Version() string {
	return version(runtime.GOOS, readProcVersion)
}

// readProcVersion returns the contents of /proc/version.
func readProcVersion() (string, error) {
	b, err := os.ReadFile("/proc/version")

	return string(b), err //nolint:wrapcheck // surfaced only as a fallback signal, never to the user.
}

// version is the pure core of Version, with the OS name and the /proc reader
// injected so every branch is testable without a platform dependency.
func version(goos string, readProc func() (string, error)) string {
	if goos != "linux" {
		return goos
	}

	data, err := readProc()
	if err != nil {
		return goos
	}

	return parseLinuxKernel(data)
}

// parseLinuxKernel extracts the kernel version from a /proc/version line,
// falling back to the trimmed whole string when it doesn't have the expected
// shape.
func parseLinuxKernel(procVersion string) string {
	parts := strings.Fields(procVersion)
	if len(parts) > kernelVersionField {
		return parts[kernelVersionField]
	}

	return strings.TrimSpace(procVersion)
}
