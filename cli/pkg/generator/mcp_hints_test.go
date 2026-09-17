package generator

import (
	"bytes"
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func TestManifestCommand_MCPHints_YAMLRoundTrip(t *testing.T) {
	t.Parallel()

	in := ManifestCommand{Name: "generate", Description: "make", MCPHints: &ManifestMCPHints{
		Title: "Generate images", ReadOnly: props.BoolPtr(false), OpenWorld: props.BoolPtr(true),
	}}

	out, err := yaml.Marshal(in)
	require.NoError(t, err)
	assert.Contains(t, string(out), "mcp_hints:\n")
	assert.Contains(t, string(out), "    title: Generate images\n")
	assert.Contains(t, string(out), "    read_only: false\n")
	assert.Contains(t, string(out), "    open_world: true\n")
	assert.NotContains(t, string(out), "destructive", "an unset hint writes no key")

	var back ManifestCommand
	require.NoError(t, yaml.Unmarshal(out, &back))
	assert.Equal(t, in.MCPHints, back.MCPHints)

	bare, err := yaml.Marshal(ManifestCommand{Name: "post", Description: "x"})
	require.NoError(t, err)
	assert.NotContains(t, string(bare), "mcp_hints")
}

func TestManifestMCPHints_ConvertsToSetup(t *testing.T) {
	t.Parallel()

	var none *ManifestMCPHints
	assert.True(t, none.Setup().IsZero())

	m := &ManifestMCPHints{Title: "T", Idempotent: props.BoolPtr(true)}
	assert.Equal(t, setup.MCPHints{Title: "T", Idempotent: props.BoolPtr(true)}, m.Setup())
	assert.Equal(t, m, ManifestMCPHintsFrom(m.Setup()))
	assert.Nil(t, ManifestMCPHintsFrom(setup.MCPHints{}), "zero hints are no block")
}

func renderPostCmdWithHints(t *testing.T, hints setup.MCPHints) []byte {
	t.Helper()

	data := templates.CommandData{
		Package:     "post",
		PascalName:  "Post",
		Name:        "post",
		Short:       "publish",
		Long:        "publish",
		MCPExposure: setup.MCPExposureExposed,
		MCPHints:    hints,
	}

	var buf bytes.Buffer
	require.NoError(t, templates.CommandRegistration(data).Render(&buf))

	return buf.Bytes()
}

func TestCommandTemplate_EmitsOnlySetHints(t *testing.T) {
	t.Parallel()

	src := string(renderPostCmdWithHints(t, setup.MCPHints{Title: "Post \"it\"", ReadOnly: props.BoolPtr(false), OpenWorld: props.BoolPtr(true)}))
	assert.Contains(t, src, `setup.AnnotateMCP(cmd, setup.MCPHints{Title: "Post \"it\"", ReadOnly: new(false), OpenWorld: new(true)})`)
	assert.Less(t, indexOf(src, "setup.IncludeInMCP(cmd)"), indexOf(src, "setup.AnnotateMCP("), "hints follow the exposure marker")

	none := string(renderPostCmdWithHints(t, setup.MCPHints{}))
	assert.NotContains(t, none, "AnnotateMCP")
}

func indexOf(s, sub string) int {
	return bytes.Index([]byte(s), []byte(sub))
}

func TestDetectMCPHints_RoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		hints setup.MCPHints
	}{
		{"full", setup.MCPHints{Title: "Post it", ReadOnly: props.BoolPtr(false), Destructive: props.BoolPtr(false), Idempotent: props.BoolPtr(true), OpenWorld: props.BoolPtr(true)}},
		{"title only", setup.MCPHints{Title: "Post it"}},
		{"one hint", setup.MCPHints{ReadOnly: props.BoolPtr(true)}},
		{"none", setup.MCPHints{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			p := &props.Props{FS: fs, Logger: logger.NewNoop()}
			path := "/work/pkg/cmd/post/cmd.go"
			require.NoError(t, afero.WriteFile(fs, path, renderPostCmdWithHints(t, tc.hints), 0o644))

			g := New(p, &Config{Path: "/work"})

			cmd, _, _, err := g.extractCommandMetadata(path)
			require.NoError(t, err)
			require.Equal(t, "post", cmd.Name)
			require.NotNil(t, cmd.MCPEnabled, "the exposure marker is still read")
			assert.Equal(t, ManifestMCPHintsFrom(tc.hints), cmd.MCPHints)
			assert.Empty(t, cmd.Flags, "the hints call is not mistaken for a flag")
		})
	}
}

func TestSetMCPHints_SetMergeClear(t *testing.T) {
	t.Parallel()

	p, root, fs := newSetEnabledFixture(t)
	g := New(p, &Config{Path: root})
	ctx := context.Background()

	require.NoError(t, g.SetMCPHints(ctx, "post", setup.MCPHints{Title: "Post it", ReadOnly: props.BoolPtr(true)}, false))

	manifest, _ := afero.ReadFile(fs, root+"/.gtb/manifest.yaml")
	assert.Contains(t, string(manifest), "mcp_hints:\n")
	assert.Contains(t, string(manifest), "title: Post it\n")
	assert.Contains(t, string(manifest), "read_only: true\n")

	cmdGo, err := afero.ReadFile(fs, root+"/pkg/cmd/post/cmd.go")
	require.NoError(t, err)
	assert.Contains(t, string(cmdGo), `setup.AnnotateMCP(cmd, setup.MCPHints{Title: "Post it", ReadOnly: new(true)})`)

	// A second call merges: the title survives, read_only flips, open_world is added.
	require.NoError(t, g.SetMCPHints(ctx, "post", setup.MCPHints{ReadOnly: props.BoolPtr(false), OpenWorld: props.BoolPtr(true)}, false))

	manifest, _ = afero.ReadFile(fs, root+"/.gtb/manifest.yaml")
	assert.Contains(t, string(manifest), "title: Post it\n")
	assert.Contains(t, string(manifest), "read_only: false\n")
	assert.Contains(t, string(manifest), "open_world: true\n")

	cmdGo, _ = afero.ReadFile(fs, root+"/pkg/cmd/post/cmd.go")
	assert.Contains(t, string(cmdGo), `setup.MCPHints{Title: "Post it", ReadOnly: new(false), OpenWorld: new(true)}`)

	// Clear removes the block and the call.
	require.NoError(t, g.SetMCPHints(ctx, "post", setup.MCPHints{}, true))

	manifest, _ = afero.ReadFile(fs, root+"/.gtb/manifest.yaml")
	assert.NotContains(t, string(manifest), "mcp_hints")

	cmdGo, _ = afero.ReadFile(fs, root+"/pkg/cmd/post/cmd.go")
	assert.NotContains(t, string(cmdGo), "AnnotateMCP")
}

func TestSetMCPHints_RefusesProtectedAndUnknown(t *testing.T) {
	t.Parallel()

	p, root, _ := newSetEnabledFixture(t)
	g := New(p, &Config{Path: root})

	require.ErrorIs(t, g.SetMCPHints(context.Background(), "secret", setup.MCPReadOnly(), false), ErrCommandProtected)

	err := g.SetMCPHints(context.Background(), "missing", setup.MCPReadOnly(), false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing")
}

func TestSetMCPHints_RejectsInvalidTitle(t *testing.T) {
	t.Parallel()

	p, root, _ := newSetEnabledFixture(t)
	g := New(p, &Config{Path: root})

	require.Error(t, g.SetMCPHints(context.Background(), "post", setup.MCPHints{Title: "bad\x00title"}, false))
}
