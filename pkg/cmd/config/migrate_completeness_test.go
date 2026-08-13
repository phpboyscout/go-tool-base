package config

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/phpboyscout/go/credentials"
)

// Spec 0189 phase 6 (R7, R9). A migration that ends when the config is rewritten
// has not removed the exposure it was run to fix.

func TestPrintResult_SaysTheOldCredentialIsStillLive(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	PrintResult(&buf, &MigrateResult{Actions: []MigrationAction{{
		SourceKey:        "anthropic.api.key",
		Target:           credentials.ModeEnvVar,
		DestKey:          "anthropic.api.env",
		DestValue:        "ANTHROPIC_API_KEY",
		Verified:         VerifiedEnvVarSet,
		RotationRequired: true,
	}}}, false)

	out := buf.String()

	// The step that actually ends the exposure is the one this command cannot
	// perform, so it has to be unmissable.
	assert.Contains(t, out, "STILL LIVE")
	assert.Contains(t, out, "anthropic.api.key")
	assert.Contains(t, out, "cannot")
	assert.Contains(t, out, "verified: environment variable resolves")
}

func TestPrintResult_DryRunDoesNotClaimAnythingHappened(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	PrintResult(&buf, &MigrateResult{Actions: []MigrationAction{{
		SourceKey: "anthropic.api.key", RotationRequired: true,
	}}}, true)

	out := buf.String()

	assert.NotContains(t, out, "STILL LIVE", "a dry run has not moved anything to rotate away from")
	assert.Contains(t, out, "would still be live")
}

func TestPrintResult_SkippedCredentialsAreNotListedForRotation(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	PrintResult(&buf, &MigrateResult{Actions: []MigrationAction{{
		SourceKey: "github.auth.value", Skipped: true, Reason: "already migrated",
	}}}, false)

	// Nothing moved, so nothing needs rotating — telling an operator to rotate a
	// credential this run did not touch would train them to ignore the notice.
	assert.NotContains(t, buf.String(), "STILL LIVE")
}

func TestPrintResult_UnverifiedMigrationSaysSo(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	PrintResult(&buf, &MigrateResult{Actions: []MigrationAction{{
		SourceKey: "anthropic.api.key", Verified: VerifiedNone, RotationRequired: true,
	}}}, false)

	assert.NotContains(t, buf.String(), "verified:",
		"an unverified migration must not display a verification it does not have")
}

func TestVerifyKeychainReadBack_RefusesWhenTheSecretDoesNotComeBack(t *testing.T) {
	t.Parallel()

	// No keychain backend is linked in this binary, so Retrieve fails — which is
	// the case that matters: the literal must NOT be staged for removal on the
	// strength of a write that returned nil.
	err := verifyKeychainReadBack(t.Context(), MigrateOptions{}, "svc", "acct", "s3cret")

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "s3cret", "the refusal must not quote the secret")
	assert.Contains(t, strings.ToLower(err.Error()), "reading back")
}

func TestVerifyKeychainReadBack_HonoursSkipVerify(t *testing.T) {
	t.Parallel()

	err := verifyKeychainReadBack(t.Context(), MigrateOptions{SkipVerify: true}, "svc", "acct", "s3cret")
	require.NoError(t, err, "--skip-verify is the operator's decision to make")
}
