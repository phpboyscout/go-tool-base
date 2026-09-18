package changelog

import (
	"bytes"
	"testing"
	"testing/fstest"

	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/changelog"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// assetsWith builds a props.Assets exposing a CHANGELOG.md at the path the
// command reads (assets/CHANGELOG.md) with the given content.
func assetsWith(content string) *props.Assets {
	return props.NewAssets(props.AssetMap{
		"test": fstest.MapFS{
			changelogAssetPath: &fstest.MapFile{Data: []byte(content)},
		},
	})
}

const sampleChangelog = `# v1.2.0

### Features

- add a thing

# v1.1.0

### Bug Fixes

- fix a thing
`

func TestLoadChangelog(t *testing.T) {
	t.Parallel()

	t.Run("no assets configured", func(t *testing.T) {
		t.Parallel()

		_, err := loadChangelog(&props.Props{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "assets not configured")
	})

	t.Run("CHANGELOG.md missing", func(t *testing.T) {
		t.Parallel()

		p := &props.Props{Assets: props.NewAssets(props.AssetMap{
			"test": fstest.MapFS{"assets/other.md": &fstest.MapFile{Data: []byte("x")}},
		})}

		_, err := loadChangelog(p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("empty changelog", func(t *testing.T) {
		t.Parallel()

		_, err := loadChangelog(&props.Props{Assets: assetsWith("   \n  ")})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty")
	})

	t.Run("valid changelog", func(t *testing.T) {
		t.Parallel()

		got, err := loadChangelog(&props.Props{Assets: assetsWith(sampleChangelog)})
		require.NoError(t, err)
		assert.Contains(t, got, "v1.2.0")
	})
}

func TestFilterReleases(t *testing.T) {
	t.Parallel()

	cl := &changelog.Changelog{Releases: []changelog.Release{
		{Version: "v1.0.0"},
		{Version: "v1.1.0"},
		{Version: "v1.2.0"},
	}}

	t.Run("latest returns the most recent", func(t *testing.T) {
		t.Parallel()

		got := filterReleases(cl, "", "", true)
		require.Len(t, got, 1)
		assert.Equal(t, "v1.2.0", got[0].Version)
	})

	t.Run("latest on empty changelog returns all (none)", func(t *testing.T) {
		t.Parallel()

		got := filterReleases(&changelog.Changelog{}, "", "", true)
		assert.Empty(t, got)
	})

	t.Run("version match", func(t *testing.T) {
		t.Parallel()

		got := filterReleases(cl, "v1.1.0", "", false)
		require.Len(t, got, 1)
		assert.Equal(t, "v1.1.0", got[0].Version)
	})

	t.Run("version no match", func(t *testing.T) {
		t.Parallel()

		assert.Nil(t, filterReleases(cl, "v9.9.9", "", false))
	})

	t.Run("since is exclusive, newest first", func(t *testing.T) {
		t.Parallel()

		got := filterReleases(cl, "", "v1.0.0", false)
		require.Len(t, got, 2)
		assert.Equal(t, "v1.2.0", got[0].Version)
		assert.Equal(t, "v1.1.0", got[1].Version)
	})

	// F20 of the v0.43.0 manual round: the parsed changelog is oldest-first,
	// and the command rendered it that way, so a reader scrolled past every
	// release to reach the current one. A changelog reads newest-first.
	t.Run("default returns all, newest first", func(t *testing.T) {
		t.Parallel()

		got := filterReleases(cl, "", "", false)
		require.Len(t, got, 3)
		assert.Equal(t, "v1.2.0", got[0].Version)
		assert.Equal(t, "v1.0.0", got[2].Version)
	})
}

// cmdWithOutput returns a cobra.Command carrying an --output flag (set to
// format) and a captured stdout buffer, mirroring how the root wires JSON
// output detection.
func cmdWithOutput(format string) (*cobra.Command, *bytes.Buffer) {
	cmd := &cobra.Command{Use: "changelog"}
	cmd.Flags().String("output", format, "")

	var buf bytes.Buffer

	cmd.SetOut(&buf)

	return cmd, &buf
}

func TestRenderOutput(t *testing.T) {
	t.Parallel()

	releases := []changelog.Release{{
		Version: "v1.2.0",
		Entries: []changelog.Entry{
			{Category: changelog.CategoryFix, Scope: "http", Description: "add server"},
			{Category: changelog.CategoryOther, Description: "tidy up"},
		},
	}}

	t.Run("no releases prints a friendly message", func(t *testing.T) {
		t.Parallel()

		cmd, out := cmdWithOutput("")

		require.NoError(t, renderOutput(cmd, nil))
		assert.Contains(t, out.String(), "No matching changelog entries")
	})

	t.Run("json output emits a structured response with named fields", func(t *testing.T) {
		t.Parallel()

		cmd, out := cmdWithOutput("json")

		require.NoError(t, renderOutput(cmd, releases))
		// F20: the wire shape is this command's, not the parser's Go struct:
		// snake_case keys and a category by name rather than an iota.
		assert.Contains(t, out.String(), "\"status\"")
		assert.Contains(t, out.String(), "\"version\": \"v1.2.0\"")
		assert.Contains(t, out.String(), "\"category\": \"fix\"")
		assert.Contains(t, out.String(), "\"scope\": \"http\"")
		assert.NotContains(t, out.String(), "\"Category\": 2")
	})

	t.Run("text output on a pipe is plain markdown, newest release first", func(t *testing.T) {
		t.Parallel()

		cmd, out := cmdWithOutput("")

		two := append([]changelog.Release{{Version: "v1.3.0", Entries: []changelog.Entry{{Description: "newer"}}}}, releases...)
		require.NoError(t, renderOutput(cmd, two))

		// F20: a buffer is not a terminal, so no ANSI, and each heading starts
		// its own block rather than rendering as literal text inside the
		// previous release's list.
		s := out.String()
		assert.Equal(t, s, ansi.Strip(s), "no escape sequences on a non-terminal")
		assert.Contains(t, s, "## v1.3.0\n\n- newer\n\n## v1.2.0\n\n- **http:** add server\n- tidy up\n")
	})
}

func TestNewCmdChangelog(t *testing.T) {
	t.Parallel()

	t.Run("metadata and flags", func(t *testing.T) {
		t.Parallel()

		cmd := NewCmdChangelog(&props.Props{})
		require.NotNil(t, cmd)
		assert.Equal(t, "changelog", cmd.Use)

		for _, f := range []string{"version", "since", "latest"} {
			assert.NotNil(t, cmd.Flags().Lookup(f), "flag %q must exist", f)
		}
	})

	t.Run("RunE renders the embedded changelog", func(t *testing.T) {
		t.Parallel()

		p := &props.Props{Assets: assetsWith(sampleChangelog)}

		cmd := NewCmdChangelog(p)

		var buf bytes.Buffer

		cmd.SetOut(&buf)

		require.NoError(t, cmd.RunE(cmd.Command, nil))
		assert.Contains(t, buf.String(), "v1.2.0")
	})

	t.Run("RunE surfaces a missing changelog", func(t *testing.T) {
		t.Parallel()

		cmd := NewCmdChangelog(&props.Props{Logger: logger.NewBuffer()})
		require.Error(t, cmd.RunE(cmd.Command, nil))
	})

	t.Run("RunE with --latest shows the most recent release only", func(t *testing.T) {
		t.Parallel()

		cmd := NewCmdChangelog(&props.Props{Assets: assetsWith(sampleChangelog)})

		var buf bytes.Buffer

		cmd.SetOut(&buf)

		require.NoError(t, cmd.Flags().Set("latest", "true"))
		require.NoError(t, cmd.RunE(cmd.Command, nil))
		assert.Contains(t, buf.String(), "v1.2.0")
		assert.NotContains(t, buf.String(), "v1.1.0", "--latest must exclude older releases")
	})
}
