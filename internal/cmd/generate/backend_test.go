package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

// TestGitBackendsAreScaffoldable is the invariant that keeps the wizard honest.
//
// A forge feature gates a credential wizard; a git backend selects a skeleton
// asset set. Those are different axes, and the generator can only offer a
// backend it has assets for. Deriving the chooser from the forge registry alone
// would have offered Gitea and Bitbucket — which
// generator.ValidateReleaseSourceType rejects two files away, so the project
// would have failed to generate after the user chose it.
func TestGitBackendsAreScaffoldable(t *testing.T) {
	t.Parallel()

	names := gitBackendNames()
	require.NotEmpty(t, names)

	for _, name := range names {
		assert.NoErrorf(t, generator.ValidateReleaseSourceType(name),
			"the wizard offers %q as a git backend, but the generator cannot scaffold it", name)
	}
}

// TestGitBackendOptionsMatchNames pins that the flag's documented set and the
// wizard's offered set come from the same table — the drift that let the
// generator offer GitLab while nothing else agreed it existed.
func TestGitBackendOptionsMatchNames(t *testing.T) {
	t.Parallel()

	names := gitBackendNames()
	options := gitBackendOptions()

	require.Len(t, options, len(names),
		"--git-backend must document exactly the set the wizard offers")

	for i, opt := range options {
		assert.Equal(t, names[i], opt.Value)

		d, ok := forge.DisplayFor(props.FeatureID(opt.Value))
		require.Truef(t, ok, "offered backend %q has no display data", opt.Value)
		assert.Equal(t, d.Label, opt.Key, "the option label must come from the forge profile")
	}
}

// TestBackendAccessorsMatchPreviousBehaviour is the spec 0185 D7 guard at the
// unit level: GitHub and GitLab must render exactly what the four hand-written
// branch functions rendered, so the generator golden-file diff stays a real
// assertion rather than noise.
func TestBackendAccessorsMatchPreviousBehaviour(t *testing.T) {
	t.Parallel()

	tests := []struct {
		backend     string
		label       string
		host        string
		placeholder string
		description string
	}{
		{
			backend:     "github",
			label:       "GitHub",
			host:        "github.com",
			placeholder: "org/repo",
			description: "The repository path in org/repo format.",
		},
		{
			backend:     "gitlab",
			label:       "GitLab",
			host:        "gitlab.com",
			placeholder: "group/subgroup/repo",
			description: "The repository path. GitLab supports nested groups — use the full path and the last segment will be treated as the repository name (e.g. group/subgroup/repo).",
		},
	}

	for _, tt := range tests {
		t.Run(tt.backend, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.label, backendLabel(tt.backend))
			assert.Equal(t, tt.host, hostForBackend(tt.backend))
			assert.Equal(t, tt.placeholder, repoPlaceholder(tt.backend))
			assert.Equal(t, tt.description, repoDescription(tt.backend))
		})
	}
}

// TestBackendAccessorsFallBackToGitHub preserves the old branches' `else`
// arm. Every accessor was `if backend == "gitlab" { … } else { github }`, so an
// unknown or empty value rendered GitHub — and the wizard relies on that while
// the field is still being filled in.
func TestBackendAccessorsFallBackToGitHub(t *testing.T) {
	t.Parallel()

	for _, backend := range []string{"", "bogus", "gitea", "bitbucket"} {
		t.Run("backend="+backend, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, "GitHub", backendLabel(backend))
			assert.Equal(t, "github.com", hostForBackend(backend))
			assert.Equal(t, "org/repo", repoPlaceholder(backend))
		})
	}
}

// TestScaffoldableBackendsAreRegisteredForges guards the other direction: an
// entry in scaffoldableBackends that is not a registered forge would render a
// blank option in the chooser.
func TestScaffoldableBackendsAreRegisteredForges(t *testing.T) {
	t.Parallel()

	for id := range scaffoldableBackends {
		_, ok := forge.DisplayFor(id)
		assert.Truef(t, ok, "scaffoldable backend %q is not a registered forge", id)
	}
}
