package generator

import (
	"context"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dave/dst"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Issue #16: `regenerate manifest` is the tool's own answer to a manifest that
// has drifted behind the code, and it gets the flags right — but the same run
// destroyed two other things, both silently.
//
//  1. Every per-command hash was dropped (67 → 0 on keryx), which is the entire
//     drift-detection baseline that 0187 D10 taught doctor to read.
//  2. A multi-line description was rewritten from a block scalar to a plain
//     scalar with the newlines escaped. YAML does not interpret `\n` in a plain
//     scalar, so the value became a literal backslash-n — and reached the user's
//     terminal via `--help`.

// roundTripProject lays out a minimal project whose root registers one command,
// with the given command source and starting manifest. It returns the
// filesystem and the generator.
func roundTripProject(t *testing.T, cmdName, cmdSource, manifest string) (afero.Fs, *Generator) {
	t.Helper()

	fs := afero.NewMemMapFs()
	workDir := "/work"

	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "go.mod"), []byte("module test-tool\n"), 0644))
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/root"), 0755))
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd", cmdName), 0755))
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, ".gtb"), 0755))

	rootCode := `package root
import (
	gtbRoot "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"test-tool/pkg/cmd/` + cmdName + `"
)
func NewCmdRoot(p *props.Props) *setup.Command {
	return gtbRoot.NewCmdRoot(p, ` + cmdName + `.NewCmd` + strings.ToUpper(cmdName[:1]) + cmdName[1:] + `(p))
}`

	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/root/cmd.go"), []byte(rootCode), 0644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd", cmdName, "cmd.go"), []byte(cmdSource), 0644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, ".gtb/manifest.yaml"), []byte(manifest), 0644))

	var logBuf strings.Builder

	p := &props.Props{
		FS:     fs,
		Logger: logger.NewCharm(&logBuf),
		Config: emptyTestStore(t),
		Tool:   props.Tool{Name: "test-tool"},
	}

	return fs, New(p, &Config{Path: workDir})
}

func readRoundTripManifest(t *testing.T, fs afero.Fs) (raw string, m Manifest) {
	t.Helper()

	data, err := afero.ReadFile(fs, "/work/.gtb/manifest.yaml")
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(data, &m))

	return string(data), m
}

// approveSource is the shape the keryx report was taken from: a description
// authored across several lines, which the Go source carries as a single
// double-quoted literal with `\n` escapes.
const approveSource = `package approve
import (
	"github.com/spf13/cobra"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)
func NewCmdApprove(props *props.Props) *setup.Command {
	return setup.Wrap("approve", &cobra.Command{
		Use:   "approve",
		Short: "Approve a platform's reel for posting",
		Long:  "Approve a platform's reel for posting.\nRecords the approval.\nUse --revoke to return to draft.",
	})
}`

func TestRegenerateManifest_PreservesBookkeepingTheCodeCannotCarry(t *testing.T) {
	t.Parallel()

	// The manifest describes the same command the scan will find, plus the
	// state only the manifest holds: the drift-detection hashes, and the
	// hidden/protected flags the AST does not read back.
	manifest := `properties:
  name: test-tool
commands:
  - name: approve
    description: Approve a platform's reel for posting
    hidden: true
    protected: true
    hashes:
      cmd.go: aaaabbbbccccddddeeeeffff0000111122223333444455556666777788889999
`

	fs, g := roundTripProject(t, "approve", approveSource, manifest)
	require.NoError(t, g.RegenerateManifest(context.Background()))

	_, m := readRoundTripManifest(t, fs)
	require.Len(t, m.Commands, 1)

	cmd := m.Commands[0]

	assert.Equal(t, "aaaabbbbccccddddeeeeffff0000111122223333444455556666777788889999",
		cmd.Hashes["cmd.go"],
		"the drift-detection baseline must survive a manifest regeneration")
	assert.True(t, cmd.Hidden, "hidden is manifest state the scanner does not recover")
	require.NotNil(t, cmd.Protected, "protected is manifest state the scanner does not recover")
	assert.True(t, *cmd.Protected)

	// The scan is still authoritative for what it does read.
	assert.Equal(t, "Approve a platform's reel for posting", string(cmd.Description))
}

func TestRegenerateManifest_KeepsAMultilineDescriptionReadable(t *testing.T) {
	t.Parallel()

	fs, g := roundTripProject(t, "approve", approveSource, "properties:\n  name: test-tool\n")
	require.NoError(t, g.RegenerateManifest(context.Background()))

	raw, m := readRoundTripManifest(t, fs)
	require.Len(t, m.Commands, 1)

	long := string(m.Commands[0].LongDescription)

	assert.NotContains(t, long, `\n`,
		"a literal backslash-n in the value reaches the user's terminal via --help")
	assert.Contains(t, long, "\n", "the description is multi-line and must stay so")
	assert.Equal(t,
		"Approve a platform's reel for posting.\nRecords the approval.\nUse --revoke to return to draft.",
		long)

	assert.Contains(t, raw, "long_description: |-",
		"a multi-line value must be written back as a block scalar so it survives the trip")
}

// TestResolveStringValue_InterpretsEscapes covers the literal decoding directly:
// the reported `\n` corruption is one symptom of not decoding Go string
// literals at all, and every other escape is affected the same way.
func TestResolveStringValue_InterpretsEscapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		literal string
		want    string
	}{
		{"newline escape", `"one\ntwo"`, "one\ntwo"},
		{"tab escape", `"one\ttwo"`, "one\ttwo"},
		{"escaped quote", `"say \"hi\""`, `say "hi"`},
		{"escaped backslash", `"a\\b"`, `a\b`},
		{"raw string keeps its newline", "`one\ntwo`", "one\ntwo"},
		{"raw string does not interpret escapes", "`one\\ntwo`", `one\ntwo`},
		{"plain text is unchanged", `"nothing special"`, "nothing special"},
		{"a quote inside a raw string survives", "`say \"hi\"`", `say "hi"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lit := &dst.BasicLit{Kind: token.STRING, Value: tt.literal}

			got, ok := resolveStringValue(lit, nil)
			require.True(t, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}
