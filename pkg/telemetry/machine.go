package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"os/user"
	"strings"
)

// HashedMachineID returns a privacy-preserving machine identifier derived from
// multiple system signals: OS machine ID, first non-loopback MAC address,
// hostname, and username. Each signal degrades gracefully if unavailable.
// The result is the first 8 bytes of the SHA-256 hash, encoded as 16 hex chars.
// Computed fresh on every invocation — not persisted to config.
func HashedMachineID() string {
	var parts []string

	parts = append(parts, osMachineID())
	parts = append(parts, firstMACAddress())

	hostname, _ := os.Hostname()
	parts = append(parts, hostname)

	u, _ := user.Current()
	if u != nil {
		parts = append(parts, u.Username)
	}

	raw := strings.Join(parts, ":")
	h := sha256.Sum256([]byte(raw))

	return hex.EncodeToString(h[:8])
}

// firstMACAddress returns the hardware address of the first non-loopback
// network interface. Returns "" if unavailable.
func firstMACAddress() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		if len(iface.HardwareAddr) > 0 {
			return iface.HardwareAddr.String()
		}
	}

	return ""
}
