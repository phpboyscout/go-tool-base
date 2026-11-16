package generator

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

func TestManifestProperties_MCPWithhold_YAMLRoundTrip(t *testing.T) {
	t.Parallel()

	out, err := yaml.Marshal(ManifestProperties{Name: "t", MCP: ManifestMCP{Withhold: []string{"config/trust", "sign"}}})
	require.NoError(t, err)
	assert.Contains(t, string(out), "mcp:\n    withhold:\n        - config/trust\n        - sign\n")
	assert.NotContains(t, string(out), "mode:")

	var back ManifestProperties
	require.NoError(t, yaml.Unmarshal(out, &back))
	assert.Equal(t, []string{"config/trust", "sign"}, back.MCP.Withhold)

	bare, err := yaml.Marshal(ManifestProperties{Name: "t"})
	require.NoError(t, err)
	assert.NotContains(t, string(bare), "mcp:")
}

func TestValidateMCPWithholdPath(t *testing.T) {
	t.Parallel()

	// import and options are refused as generated command names, but an
	// attached command can carry either.
	for _, ok := range []string{"sign", "config/trust", "keys/import", "options", "a/b/c", "key_s/pub-lish"} {
		require.NoError(t, ValidateMCPWithholdPath(ok), ok)
	}

	for _, bad := range []string{
		"", " ", "/sign", "sign/", "config//trust", "Sign", "config/Trust",
		"config.trust", "config trust", "sign\n", "../sign", "config\\trust", "1sign",
	} {
		require.ErrorIs(t, ValidateMCPWithholdPath(bad), ErrInvalidInput, "%q", bad)
	}
}

func TestValidateManifest_ChecksEachWithheldPath(t *testing.T) {
	t.Parallel()

	g, _, _ := newPerimeterTestProject(t, settingsManifest)

	m, err := g.loadManifest()
	require.NoError(t, err)

	m.Properties.MCP.Withhold = []string{"sign", "config/trust"}
	require.NoError(t, ValidateManifest(m))

	m.Properties.MCP.Withhold = []string{"sign", "Config/Trust"}
	require.ErrorIs(t, ValidateManifest(m), ErrInvalidInput)
}

func renderRootWithMCP(t *testing.T, mode string, withhold ...string) string {
	t.Helper()

	src, err := renderRoot(t, templates.SkeletonRootData{Name: "tool", Description: "d", MCPMode: mode, MCPWithhold: withhold})
	require.NoError(t, err)

	return src
}

func TestSkeletonRoot_RendersTheWithholdList(t *testing.T) {
	t.Parallel()

	const list = `Withhold: []string{"config/trust", "sign"}`

	compact := renderRootWithMCP(t, "compact", "config/trust", "sign")
	assert.Contains(t, compact, "props.MCPConfig{"+list+"}")
	assert.NotContains(t, compact, "MCPDirect")

	unset := renderRootWithMCP(t, "", "config/trust", "sign")
	assert.Contains(t, unset, "props.MCPConfig{"+list+"}")

	direct := renderRootWithMCP(t, "direct", "config/trust", "sign")
	assert.Contains(t, direct, "props.MCPConfig{")
	assert.Contains(t, direct, "Mode:")
	assert.Contains(t, direct, "props.MCPDirect")
	assert.Contains(t, direct, list)

	assert.NotContains(t, renderRootWithMCP(t, "compact"), "MCPConfig", "compact with no list renders nothing")
	assert.NotContains(t, renderRootWithMCP(t, ""), "MCPConfig")
}

func TestExtractProperties_ReadsTheWithholdListBack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mode     string
		withhold []string
	}{
		{name: "compact with a list", withhold: []string{"config/trust", "sign"}},
		{name: "direct with a list", mode: "direct", withhold: []string{"sign"}},
		{name: "direct with no list", mode: "direct"},
		{name: "neither"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			g := New(&props.Props{FS: fs, Logger: logger.NewNoop()}, &Config{Path: "/work"})

			path := "/work/pkg/cmd/root/cmd.go"
			require.NoError(t, afero.WriteFile(fs, path, []byte(renderRootWithMCP(t, tt.mode, tt.withhold...)), 0o644))

			properties, _, err := g.extractProjectProperties(path)
			require.NoError(t, err)
			assert.Equal(t, tt.withhold, properties.MCP.Withhold)
			assert.Equal(t, tt.mode, properties.MCP.Mode)
		})
	}
}

