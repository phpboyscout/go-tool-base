package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFrameworkLinksNoAdapter builds this package and reads its build info.
// A chat provider or forge adapter in the list means something under pkg/
// imports one again, and every tool built on the framework has just grown by
// that module's SDK.
func TestFrameworkLinksNoAdapter(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}

	bin := filepath.Join(t.TempDir(), "footprint")

	build := exec.Command("go", "build", "-o", bin, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := build.CombinedOutput()
	require.NoError(t, err, string(out))

	info, err := exec.Command("go", "version", "-m", bin).Output()
	require.NoError(t, err)

	for _, line := range strings.Split(string(info), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "dep" {
			continue
		}

		module := fields[1]
		for _, banned := range []string{
			"gitlab.com/phpboyscout/go/chat-",
			"gitlab.com/phpboyscout/go/forge-",
			"gitlab.com/phpboyscout/go/signing-aws-kms",
		} {
			require.False(t, strings.HasPrefix(module, banned),
				"framework binary links %s; registration belongs in the tool's main, not pkg/ (spec 0194 D3)", module)
		}
	}
}
