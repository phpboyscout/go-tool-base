package templates

import (
	"bytes"
	"testing"

	"github.com/dave/jennifer/jen"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func renderSkeletonRoot(t *testing.T, data SkeletonRootData) string {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, SkeletonRoot(data).Render(&buf))

	return buf.String()
}

// TestSkeletonRoot_ExternalSigillumShape is the headline case: the sign/keys
// tree from go/signing-cli, each constructor taking the narrow logger seam and
// wrapped (returns *cobra.Command). This is exactly what the sigillum main.go
// workaround did by hand — now rendered into the generated root.
func TestSkeletonRoot_ExternalSigillumShape(t *testing.T) {
	t.Parallel()

	out := renderSkeletonRoot(t, SkeletonRootData{
		Name: "sigillum",
		ExternalCommands: []SkeletonExternalCommand{
			{ImportPath: "gitlab.com/phpboyscout/go/signing-cli", PkgAlias: "signingcli", Constructor: "NewCmdSign", Args: []string{"logger"}, Wrap: true},
			{ImportPath: "gitlab.com/phpboyscout/go/signing-cli", PkgAlias: "signingcli", Constructor: "NewCmdKeys", Args: []string{"logger"}, Wrap: true},
		},
	})

	assert.Contains(t, out, `setup.Wrap("", signingcli.NewCmdSign(p.GetLogger()))`)
	assert.Contains(t, out, `setup.Wrap("", signingcli.NewCmdKeys(p.GetLogger()))`)
	// No adapter → the simple inline NewCmdRoot form (no append/spread).
	assert.NotContains(t, out, "append(")
	assert.NotContains(t, out, "external.Commands")
}

// TestSkeletonRoot_ExternalPropsStyle covers a props-taking constructor that
// returns *setup.Command directly (wrap: false) — attached without setup.Wrap.
func TestSkeletonRoot_ExternalPropsStyle(t *testing.T) {
	t.Parallel()

	out := renderSkeletonRoot(t, SkeletonRootData{
		Name: "widget",
		ExternalCommands: []SkeletonExternalCommand{
			{ImportPath: "example.com/ext", PkgAlias: "ext", Constructor: "NewCmdFoo", Args: []string{"props"}, Wrap: false},
		},
	})

	assert.Contains(t, out, "ext.NewCmdFoo(p)")
	assert.NotContains(t, out, "setup.Wrap")
}

// TestSkeletonRoot_ExternalZeroArg covers a zero-argument constructor.
func TestSkeletonRoot_ExternalZeroArg(t *testing.T) {
	t.Parallel()

	out := renderSkeletonRoot(t, SkeletonRootData{
		Name: "widget",
		ExternalCommands: []SkeletonExternalCommand{
			{ImportPath: "example.com/ext", PkgAlias: "ext", Constructor: "NewCmdBar", Wrap: true},
		},
	})

	assert.Contains(t, out, `setup.Wrap("", ext.NewCmdBar())`)
}

// TestSkeletonRoot_ExternalAdapter covers the adapter channel: external.Commands(p)
// is spread into NewCmdRoot via append (individual args cannot mix with a spread).
func TestSkeletonRoot_ExternalAdapter(t *testing.T) {
	t.Parallel()

	out := renderSkeletonRoot(t, SkeletonRootData{
		Name:            "widget",
		ModulePath:      "example.com/org/widget",
		ExternalAdapter: true,
		Subcommands: []SkeletonSubcommand{
			{ImportPath: "example.com/org/widget/pkg/cmd/serve", PkgAlias: "serve", Constructor: "NewCmdServe"},
		},
	})

	assert.Contains(t, out, "external.Commands(p)")
	assert.Contains(t, out, "append(")
	assert.Contains(t, out, "[]*setup.Command{")
	assert.Contains(t, out, "example.com/org/widget/pkg/cmd/external")
}

// TestSkeletonRoot_NoExternalUnchanged guards the zero-churn promise: a project
// with neither an adapter nor declarative attachments renders the plain inline
// NewCmdRoot form, so no existing generated cmd.go changes.
func TestSkeletonRoot_NoExternalUnchanged(t *testing.T) {
	t.Parallel()

	out := renderSkeletonRoot(t, SkeletonRootData{
		Name: "widget",
		Subcommands: []SkeletonSubcommand{
			{ImportPath: "example.com/org/widget/pkg/cmd/serve", PkgAlias: "serve", Constructor: "NewCmdServe"},
		},
	})

	assert.Contains(t, out, "gtbRoot.NewCmdRoot(p, serve.NewCmdServe(p))")
	assert.NotContains(t, out, "append(")
	assert.NotContains(t, out, "external.Commands")
}

// TestExternalArgVocabularyLockstep guards that every token in the closed
// vocabulary is handled by ExternalArgExpr (none silently falls through to the
// default p), so the validator's accepted set and the renderer stay in lockstep.
func TestExternalArgVocabularyLockstep(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"logger":  "p.GetLogger()",
		"props":   "p",
		"config":  "p.Config",
		"fs":      "p.FS",
		"version": "p.Version",
	}

	for _, tok := range ExternalArgTokens {
		require.Contains(t, want, tok, "token %q has no expected expression — vocabulary drifted", tok)

		var buf bytes.Buffer
		require.NoError(t, jen.Null().Add(ExternalArgExpr(tok)).Render(&buf))
		assert.Equal(t, want[tok], buf.String(), "token %q rendered unexpectedly", tok)
	}
}
