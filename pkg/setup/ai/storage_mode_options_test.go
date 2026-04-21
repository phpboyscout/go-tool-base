package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/credentials"
)

// These tests exercise the pure option-building logic — independent
// of whether the keychain tag was compiled in — so we can assert the
// CI / probe matrix without depending on real backend state.

func TestStorageModeOptions_EnvVarOnlyUnderCI(t *testing.T) {
	t.Parallel()

	// CI=true + probe=false: only env-var. No keychain (probe failed),
	// no literal (CI refuses literal). The one remaining option must
	// be the recommended env-var reference.
	opts := storageModeOptions(true, false)
	require.Len(t, opts, 1)
	assert.Equal(t, credentials.ModeEnvVar, opts[0].Value)
}

func TestStorageModeOptions_KeychainAddedWhenProbeSucceeds(t *testing.T) {
	t.Parallel()

	opts := storageModeOptions(false, true)

	// env-var, keychain, literal — in that order.
	require.Len(t, opts, 3)
	assert.Equal(t, credentials.ModeEnvVar, opts[0].Value)
	assert.Equal(t, credentials.ModeKeychain, opts[1].Value)
	assert.Equal(t, credentials.ModeLiteral, opts[2].Value)
}

func TestStorageModeOptions_KeychainHiddenWhenProbeFails(t *testing.T) {
	t.Parallel()

	opts := storageModeOptions(false, false)

	// env-var + literal only — keychain is omitted because the probe
	// found the backend unreachable. The wizard must never offer a
	// storage mode that will fail the moment the user picks it.
	require.Len(t, opts, 2)
	assert.Equal(t, credentials.ModeEnvVar, opts[0].Value)
	assert.Equal(t, credentials.ModeLiteral, opts[1].Value)
}

func TestStorageModeOptions_KeychainSurvivesCI(t *testing.T) {
	t.Parallel()

	// CI=true + probe=true is unusual (containers rarely have a
	// reachable keychain) but if the user has engineered such an
	// environment, keychain should still be offered — literal stays
	// hidden either way.
	opts := storageModeOptions(true, true)
	require.Len(t, opts, 2)
	assert.Equal(t, credentials.ModeEnvVar, opts[0].Value)
	assert.Equal(t, credentials.ModeKeychain, opts[1].Value)
}
