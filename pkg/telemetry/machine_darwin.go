package telemetry

import (
	"os/exec"
	"strings"
)

// osMachineID returns the OS-level machine identifier on macOS.
// Reads IOPlatformUUID via ioreg. Returns "" if unavailable.
func osMachineID() string {
	out, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return ""
	}

	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.Contains(line, "IOPlatformUUID") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				return strings.Trim(strings.TrimSpace(parts[1]), "\"")
			}
		}
	}

	return ""
}
