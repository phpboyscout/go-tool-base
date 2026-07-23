package generator

// Regression tests for the boilerplate docs builder escaping
// (docs/development/specs/2026-07-23-generator-manifest-validation-hardening.md
// §MEDIUM finding): command/flag descriptions render into Markdown prose and
// tables via fmt.Fprintf, so the templateFuncMap escape pipes never applied.
// A `|` or newline in a description corrupted every generated table row, and
// raw HTML passed through unescaped.

import (
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unescapedPipes counts '|' runes not preceded by an odd run of backslashes.
func unescapedPipes(line string) int {
	count := 0

	for i, r := range line {
		if r != '|' {
			continue
		}

		backslashes := 0
		for j := i - 1; j >= 0 && line[j] == '\\'; j-- {
			backslashes++
		}

		if backslashes%2 == 0 {
			count++
		}
	}

	return count
}

const tableEscapeManifest = `properties:
  name: mytool
commands:
  - name: deploy
    description: "Deploys <script>alert(1)</script> stuff"
    flags:
      - name: region
        type: string
        description: "Region | zone\nsecond line"
        default: "eu-west-2"
    commands:
      - name: now
        description: "Immediately | no confirmation"
`

func TestWriteBasicCommandDocs_TableCellEscaping(t *testing.T) {
	t.Parallel()

	g := newPromptGenerator(t, tableEscapeManifest, false)
	out := "/work/docs/reference/cli/deploy.md"
	require.NoError(t, g.writeBasicCommandDocs("deploy", "mytool deploy", out))

	data, err := afero.ReadFile(g.props.FS, out)
	require.NoError(t, err)
	s := string(data)

	// The flag row must stay a single well-formed table row: the newline in
	// the description collapsed, the pipe escaped, so exactly 5 structural
	// pipes remain (4 columns).
	var flagRow string

	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, "--region") {
			flagRow = line

			break
		}
	}

	require.NotEmpty(t, flagRow, "flags table must contain a row for --region")
	assert.Equal(t, 5, unescapedPipes(flagRow),
		"flag row must have exactly 5 structural pipes, got %q", flagRow)
	assert.Contains(t, flagRow, "second line",
		"the description's second line must be collapsed into the same table row")

	// The subcommand row likewise: 3 structural pipes (2 columns).
	var subRow string

	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, "deploy now") {
			subRow = line

			break
		}
	}

	require.NotEmpty(t, subRow, "subcommands table must contain a row for now")
	assert.Equal(t, 3, unescapedPipes(subRow),
		"subcommand row must have exactly 3 structural pipes, got %q", subRow)

	// Raw HTML in the command description must arrive entity/backslash
	// escaped per escapeMarkdown semantics — never verbatim.
	assert.NotContains(t, s, "<script>",
		"hostile HTML in a description must not pass through unescaped")
}

func TestWriteBasicCommandDocs_LongDescriptionEscaped(t *testing.T) {
	t.Parallel()

	manifest := "properties:\n  name: mytool\ncommands:\n  - name: deploy\n    description: \"Deploy\"\n    long_description: \"Long <img src=x onerror=alert(1)> tail\"\n"

	g := newPromptGenerator(t, manifest, false)
	out := "/work/docs/reference/cli/deploy.md"
	require.NoError(t, g.writeBasicCommandDocs("deploy", "mytool deploy", out))

	data, err := afero.ReadFile(g.props.FS, out)
	require.NoError(t, err)

	s := string(data)
	assert.NotContains(t, s, "Long <img",
		"hostile HTML in a long description must not pass through unescaped")
	assert.Contains(t, s, `\<img`,
		"the angle bracket must arrive backslash-escaped per escapeMarkdown semantics")
}