// The list has one way in and one way out, so gtb unset cannot clear it.
func TestSetSetting_RefusesTheWithholdList(t *testing.T) {
	t.Parallel()

	g, _, _ := newPerimeterTestProject(t, "properties:\n  name: mytool\n  mcp:\n    withhold:\n      - sign\n"+
		"release_source:\n  type: github\n  backend: github\n  host: github.com\n  owner: org\n  repo: mytool\n"+
		"version:\n  gtb: v1.0.0\n  go: \"1.26\"\ncommands: []\n")

	for _, path := range []string{"mcp.withhold", "properties.mcp.withhold"} {
		err := g.SetSetting(context.Background(), path, []string{"keys"})
		require.ErrorIs(t, err, ErrSettingReadOnly, path)
		assert.Contains(t, errors.FlattenHints(err), "gtb disable mcp <path>")

		require.ErrorIs(t, g.UnsetSetting(context.Background(), path), ErrSettingReadOnly, path)
	}

	m, err := g.loadManifest()
	require.NoError(t, err)
	assert.Equal(t, []string{"sign"}, m.Properties.MCP.Withhold)

	require.NoError(t, g.SetSetting(context.Background(), "mcp.mode", []string{"direct"}), "the mode beside it stays settable")
}

func withheldIn(t *testing.T, g *Generator) []string {
	t.Helper()

	m, err := g.loadManifest()
	require.NoError(t, err)

	return m.Properties.MCP.Withhold
}

func manifestSource(t *testing.T, fs afero.Fs) string {
	t.Helper()

	src, err := afero.ReadFile(fs, "/work/.gtb/manifest.yaml")
	require.NoError(t, err)

	return string(src)
}

func rootSource(t *testing.T, fs afero.Fs) string {
	t.Helper()

	src, err := afero.ReadFile(fs, "/work/pkg/cmd/root/cmd.go")
	require.NoError(t, err)

	return string(src)
}

