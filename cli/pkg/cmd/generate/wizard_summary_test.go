package generate

import (
	"testing"

	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The wizard ends on a summary of every answer and asks before generating.
func TestWizard_EndsOnASummaryAndAConfirm(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Repo: "org/my-app"}
	f, m := startWizard(o)
	m = driveUntilKey(f, m, o, "confirm")
	require.Equal(t, "confirm", f.GetFocusedField().GetKey(), "the last page is the confirm")

	_, ok := advance(f, m)
	require.True(t, ok)
	assert.Equal(t, huh.StateCompleted, f.State)
	assert.True(t, o.confirmed, "Generate now is the default")
}

func TestWizard_CancelOnTheSummaryGeneratesNothing(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Repo: "org/my-app"}
	f, m := startWizard(o)
	m = driveUntilKey(f, m, o, "confirm")
	m = typeAnswer(m, "l")
	_, ok := advance(f, m)
	require.True(t, ok)

	assert.False(t, o.confirmed)
	require.ErrorIs(t, o.confirmedOrCancelled(), huh.ErrUserAborted, "Cancel is the same as ctrl+c")
}

func TestWizardSummary(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{
		Name: "my-app", Description: "does things", Repo: "org/my-app", ForgeBackend: "gitlab", hosted: true,
		Features: []string{"update", "mcp"}, envPrefixChoice: envPrefixDerived,
		ReleaseChannel: "forge", UpdatePolicy: "prompt", MCPMode: "compact",
		ConfigFormats: []string{"toml"}, ConfigFormat: "toml",
		sourceSlots: []wizardSource{{Kind: "vault", Required: true}, {Kind: "vault", Name: "team"}, {}},
	}

	summary := shownNote(t, o.wizardSummary())
	for _, want := range []string{
		"my-app", "does things", "gitlab", "org/my-app", "update, mcp", "MY_APP",
		"prompt", "compact", "toml", "vault (vault), team (vault, optional)",
	} {
		assert.Contains(t, summary, want)
	}
}

// The summary's binding changes with every answer it shows, the unexported
// slot state included, or huh would keep showing a stale summary.
func TestSummaryBinding_TracksTheAnswers(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Name: "my-app", sourceSlots: []wizardSource{{}}}
	before, err := textBinding{o.wizardSummary}.Hash()
	require.NoError(t, err)

	o.sourceSlots[0].Kind = "vault"
	after, err := textBinding{o.wizardSummary}.Hash()
	require.NoError(t, err)

	assert.NotEqual(t, before, after)
}

// A keychain slot whose feature was unticked is dropped, and the summary
// says so before anything is generated.
func TestWizardSummary_LeavesOutADroppedKeychainSlot(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{
		Name: "my-app", Features: []string{"init"},
		sourceSlots: []wizardSource{{Kind: "keychain", Required: true}, {Kind: "vault", Required: true}, {}},
	}

	assert.NotContains(t, shownNote(t, o.wizardSummary()), "keychain")
}
