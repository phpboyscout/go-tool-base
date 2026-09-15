package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

// TestForgeBackendsAreValidReleaseSources keeps the chooser and the manifest
// validator on one table: every backend the wizard offers is a release source
// type the manifest accepts (spec 0195 D4), and every one has display data.
func TestForgeBackendsAreValidReleaseSources(t *testing.T) {
	t.Parallel()

	names := forgeBackendNames()
	require.Len(t, names, len(generator.ForgeBackends()), "every backend has display data")

	for _, name := range names {
		require.NoErrorf(t, generator.ValidateReleaseSourceType(name),
			"the wizard offers %q as a backend, but the manifest refuses it as a release source", name)
	}

	assert.Contains(t, names, "gitea", "a forge without a CI skeleton is still offered (D8)")
}

// TestGitBackendOptionsMatchNames pins that the flag's documented set and the
// wizard's offered set come from the same table — the drift that let the
// generator offer GitLab while nothing else agreed it existed.
func TestGitBackendOptionsMatchNames(t *testing.T) {
	t.Parallel()

	names := forgeBackendNames()
	options := forgeBackendOptions()

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
	// Only an unknown value falls back: every registered forge resolves to
	// itself since spec 0195 D4.
	t.Parallel()

	for _, backend := range []string{"", "bogus"} {
		t.Run("backend="+backend, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, "GitHub", backendLabel(backend))
			assert.Equal(t, "github.com", hostForBackend(backend))
			assert.Equal(t, "org/repo", repoPlaceholder(backend))
		})
	}
}
