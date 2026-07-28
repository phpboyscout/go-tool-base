package generate

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// TestScaffold_HasNoGtbIgnore_CurrentGap documents the current-state gap
// reported in issue #3: a fresh `generate project` scaffold writes
// `.gtb/manifest.yaml` but nothing else in `.gtb/`. In particular it does NOT
// scaffold a commented `.gtb/ignore`, so the ignore mechanism is invisible to
// anyone (human or AI agent) inspecting a new project.
//
// This test asserts the CURRENT behaviour. When discoverability change #1
// (scaffold a commented `.gtb/ignore`) is implemented, this test is expected to
// FLIP: the file will start to exist, and the assertion below must be inverted
// to assert presence + a header/comment. Treat the flip as the signal that the
// scaffold change landed.
func TestScaffold_HasNoGtbIgnore_CurrentGap(t *testing.T) {
	fs := afero.NewMemMapFs()
	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool: props.Tool{
			ReleaseSource: props.ReleaseSource{
				Type:  "github",
				Owner: "phpboyscout",
				Repo:  "gtb",
			},
		},
		Version: version.NewInfo("1.2.3", "", ""),
	}

	opts := SkeletonOptions{
		Name:        "test-tool",
		Repo:        "phpboyscout/test-tool",
		Description: "A description of the test tool",
		Path:        "test-project",
	}

	require.NoError(t, opts.Run(context.Background(), p))

	// Sanity: the manifest IS scaffolded today.
	manifestExists, err := afero.Exists(fs, "test-project/.gtb/manifest.yaml")
	require.NoError(t, err)
	assert.True(t, manifestExists, "manifest should be scaffolded")

	// The gap: no .gtb/ignore is scaffolded today.
	ignoreExists, err := afero.Exists(fs, "test-project/.gtb/ignore")
	require.NoError(t, err)
	assert.False(t, ignoreExists,
		"CURRENT GAP (issue #3): a fresh scaffold must not yet ship a .gtb/ignore; "+
			"when scaffolding is added, flip this to assert presence and a commented header")
}
