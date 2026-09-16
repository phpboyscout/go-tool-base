package repopolicy

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// streamsAllowlist names the files that may still name the process's streams
// directly. pkg/props/io.go is the default an IO falls back to and stays;
// the rest are spec 0198 D6's phase 3 and leave this list as they move to
// Props.GetIO().
var streamsAllowlist = map[string]bool{
	"pkg/props/io.go":           true,
	"pkg/cmd/config/edit.go":    true, // 0198 phase 3
	"pkg/cmd/config/migrate.go": true, // 0198 phase 3
	"pkg/cmd/doctor/doctor.go":  true, // 0198 phase 3
	"pkg/cmd/root/root.go":      true, // 0198 phase 3
	"pkg/cmd/update/update.go":  true, // 0198 phase 3
	"pkg/utils/main.go":         true, // deprecated IsInteractive; goes with it
}

// TestNoProcessStreamsInPkg pins spec 0198 D6: a package under pkg/ reads
// and writes the invocation's streams through Props.GetIO(), never
// os.Stdin/os.Stdout/os.Stderr, so a wizard can be driven by a test and a
// tool can redirect its terminal. Comments are not sites.
func TestNoProcessStreamsInPkg(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	stream := regexp.MustCompile(`\bos\.(Stdin|Stdout|Stderr)\b`)
	comment := regexp.MustCompile(`^\s*//`)

	var offenders []string

	for _, f := range listGoFiles(t, root) {
		if strings.HasSuffix(f, "_test.go") || !strings.HasPrefix(f, "pkg/") || streamsAllowlist[f] {
			continue
		}

		src, err := os.ReadFile(filepath.Join(root, f))
		require.NoError(t, err)

		for i, line := range strings.Split(string(src), "\n") {
			if comment.MatchString(line) {
				continue
			}

			if stream.MatchString(line) {
				offenders = append(offenders, f+":"+strconv.Itoa(i+1))
			}
		}
	}

	assert.Empty(t, offenders, "read the invocation's streams through Props.GetIO(), not os.Std*")

	// The allowlist shrinks; a file that no longer needs its entry drops it.
	for f := range streamsAllowlist {
		src, err := os.ReadFile(filepath.Join(root, f))
		require.NoErrorf(t, err, "allowlisted %s exists", f)
		assert.Truef(t, stream.MatchString(string(src)), "%s is allowlisted but names no process stream; drop the entry", f)
	}
}
