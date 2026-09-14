package generate

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// TestScaffold_ShipsCommentedGtbIgnore verifies discoverability change #1 from
// issue #3: a fresh `generate project` scaffold now writes a commented, inert
// `.gtb/ignore` alongside `.gtb/manifest.yaml`, so the opt-out mechanism is
// discoverable by anyone (human or AI agent) inspecting a new project.
//
// This test previously documented the GAP (asserting the file's absence); it
// flipped to assert presence + a header comment when the scaffold change landed.
func TestScaffold_ShipsCommentedGtbIgnore(t *testing.T) {
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

	// Sanity: the manifest IS scaffolded.
	manifestExists, err := afero.Exists(fs, "test-project/.gtb/manifest.yaml")
	require.NoError(t, err)
	assert.True(t, manifestExists, "manifest should be scaffolded")

	// A commented, inert .gtb/ignore is now scaffolded alongside it.
	body, err := afero.ReadFile(fs, "test-project/.gtb/ignore")
	require.NoError(t, err, "a fresh scaffold must ship a .gtb/ignore")

	assert.True(t, strings.HasPrefix(string(body), "#"),
		"the scaffolded .gtb/ignore must open with an explanatory header comment")
	assert.Contains(t, string(body), "gtb ignore",
		"the header should point at the gtb ignore command")

	// It is comments-only, so it must be behaviourally inert (ignores nothing).
	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		require.True(t, trimmed == "" || strings.HasPrefix(trimmed, "#"),
			"the scaffolded ignore file must contain only comments and blanks, got rule line: %q", line)
	}
}
