package credentialposture_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/phpboyscout/go/credentials"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
)

func TestDefaultStorageMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  credentialposture.ModeEnvironment
		want credentials.Mode
	}{
		{
			// The case R6 exists for: a developer at a terminal with a working
			// keychain should not get their secret exported into every child
			// process by default.
			name: "interactive with a usable keychain defaults to keychain",
			env:  credentialposture.ModeEnvironment{Interactive: true, KeychainUsable: true},
			want: credentials.ModeKeychain,
		},
		{
			// A pipeline has no terminal, so CI needs no special case.
			name: "non-interactive defaults to an env reference",
			env:  credentialposture.ModeEnvironment{Interactive: false, KeychainUsable: true},
			want: credentials.ModeEnvVar,
		},
		{
			// A headless box with no Secret Service, or a locked keychain:
			// choosing a store that will not work moves the failure later.
			name: "interactive without a usable keychain defaults to an env reference",
			env:  credentialposture.ModeEnvironment{Interactive: true, KeychainUsable: false},
			want: credentials.ModeEnvVar,
		},
		{
			name: "neither defaults to an env reference",
			env:  credentialposture.ModeEnvironment{},
			want: credentials.ModeEnvVar,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, credentialposture.DefaultStorageMode(tt.env))
		})
	}
}

func TestRecommendedLabel_FollowsTheActualDefault(t *testing.T) {
	t.Parallel()

	desktop := credentialposture.ModeEnvironment{Interactive: true, KeychainUsable: true}
	ci := credentialposture.ModeEnvironment{}

	// The prompt must not recommend one option while pre-selecting another.
	assert.Equal(t, " (recommended)", credentialposture.RecommendedLabel(desktop, credentials.ModeKeychain))
	assert.Empty(t, credentialposture.RecommendedLabel(desktop, credentials.ModeEnvVar))

	assert.Equal(t, " (recommended)", credentialposture.RecommendedLabel(ci, credentials.ModeEnvVar))
	assert.Empty(t, credentialposture.RecommendedLabel(ci, credentials.ModeKeychain))

	// Literal is never recommended, in any environment.
	assert.Empty(t, credentialposture.RecommendedLabel(desktop, credentials.ModeLiteral))
	assert.Empty(t, credentialposture.RecommendedLabel(ci, credentials.ModeLiteral))
}

// These two exercise the discovering edge. They are deterministic here for a
// structural reason worth stating: this test binary links no keychain backend,
// so credentials.Probe short-circuits to false without touching anything, and
// `go test` does not give the process a terminal. Both facts are properties of
// the binary rather than of the machine, so these do not become the
// environment-dependent assertions this whole design exists to avoid.

func TestDiscoverModeEnvironment_NoKeychainBackendLinked(t *testing.T) {
	t.Parallel()

	env := credentialposture.DiscoverModeEnvironment(context.Background())

	assert.False(t, env.KeychainUsable,
		"Probe must report false when no backend is registered, rather than probing something")
}

func TestStorageModeOptions_OffersAndDefaultsConsistently(t *testing.T) {
	t.Parallel()

	choices, defaultMode := credentialposture.StorageModeOptions(context.Background(),
		credentialposture.ModeLabels{
			Env:      "Environment variable reference",
			Keychain: "OS keychain",
			Literal:  "Literal value in config file (plaintext)",
		})

	require.NotEmpty(t, choices)

	// Env var is always offered and always first.
	assert.Equal(t, credentials.ModeEnvVar, choices[0].Mode)

	// With no usable keychain it is not offered — an option that fails the
	// moment it is chosen is worse than one never shown.
	for _, c := range choices {
		assert.NotEqual(t, credentials.ModeKeychain, c.Mode)
	}

	// And the recommendation must be on the option that is actually the
	// default, not pinned to one of them.
	assert.Equal(t, credentials.ModeEnvVar, defaultMode)
	assert.Contains(t, choices[0].Label, "(recommended)")
}

func TestDefaultStorageMode_CIIsExcludedEvenWithATerminal(t *testing.T) {
	t.Parallel()

	// The case that turned a pipeline red. A terminal is not evidence of a
	// human: GitLab's runners allocate a TTY, so IsInteractive reports true
	// inside CI. The keychain probe happened to mask it — a runner has no
	// keychain — but a rule that is only right because a second condition
	// rescues it is a rule waiting to be wrong.
	ciWithTerminalAndKeychain := credentialposture.ModeEnvironment{
		CI:             true,
		Interactive:    true,
		KeychainUsable: true,
	}

	assert.Equal(t, credentials.ModeEnvVar,
		credentialposture.DefaultStorageMode(ciWithTerminalAndKeychain),
		"a CI run takes the CI default however interactive it looks")

	assert.Empty(t, credentialposture.RecommendedLabel(ciWithTerminalAndKeychain, credentials.ModeKeychain))
	assert.Equal(t, " (recommended)",
		credentialposture.RecommendedLabel(ciWithTerminalAndKeychain, credentials.ModeEnvVar))
}
