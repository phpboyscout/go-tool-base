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
	"pkg/credentialposture/keys.go":        true,
	"pkg/telemetrytypes/telemetrytypes.go": true,
	"internal/transportcfg/selection.go":   true,
}

// TestConfigKeysAreDeclaredOnce fails when a config key of a known family
// (telemetry, update, ai, server, log, vcs, a chat provider's api subtree or
// a forge's credential subtree) appears as a string literal outside its
// constants file, in the framework or the CLI. A key is part of the tool's
// contract; eleven copies of "telemetry.enabled" drift (#63).
func TestConfigKeysAreDeclaredOnce(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	// A constant declaring the key is where the literal belongs.
	constDecl := regexp.MustCompile(`^(const\s+)?[A-Za-z_]\w*\s*=\s*"[a-z_.]+"$`)
	keyLiteral := regexp.MustCompile(`"(` +
		`(telemetry|update|ai|server|log)\.[a-z_]+` +
		`|vcs\.provider` +
		`|(anthropic|openai|gemini|azure)\.api` +
		`|(github|gitlab|gitea|codeberg|bitbucket|direct)\.(auth|username|app_password|keychain|ssh)` +
		`)(\.[a-z_]+)*"`)

	var offenders []string

	for _, f := range listGoFiles(t, root) {
		if strings.HasSuffix(f, "_test.go") || keyFiles[f] || !guardedTree(f) {
			continue
		}

		src, err := os.ReadFile(filepath.Join(root, f))
		require.NoError(t, err)

		for i, line := range strings.Split(string(src), "\n") {
			for _, m := range keyLiteralsIn(line, constDecl, keyLiteral) {
				offenders = append(offenders, f+":"+strconv.Itoa(i+1)+" "+m)
			}
		}
	}

	require.Empty(t, offenders, "config key literals outside their constants file")
}

// keyLiteralsIn returns the key literals on a line of code that are uses
// rather than declarations: a comment, a constant declaration and a struct
// tag binding a field to its key all declare.
func keyLiteralsIn(line string, constDecl, keyLiteral *regexp.Regexp) []string {
	code := strings.TrimSpace(line)
	if strings.HasPrefix(code, "//") || constDecl.MatchString(code) || strings.Contains(code, "config:\"") {
		return nil
	}

	var uses []string

	for _, m := range keyLiteral.FindAllString(code, -1) {
		if m == `"telemetry.log"` { // a file name, not a key
			continue
		}

		uses = append(uses, m)
	}

	return uses
}

func guardedTree(f string) bool {
	return strings.HasPrefix(f, "pkg/") || strings.HasPrefix(f, "cli/pkg/")
}