func TestSetMCPExposure(t *testing.T) {
	t.Parallel()

	t.Run("a path gtb did not generate goes on the list, sorted, once", func(t *testing.T) {
		t.Parallel()

		g, fs, _ := newPerimeterTestProject(t, wizardManifest)

		report, err := g.SetMCPExposure(context.Background(), []string{"sign", "config/trust"}, false)
		require.NoError(t, err)
		assert.Equal(t, MCPExposureReport{Tool: "mytool", Listed: []string{"sign", "config/trust"}}, report)
		assert.Equal(t, []string{"config/trust", "sign"}, withheldIn(t, g))
		assert.Contains(t, rootSource(t, fs), `Withhold: []string{"config/trust", "sign"}`)

		report, err = g.SetMCPExposure(context.Background(), []string{"sign", "keys"}, false)
		require.NoError(t, err)
		assert.Equal(t, MCPExposureReport{Tool: "mytool", Listed: []string{"keys"}, Unchanged: []string{"sign"}}, report)
		assert.Equal(t, []string{"config/trust", "keys", "sign"}, withheldIn(t, g))
	})

	t.Run("a path under a listed one is still recorded", func(t *testing.T) {
		t.Parallel()

		g, _, _ := newPerimeterTestProject(t, wizardManifest)

		_, err := g.SetMCPExposure(context.Background(), []string{"keys"}, false)
		require.NoError(t, err)

		report, err := g.SetMCPExposure(context.Background(), []string{"keys/publish"}, false)
		require.NoError(t, err)
		assert.Equal(t, []string{"keys/publish"}, report.Listed)
		assert.Equal(t, []string{"keys", "keys/publish"}, withheldIn(t, g))
	})

	t.Run("a generated command keeps its own entry and marker", func(t *testing.T) {
		t.Parallel()

		g, fs, _ := newPerimeterTestProject(t, wizardManifest)

		report, err := g.SetMCPExposure(context.Background(), []string{"hello", "sign"}, false)
		require.NoError(t, err)
		assert.Equal(t, MCPExposureReport{Tool: "mytool", Commands: []string{"hello"}, Listed: []string{"sign"}}, report)
		assert.Equal(t, []string{"sign"}, withheldIn(t, g), "hello is not on the list")

		m, err := g.loadManifest()
		require.NoError(t, err)
		require.NotNil(t, m.Commands[0].MCPEnabled)
		assert.False(t, *m.Commands[0].MCPEnabled)

		cmdGo, err := afero.ReadFile(fs, "/work/pkg/cmd/hello/cmd.go")
		require.NoError(t, err)
		assert.Contains(t, string(cmdGo), "setup.ExcludeFromMCP(cmd)")

		report, err = g.SetMCPExposure(context.Background(), []string{"hello"}, true)
		require.NoError(t, err)
		assert.Equal(t, MCPExposureReport{Tool: "mytool", Commands: []string{"hello"}}, report)
		assert.Equal(t, []string{"sign"}, withheldIn(t, g))
	})

	t.Run("enable takes a listed path off", func(t *testing.T) {
		t.Parallel()

		g, fs, _ := newPerimeterTestProject(t, wizardManifest)

		_, err := g.SetMCPExposure(context.Background(), []string{"sign", "config/trust"}, false)
		require.NoError(t, err)

		report, err := g.SetMCPExposure(context.Background(), []string{"sign"}, true)
		require.NoError(t, err)
		assert.Equal(t, MCPExposureReport{Tool: "mytool", Listed: []string{"sign"}}, report)
		assert.Equal(t, []string{"config/trust"}, withheldIn(t, g))
		assert.Contains(t, rootSource(t, fs), `Withhold: []string{"config/trust"}`)

		_, err = g.SetMCPExposure(context.Background(), []string{"config/trust"}, true)
		require.NoError(t, err)
		assert.Empty(t, withheldIn(t, g))
		assert.NotContains(t, rootSource(t, fs), "MCPConfig", "an empty list renders nothing")

		assert.NotContains(t, manifestSource(t, fs), "withhold")
	})

	t.Run("enable under a listed ancestor fails, names it and writes nothing", func(t *testing.T) {
		t.Parallel()

		g, fs, _ := newPerimeterTestProject(t, wizardManifest)

		_, err := g.SetMCPExposure(context.Background(), []string{"keys", "keys/publish", "sign"}, false)
		require.NoError(t, err)

		before := manifestSource(t, fs)

		_, err = g.SetMCPExposure(context.Background(), []string{"sign", "keys/publish"}, true)
		require.ErrorIs(t, err, ErrMCPPathStillWithheld)
		assert.Contains(t, err.Error(), "keys/publish")
		assert.Contains(t, errors.FlattenHints(err), "gtb enable mcp keys")

		assert.Equal(t, before, manifestSource(t, fs), "sign stays listed: nothing is written when one path is refused")

		_, err = g.SetMCPExposure(context.Background(), []string{"keys/rotate"}, true)
		require.ErrorIs(t, err, ErrMCPPathStillWithheld, "a path that is not itself listed is covered too")
	})

	t.Run("enabling the ancestor in the same call clears the way", func(t *testing.T) {
		t.Parallel()

		g, _, _ := newPerimeterTestProject(t, wizardManifest)

		_, err := g.SetMCPExposure(context.Background(), []string{"keys", "keys/publish"}, false)
		require.NoError(t, err)

		report, err := g.SetMCPExposure(context.Background(), []string{"keys/publish", "keys"}, true)
		require.NoError(t, err)
		assert.Equal(t, []string{"keys/publish", "keys"}, report.Listed)
		assert.Empty(t, withheldIn(t, g))
	})

	t.Run("enabling a path that is neither listed nor generated changes nothing", func(t *testing.T) {
		t.Parallel()

		g, fs, _ := newPerimeterTestProject(t, wizardManifest)

		before := manifestSource(t, fs)

		report, err := g.SetMCPExposure(context.Background(), []string{"sign"}, true)
		require.NoError(t, err)
		assert.Equal(t, MCPExposureReport{Tool: "mytool", Unchanged: []string{"sign"}}, report)

		assert.Equal(t, before, manifestSource(t, fs))
	})

	t.Run("a path that is not command names is refused before anything is written", func(t *testing.T) {
		t.Parallel()

		g, fs, _ := newPerimeterTestProject(t, wizardManifest)

		before := manifestSource(t, fs)

		for _, exposed := range []bool{false, true} {
			_, err := g.SetMCPExposure(context.Background(), []string{"sign", "Config/Trust"}, exposed)
			require.ErrorIs(t, err, ErrInvalidInput)
		}

		assert.Equal(t, before, manifestSource(t, fs))
	})

	t.Run("a path named twice is handled once", func(t *testing.T) {
		t.Parallel()

		g, _, _ := newPerimeterTestProject(t, wizardManifest)

		report, err := g.SetMCPExposure(context.Background(), []string{"sign", "sign"}, false)
		require.NoError(t, err)
		assert.Equal(t, MCPExposureReport{Tool: "mytool", Listed: []string{"sign"}}, report)
		assert.Equal(t, []string{"sign"}, withheldIn(t, g))
	})

	t.Run("exposing a generated command also takes a hand-listed copy off", func(t *testing.T) {
		t.Parallel()

		g, _, _ := newPerimeterTestProject(t, strings.Replace(wizardManifest,
			"  name: mytool\n", "  name: mytool\n  mcp:\n    withhold:\n      - hello\n      - sign\n", 1))
		require.Equal(t, []string{"hello", "sign"}, withheldIn(t, g))

		report, err := g.SetMCPExposure(context.Background(), []string{"hello"}, true)
		require.NoError(t, err)
		assert.Equal(t, MCPExposureReport{Tool: "mytool", Commands: []string{"hello"}}, report)
		assert.Equal(t, []string{"sign"}, withheldIn(t, g))
	})

	t.Run("a protected generated command is refused, and the list is not written", func(t *testing.T) {
		t.Parallel()

		g, _, _ := newPerimeterTestProject(t, wizardManifest+"  - name: locked\n    description: l\n    protected: true\n")

		_, err := g.SetMCPExposure(context.Background(), []string{"sign", "locked"}, false)
		require.ErrorIs(t, err, ErrCommandProtected)
		assert.Empty(t, withheldIn(t, g))
	})
}

