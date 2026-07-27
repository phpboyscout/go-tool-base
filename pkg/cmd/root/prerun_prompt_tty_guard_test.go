package root

import (
	"context"
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"charm.land/huh/v2"

	forgetest "gitlab.com/phpboyscout/go/forge/test"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// nonInteractiveStdin replaces os.Stdin with the read end of a pipe whose write
// end is closed, so utils.IsInteractive() deterministically reports false (a
// pipe is not a character device) regardless of whether the test runner itself
// holds a terminal. Reads return EOF immediately, so any huh form reached on the
// pre-fix code path errors out rather than hanging. Restored on cleanup. Not
// safe for t.Parallel — it mutates the process-global os.Stdin.
func nonInteractiveStdin(t *testing.T) {
	t.Helper()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	require.NoError(t, w.Close())

	orig := os.Stdin
	os.Stdin = r

	t.Cleanup(func() {
		os.Stdin = orig

		_ = r.Close()
	})
}

// msgContainer is satisfied by the buffer logger (logger.NewBuffer), exposing
// its captured-message search so a test can assert whether a given log line was
// emitted — i.e. whether a code path was entered.
type msgContainer interface {
	Contains(string) bool
}

func consentBuffer(t *testing.T, props *p.Props) msgContainer {
	t.Helper()

	c, ok := props.Logger.(msgContainer)
	require.True(t, ok, "test props must use logger.NewBuffer")

	return c
}

// TestPromptTelemetryConsent_CIEnvVarSkipsPrompt proves the consent prompt is
// skipped when CI=true is set in the environment (parity with the --ci flag /
// `ci` config key). On unmodified origin/main only the `ci` config key is
// consulted, so a bare CI env var does not suppress the prompt: the form is
// attempted and, on a non-TTY, logs "telemetry consent prompt skipped" — which
// this test asserts must NOT happen.
func TestPromptTelemetryConsent_CIEnvVarSkipsPrompt(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	// Not parallel: mutates the CI env var.
	t.Setenv("CI", "true")

	props := consentProps(t, "", true)
	buf := consentBuffer(t, props)

	promptTelemetryConsent(t.Context(), props)

	assert.False(t, buf.Contains("telemetry consent prompt skipped"),
		"CI=true env must skip the consent prompt without ever attempting the form")
}

// TestPromptTelemetryConsent_NonInteractiveSkipsPrompt proves the consent
// prompt is skipped when stdin is not a terminal (utils.IsInteractive is false
// under `go test`). CI is neutralised so this isolates the interactivity gate.
// On origin/main there is no such gate, so the form is attempted and logs
// "telemetry consent prompt skipped" on the non-TTY error path.
func TestPromptTelemetryConsent_NonInteractiveSkipsPrompt(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	// Not parallel: neutralises any ambient CI so only interactivity gates, and
	// swaps os.Stdin for a non-terminal.
	t.Setenv("CI", "")
	nonInteractiveStdin(t)

	props := consentProps(t, "", true)
	buf := consentBuffer(t, props)

	promptTelemetryConsent(t.Context(), props)

	assert.False(t, buf.Contains("telemetry consent prompt skipped"),
		"non-interactive stdin must skip the consent prompt without attempting the form")
}

// TestHandleOutdatedVersion_NonInteractiveSkipsPrompt proves the update prompt
// is not attempted when stdin is not a terminal. On origin/main the form
// creator is always invoked; after the fix a non-interactive run skips the form
// entirely and, under the prompt policy, warns and continues.
func TestHandleOutdatedVersion_NonInteractiveSkipsPrompt(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	// Not parallel: neutralises any ambient CI and swaps os.Stdin.
	t.Setenv("CI", "")
	nonInteractiveStdin(t)

	formCalled := false
	form := func(_ *bool) *huh.Form { formCalled = true; return nil }

	props := newUpdateProps(t, "v1.0.0", forgetest.New(forgetest.WithRelease("v2.0.0")))
	result := &UpdateCheckResult{}
	state := newRootState()

	handleOutdatedVersion(context.Background(), props, "v2 available", result, state, p.UpdatePolicyPrompt, WithForm(form))

	assert.False(t, formCalled, "non-interactive stdin must not attempt the update prompt")
	assert.NoError(t, result.Error, "prompt policy continues (warn only) when non-interactive")
}

// TestShouldSkipUpdateCheck_CIEnvVar proves the pre-run update check is skipped
// when CI=true is in the environment (parity with the `ci` config key). On
// origin/main only the config key is consulted, so the bare env var does not
// skip the check.
func TestShouldSkipUpdateCheck_CIEnvVar(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	// Not parallel: mutates the CI env var.
	t.Setenv("CI", "true")

	props := newUpdateProps(t, "v1.0.0", forgetest.New(forgetest.WithRelease("v2.0.0")))
	state := newRootState()

	skip := shouldSkipUpdateCheck(props, props.Config.View(), mkUpdateCmd(t), state)

	assert.True(t, skip, "CI=true env must skip the pre-run update check")
}

// TestCheckForUpdates_CIEnvSuppressesBehindReminder proves the persistent
// out-of-date reminder (warnIfBehindCached) is suppressed under CI=true in the
// environment, for full parity with the --ci flag. On origin/main the reminder
// is gated only on the `ci` config key, so a bare env var does not suppress it
// and the "a newer …" warning is emitted.
func TestCheckForUpdates_CIEnvSuppressesBehindReminder(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	// Not parallel: mutates CI and HOME (marker path is HOME-derived).
	t.Setenv("CI", "true")
	t.Setenv("HOME", t.TempDir())

	props := newUpdateProps(t, "v1.0.0", forgetest.New(forgetest.WithRelease("v2.0.0")))
	buf := consentBuffer(t, props)

	// Pre-seed a newer cached latest version so warnIfBehindCached WOULD warn
	// absent the CI suppression.
	require.NoError(t, setup.SetCheckedVersion(props.FS, props.Tool.Name, "v2.0.0"))

	checkForUpdates(context.Background(), mkUpdateCmd(t), props, newRootState())

	assert.False(t, buf.Contains("a newer"),
		"CI=true env must suppress the persistent behind reminder")
}

// --- green: interactive paths and mcp exemption preserved ------------------

// TestHandleOutdatedVersion_InteractiveAttemptsPrompt proves the update prompt
// is still attempted on an interactive terminal: with the TTY gate forced true,
// the form creator is invoked exactly as before the fix.
func TestHandleOutdatedVersion_InteractiveAttemptsPrompt(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	formCalled := false
	form := func(runUpdate *bool) *huh.Form { formCalled = true; *runUpdate = false; return nil }

	props := newUpdateProps(t, "v1.0.0", forgetest.New(forgetest.WithRelease("v2.0.0")))
	result := &UpdateCheckResult{}
	state := newRootState()

	handleOutdatedVersion(context.Background(), props, "v2 available", result, state, p.UpdatePolicyPrompt,
		WithForm(form), WithInteractive(func() bool { return true }))

	assert.True(t, formCalled, "interactive stdin must still attempt the update prompt")
	assert.NoError(t, result.Error)
}

// TestPromptTelemetryConsent_InteractiveReachesForm proves the consent form is
// still reached on an interactive terminal. The TTY gate is forced true while
// stdin is a non-terminal returning EOF, so the form runs and fails immediately
// (covering the form-error tail) rather than hanging.
func TestPromptTelemetryConsent_InteractiveReachesForm(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	// Not parallel: neutralises CI and swaps os.Stdin for a non-terminal.
	t.Setenv("CI", "")
	nonInteractiveStdin(t)

	props := consentProps(t, "", true)
	buf := consentBuffer(t, props)

	promptTelemetryConsent(t.Context(), props, WithConsentInteractive(func() bool { return true }))

	assert.True(t, buf.Contains("telemetry consent prompt skipped"),
		"with the TTY gate satisfied the consent form must be reached")
}

// TestPromptTelemetryConsent_NonInteractiveDoesNotPersist proves a deferred
// (non-interactive) consent prompt persists nothing: absence of consent is not
// refusal, so telemetry.enabled stays unset and the opt-in reappears later.
func TestPromptTelemetryConsent_NonInteractiveDoesNotPersist(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Setenv("CI", "")
	nonInteractiveStdin(t)

	props := consentProps(t, "", true)
	require.False(t, props.Config.View().IsSet("telemetry.enabled"))

	promptTelemetryConsent(t.Context(), props)

	assert.False(t, props.Config.View().IsSet("telemetry.enabled"),
		"a deferred prompt must not auto-persist a telemetry choice")
}

// TestIsMCPFeatureSubtree proves the mcp prompt exemption matches on the McpCmd
// feature annotation (stamped by setup.Wrap), across the whole subtree, and
// never on a name match alone.
func TestIsMCPFeatureSubtree(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	mcp := &cobra.Command{Use: "mcp"}
	setup.Wrap(p.McpCmd, mcp)

	start := &cobra.Command{Use: "start"}
	mcp.AddCommand(start)

	// A command merely named "mcp" but never wrapped must NOT match.
	unwrapped := &cobra.Command{Use: "mcp"}

	other := &cobra.Command{Use: "status"}

	assert.True(t, isMCPFeatureSubtree(mcp), "the wrapped mcp command matches")
	assert.True(t, isMCPFeatureSubtree(start), "a descendant of mcp matches (parent walk)")
	assert.False(t, isMCPFeatureSubtree(unwrapped), "an unwrapped command named mcp must not match")
	assert.False(t, isMCPFeatureSubtree(other), "an unrelated command must not match")
}
