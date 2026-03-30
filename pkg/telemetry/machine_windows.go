package telemetry

import (
	"os/exec"
	"strings"
)

// osMachineID returns the OS-level machine identifier on Windows.
// Reads MachineGuid from the registry. Returns "" if unavailable.
func osMachineID() string {
	out, err := exec.Command("reg", "query",
		`HKLM\SOFTWARE\Microsoft\Cryptography`,
		"/v", "MachineGuid").Output()
	if err != nil {
		return ""
	}

	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.Contains(line, "MachineGuid") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				return fields[len(fields)-1]
			}
		}
	}

	return ""
}