// The wizard writes every author setting back from its pages, none of which
// asks about the list (spec 0213 D6).
func TestApplyAuthorSettings_KeepsTheWithholdList(t *testing.T) {
	t.Parallel()

	g, _, _ := newPerimeterTestProject(t, wizardManifest)

	_, err := g.SetMCPExposure(context.Background(), []string{"sign"}, false)
	require.NoError(t, err)

	m, err := g.LoadManifest()
	require.NoError(t, err)

	cfg := SkeletonConfigFromManifest(*m)
	cfg.MCPWithhold = nil
	cfg.Description = "new"
	require.NoError(t, g.ApplyAuthorSettings(context.Background(), cfg))

	assert.Equal(t, []string{"sign"}, withheldIn(t, g))
}

func TestRegenerateManifest_RecoversTheWithholdList(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop(), Config: emptyTestStore(t), Version: version.NewInfo("v1.0.0", "", "")}

	const path = "/proj"

	g := New(p, &Config{Path: path})
	g.runCommand = func(context.Context, string, string, ...string) ([]byte, error) { return nil, nil }

	require.NoError(t, g.GenerateSkeleton(context.Background(), SkeletonConfig{
		Name: "roundtrip", Repo: "acme/roundtrip", Host: "gitlab.com", ForgeBackend: "gitlab",
		Description: "round trip", Path: path,
		MCPMode: "direct", MCPWithhold: []string{"config/trust", "sign"},
	}))

	before := readManifest(t, fs, path)
	require.Equal(t, ManifestMCP{Mode: "direct", Withhold: []string{"config/trust", "sign"}}, before.Properties.MCP)

	require.NoError(t, fs.Remove(ManifestPathFor(path)))
	require.NoError(t, g.RegenerateManifest(context.Background()))

	assert.Equal(t, before.Properties.MCP, readManifest(t, fs, path).Properties.MCP)
}
