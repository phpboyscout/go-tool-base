package telemetry

import (
	"os"
	"runtime"
	"strings"
)

// osVersion returns a human-readable OS version string.
// Linux: reads /proc/version, macOS/Windows: uses runtime.GOOS as fallback.
func osVersion() string {
	const kernelVersionField = 2 // "Linux version <kernel> ..."

	switch runtime.GOOS {
	case "linux":
		data, err := os.ReadFile("/proc/version")
		if err != nil {
			return runtime.GOOS
		}

		// Extract kernel version from "Linux version 6.8.0-106-generic ..."
		parts := strings.Fields(string(data))
		if len(parts) > kernelVersionField {
			return parts[kernelVersionField]
		}

		return strings.TrimSpace(string(data))
	default:
		return runtime.GOOS
	}
}
