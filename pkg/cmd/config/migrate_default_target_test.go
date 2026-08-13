package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/phpboyscout/go/credentials"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
)

// Spec 0189 R6/D8: the default migration target follows the environment.
//
// The environment arrives as data. An earlier attempt had resolveMigrateTarget
// discover it — calling utils.IsInteractive and credentials.Probe itself — and
// the suite became non-hermetic: whether a test passed depended on whether
// stdin happened to be a terminal and whether a keychain answered. Taking it as
// a parameter is what makes these assertions mean anything.

func TestResolveMigrateTarget_DefaultsFollowTheEnvironment(t *testing.T) {
	t.Parallel()

	desktop := credentialposture.ModeEnvironment{Interactive: true, KeychainUsable: true}
	ci := credentialposture.ModeEnvironment{}

	tests := []struct {
		name     string
		explicit credentials.Mode
		yaml     string
		env      credentialposture.ModeEnvironment
		want     credentials.Mode
	}{
		{
			name: "interactive with a usable keychain migrates to the keychain",
			env:  desktop,
			want: credentials.ModeKeychain,
		},
		{
			// The zero value is what a caller that says nothing gets, and it
			// must be the behaviour this always had.
			name: "the zero environment keeps the env-var reference",
			env:  ci,
			want: credentials.ModeEnvVar,
		},
		{
			// An operator who stated a preference keeps deterministic
			// behaviour: neither earlier rung consults the environment.
			name:     "an explicit target beats the environment",
			explicit: credentials.ModeEnvVar,
			env:      desktop,
			want:     credentials.ModeEnvVar,
		},
		{
			name: "a configured default beats the environment",
			yaml: "credentials:\n  migrate:\n    default_target: env\n",
			env:  desktop,
			want: credentials.ModeEnvVar,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			yaml := tt.yaml
			if yaml == "" {
				yaml = "other: value\n"
			}

			got, err := resolveMigrateTarget(tt.explicit, testutil.ViewFromYAML(t, yaml), tt.env)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveMigrateTarget_StillRefusesLiteral(t *testing.T) {
	t.Parallel()

	_, err := resolveMigrateTarget(credentials.ModeLiteral,
		testutil.ViewFromYAML(t, "other: value\n"),
		credentialposture.ModeEnvironment{Interactive: true, KeychainUsable: true})

	require.Error(t, err, "migrate moves credentials OFF literal mode, never onto it")
}
