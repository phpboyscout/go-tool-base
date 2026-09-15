package repopolicy

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// keyFiles are the files allowed to spell a config key of these families as
// a literal: the constants every other site reads from.
var keyFiles = map[string]bool{
	"pkg/chat/constants.go":                true,
	"pkg/setup/config_keys.go":             true,
	"pkg/telemetrytypes/telemetrytypes.go": true,
	"internal/transportcfg/selection.go":   true,
}

// TestConfigKeysAreDeclaredOnce fails when a telemetry.*, update.*, ai.* or server.*
// config key appears as a string literal outside its constants file. A key
// is part of the tool's contract; eleven copies of "telemetry.enabled" drift.
func TestConfigKeysAreDeclaredOnce(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	// A constant declaring the key is where the literal belongs.
	constDecl := regexp.MustCompile(`^(const\s+)?[A-Za-z_]\w*\s*=\s*"[a-z_.]+"$`)
	keyLiteral := regexp.MustCompile(`"(telemetry|update|ai|server)\.[a-z_]+(\.[a-z_]+)*"`)

	var offenders []string

	for _, f := range listGoFiles(t, root) {
		if strings.HasSuffix(f, "_test.go") || keyFiles[f] {
			continue
		}

		// cli/ builds against the released framework (cli/go.mod), so it
		// can only adopt a new constant one release after pkg/ declares it;
		// the guard covers the framework and the CLI's ai.provider use until
		// #63's second half lands after the next release.
		if !strings.HasPrefix(f, "pkg/") {
			continue
		}

		src, err := os.ReadFile(filepath.Join(root, f))
		require.NoError(t, err)

		for i, line := range strings.Split(string(src), "\n") {
			code := strings.TrimSpace(line)
			if strings.HasPrefix(code, "//") || constDecl.MatchString(code) {
				continue
			}

			for _, m := range keyLiteral.FindAllString(code, -1) {
				if m == `"telemetry.log"` { // a file name, not a key
					continue
				}

				offenders = append(offenders, f+":"+strconv.Itoa(i+1)+" "+m)
			}
		}
	}

	require.Empty(t, offenders, "config key literals outside their constants file")
}
