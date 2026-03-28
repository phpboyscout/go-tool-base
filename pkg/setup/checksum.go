package setup

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
)

// VerifyChecksum reads a SHA-256 sidecar file and verifies it against
// the provided data. The sidecar format is "<hex-hash>  <filename>\n"
// (matching sha256sum output and GoReleaser checksums.txt entries).
// Returns nil if the checksum matches, or an error with a hint on mismatch.
func VerifyChecksum(fs afero.Fs, sidecarPath string, data []byte) error {
	sidecarContent, err := afero.ReadFile(fs, sidecarPath)
	if err != nil {
		return errors.Wrap(err, "failed to read checksum sidecar")
	}

	fields := strings.Fields(strings.TrimSpace(string(sidecarContent)))
	if len(fields) == 0 {
		return errors.New("empty or malformed checksum sidecar file")
	}

	expectedHash := fields[0]
	actualHash := fmt.Sprintf("%x", sha256.Sum256(data))

	if !strings.EqualFold(actualHash, expectedHash) {
		return errors.WithHint(
			errors.Newf("checksum mismatch: expected %s, got %s", expectedHash, actualHash),
			"The file may be corrupted or tampered with. Re-download from a trusted source.",
		)
	}

	return nil
}
