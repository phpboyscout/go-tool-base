package generator

// Validation tests for the keryx v0.27.0 bug report
// (BUG-REPORT-manifest-regen.md). These assert the EXPECTED (correct) behaviour
// so a failing test demonstrates the bug. Do not "fix" the tests to match
// current behaviour — they are the spec for the fixes.

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Bug 1 — re-registering one subcommand of a parent must MERGE into the
// parent's existing commands: list, preserving untouched siblings.
//
// keryx case: parent "theme" with add/edit/list/show/rm; re-running
// `generate command --name add --parent theme` reportedly dropped
// list/show/rm from the manifest.
func TestKeryxBug1_RegenSubcommandPreservesSiblings(t *testing.T) {
	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop()}
	require.NoError(t, fs.MkdirAll(".gtb", 0o755))

	initial := Manifest{Commands: []ManifestCommand{{
		Name: "theme",
		Commands: []ManifestCommand{
			{Name: "add"}, {Name: "edit"}, {Name: "list"}, {Name: "show"}, {Name: "rm"},
		},
	}}}
	data, _ := yaml.Marshal(initial)
	require.NoError(t, afero.WriteFile(fs, ".gtb/manifest.yaml", data, 0o644))

	// Re-register an existing subcommand, as `generate command` does via updateManifest.
	g := New(p, &Config{Path: ".", Name: "add", Parent: "theme", Short: "Register a new theme"})
	require.NoError(t, g.updateManifest(nil, map[string]string{"cmd.go": "h"}))

	data, _ = afero.ReadFile(fs, ".gtb/manifest.yaml")
	var m Manifest
	require.NoError(t, yaml.Unmarshal(data, &m))

	require.Len(t, m.Commands, 1)
	names := make([]string, 0, len(m.Commands[0].Commands))
	for _, c := range m.Commands[0].Commands {
		names = append(names, c.Name)
	}
	assert.ElementsMatch(t, []string{"add", "edit", "list", "show", "rm"}, names,
		"re-registering one subcommand must preserve untouched siblings")
}

// Bug 2 — a flag default that is a code constant whose initialiser is a
// nested conversion (e.g. `const defaultPort = int(int64(8080))`, as the
// generator's own templates emit) must resolve, not be warned-and-unset.
//
// resolveConstantValue handles a single conversion `int(8080)` but not the
// nested `int(int64(8080))`, so the const never enters the constants map and
// the flag default is dropped — silently changing a non-zero default to unset
// on the next regenerate project.
const keryxBug2CmdSrc = `package studio

import "github.com/spf13/cobra"

const defaultPort = int(int64(8080))

func NewCmdStudio(p *props.Props) *cobra.Command {
	cmd := &cobra.Command{Use: "studio", Short: "Studio server"}
	cmd.Flags().IntP("port", "P", defaultPort, "listen port")
	return cmd
}
`

func TestKeryxBug2_NestedConversionConstDefaultResolves(t *testing.T) {
	g, fs := newCoverageGenerator(t)
	path := writeCmd(t, fs, "studio", keryxBug2CmdSrc)

	cmd, _, _, err := g.extractCommandMetadata(path)
	require.NoError(t, err)

	port := findFlag(cmd, "port")
	require.NotNil(t, port)
	assert.Equal(t, "8080", port.Default,
		"int(int64(N)) code-constant default must resolve, not be dropped to unset")
	assert.Empty(t, port.Warning,
		"a resolvable constant default must not be warned-and-unset")
}

// Round-trip — every manifest write site now funnels through the single
// EncodeManifestFile helper (the scaffold/update/regen marshalers were
// collapsed), so two writes of the same manifest must be byte-identical. This
// is the guard against the 4-space<->2-space reformat churn keryx reported, now
// that a single serialiser makes divergence structurally impossible.
func TestKeryxRoundTrip_ManifestMarshalersAgree(t *testing.T) {
	fs := afero.NewMemMapFs()

	m := &Manifest{
		Properties: ManifestProperties{Name: "demo"},
		Commands: []ManifestCommand{
			{Name: "a", Commands: []ManifestCommand{{Name: "b"}}},
		},
	}

	require.NoError(t, EncodeManifestFile(fs, "enc.yaml", m))
	require.NoError(t, EncodeManifestFile(fs, "mar.yaml", m))

	enc, err := afero.ReadFile(fs, "enc.yaml")
	require.NoError(t, err)
	mar, err := afero.ReadFile(fs, "mar.yaml")
	require.NoError(t, err)

	assert.Equal(t, string(enc), string(mar),
		"the single manifest marshaler must produce identical YAML across write sites "+
			"so they round-trip without reformat churn")
}

// Bug 3 — `regenerate manifest --dry-run` must NOT write manifest.yaml.
func TestKeryxBug3_RegenerateManifestDryRunDoesNotWrite(t *testing.T) {
	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop(), Tool: props.Tool{Name: "demo"}}

	// Minimal scaffolded project: go.mod + a root command + one child so the
	// scan produces a manifest different from the on-disk one.
	require.NoError(t, afero.WriteFile(fs, "go.mod", []byte("module demo\n"), 0o644))
	require.NoError(t, fs.MkdirAll("pkg/cmd/root", 0o755))
	require.NoError(t, afero.WriteFile(fs, "pkg/cmd/root/cmd.go", []byte(`package root
import "github.com/spf13/cobra"
func NewCmdRoot(p any) *cobra.Command {
	cmd := &cobra.Command{Use: "demo"}
	return cmd
}`), 0o644))

	require.NoError(t, fs.MkdirAll(".gtb", 0o755))
	original := []byte("version:\n  go_tool_base: sentinel\nproperties:\n  name: demo\ncommands: []\n")
	require.NoError(t, afero.WriteFile(fs, ".gtb/manifest.yaml", original, 0o644))

	// DryRun set — the manifest MUST be untouched.
	g := New(p, &Config{Path: ".", DryRun: true})
	require.NoError(t, g.RegenerateManifest(context.Background()))

	after, err := afero.ReadFile(fs, ".gtb/manifest.yaml")
	require.NoError(t, err)
	assert.Equal(t, string(original), string(after),
		"regenerate manifest --dry-run must not modify manifest.yaml on disk")
}
