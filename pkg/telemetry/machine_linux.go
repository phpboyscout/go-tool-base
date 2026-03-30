package telemetry

import (
	"os"
	"strings"
)

// osMachineID returns the OS-level machine identifier on Linux.
// Reads /etc/machine-id. Returns "" if unavailable.
func osMachineID() string {
	data, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}
