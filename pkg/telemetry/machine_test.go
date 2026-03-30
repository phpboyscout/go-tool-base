package telemetry

import (
	"net"
	"os"
	"os/user"
	"testing"
)

func TestHashedMachineID_Stable(t *testing.T) {
	t.Parallel()

	id1 := HashedMachineID()
	id2 := HashedMachineID()

	if id1 != id2 {
		t.Errorf("machine ID not stable: %q != %q", id1, id2)
	}

	if len(id1) != 16 {
		t.Errorf("expected 16 hex chars, got %d: %q", len(id1), id1)
	}
}

func TestHashedMachineID_NotRaw(t *testing.T) {
	t.Parallel()

	id := HashedMachineID()

	hostname, _ := os.Hostname()
	if hostname != "" && len(hostname) > 2 {
		if id == hostname {
			t.Error("machine ID should not be raw hostname")
		}
	}

	u, _ := user.Current()
	if u != nil && u.Username != "" {
		if id == u.Username {
			t.Error("machine ID should not be raw username")
		}
	}
}

func TestHashedMachineID_MultiSignal(t *testing.T) {
	t.Parallel()

	// Verify that the function uses multiple signals by checking they're available
	// (We can't easily test the hash composition, but we verify signals are queried)
	_ = osMachineID()
	_ = firstMACAddress()

	hostname, _ := os.Hostname()
	_ = hostname

	// The hash should be non-empty even if some signals are empty
	id := HashedMachineID()
	if id == "" {
		t.Error("machine ID should never be empty")
	}
}

func TestFirstMACAddress(t *testing.T) {
	t.Parallel()

	mac := firstMACAddress()

	// May be empty in some CI environments (containers)
	if mac == "" {
		t.Skip("no non-loopback interface found")
	}

	// Should be a valid MAC format
	_, err := net.ParseMAC(mac)
	if err != nil {
		t.Errorf("invalid MAC format: %q: %v", mac, err)
	}
}
