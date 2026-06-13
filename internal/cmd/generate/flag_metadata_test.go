package generate

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// TestAddFlag_PreservesCommandMetadata is the regression guard for the
// `add-flag-regen-drops-command-metadata` audit finding. add-flag used to
// rebuild a partial CommandData from a handful of manifest fields, so a
// command's aliases, required/shorthand flags, and pre-run hook were dropped
// from the regenerated cmd.go, and the refreshed cmd.go hash was never written
// back to the manifest. This test sets up a command carrying all three and
// asserts they survive add-flag — both in the generated cmd.go and the manifest
// — and that the stored hash is refreshed.
func TestAddFlag_PreservesCommandMetadata(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := &props.Props{
		FS:      fs,
		Logger:  logger.NewNoop(),
		Version: version.NewInfo("v1.0.0", "", ""),
	}

	projectRoot := "/project"
	require.NoError(t, fs.MkdirAll(projectRoot, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(projectRoot, "go.mod"),
		[]byte("module example.com/test-project\n"), 0o644))

	// Manifest with a command that has: aliases, a required+shorthand flag,
	// and a persistent pre-run hook. A deliberately stale cmd.go hash lets us
	// assert the hash is rewritten after regeneration.
	manifestDir := filepath.Join(projectRoot, ".gtb")
	require.NoError(t, fs.MkdirAll(manifestDir, 0o755))

	manifestContent := `properties:
  name: test-project
github:
  org: test-org
  repo: test-project
version:
  gtb: v1.0.0
commands:
- name: deploy
  description: Deploy command
  aliases:
  - dep
  - ship
  persistent_pre_run: true
  hashes:
    cmd.go: stalehash
  flags:
  - name: target
    type: string
    description: Deployment target
    shorthand: t
    required: true
`
	require.NoError(t, afero.WriteFile(fs, filepath.Join(manifestDir, "manifest.yaml"),
		[]byte(manifestContent), 0o644))

	opts := AddFlagOptions{
		CommandName: "deploy",
		FlagName:    "verbose",
		FlagType:    "bool",
		Description: "Verbose output",
		Path:        projectRoot,
	}

	require.NoError(t, opts.Run(context.Background(), p))

	cmdGo, err := afero.ReadFile(fs, filepath.Join(projectRoot, "pkg/cmd/deploy/cmd.go"))
	require.NoError(t, err)
	gen := string(cmdGo)

	// Aliases must survive.
	assert.Contains(t, gen, `"dep"`, "alias dep must survive add-flag")
	assert.Contains(t, gen, `"ship"`, "alias ship must survive add-flag")

	// Required shorthand flag must survive (shorthand "t" + MarkFlagRequired).
	assert.Contains(t, gen, `"target"`, "target flag must survive add-flag")
	assert.Contains(t, gen, `"t"`, "shorthand for target must survive add-flag")
	assert.Contains(t, gen, "MarkFlagRequired", "required marking for target must survive add-flag")

	// Pre-run hook must survive.
	assert.Contains(t, gen, "PersistentPreRunE", "persistent pre-run hook must survive add-flag")

	// The newly added flag must be present.
	assert.Contains(t, gen, `"verbose"`, "newly added flag must be rendered")

	// Manifest must still carry aliases, the hook, and the required/shorthand
	// flag, plus the new flag — and the cmd.go hash must be refreshed.
	manifestUpdated, err := afero.ReadFile(fs, filepath.Join(projectRoot, ".gtb/manifest.yaml"))
	require.NoError(t, err)
	man := string(manifestUpdated)

	assert.Contains(t, man, "dep", "manifest must retain alias dep")
	assert.Contains(t, man, "ship", "manifest must retain alias ship")
	assert.Contains(t, man, "persistent_pre_run: true", "manifest must retain pre-run hook")
	assert.Contains(t, man, "shorthand: t", "manifest must retain shorthand")
	assert.Contains(t, man, "required: true", "manifest must retain required marking")
	assert.Contains(t, man, "name: verbose", "manifest must record the new flag")
	assert.NotContains(t, man, "stalehash", "stale cmd.go hash must be refreshed after regeneration")
}
