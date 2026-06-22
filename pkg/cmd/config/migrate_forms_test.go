package config

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withScriptedStdin runs fn with huh in accessible mode (TERM=dumb) and os.Stdin
// fed from script, restoring both afterwards. huh auto-switches to accessible
// (line-based) prompts when TERM=dumb, reading answers from os.Stdin — so this
// drives the real wizard forms headlessly with no production change.
//
// Tests using it must NOT call t.Parallel(): it mutates process-global
// os.Stdin/os.Stdout and TERM. t.Setenv enforces that constraint.
func withScriptedStdin(t *testing.T, script string, fn func()) {
	t.Helper()
	t.Setenv("TERM", "dumb")

	r, w, err := os.Pipe()
	require.NoError(t, err)

	origIn, origOut := os.Stdin, os.Stdout
	os.Stdin = r

	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	require.NoError(t, err)
	os.Stdout = devnull

	t.Cleanup(func() {
		os.Stdin, os.Stdout = origIn, origOut
		_ = devnull.Close()
	})

	go func() {
		_, _ = io.WriteString(w, script)
		_ = w.Close()
	}()

	fn()
}

// TestResolveEnvVarName_InteractivePrompt drives the real huh input form: a
// scripted line becomes the chosen env var name.
func TestResolveEnvVarName_InteractivePrompt(t *testing.T) {
	withScriptedStdin(t, "MY_CUSTOM_TOKEN\n", func() {
		name, err := resolveEnvVarName(MigrateOptions{}, literalCredential{
			Key: "github.auth.value",
		})
		require.NoError(t, err)
		assert.Equal(t, "MY_CUSTOM_TOKEN", name)
	})
}

// TestInstructAndVerifyEnvVar_ConfirmedAndSet — confirm "yes" with the env var
// exported passes verification.
func TestInstructAndVerifyEnvVar_ConfirmedAndSet(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_present")

	withScriptedStdin(t, "y\n", func() {
		err := instructAndVerifyEnvVar("GITHUB_TOKEN", literalCredential{
			Key: "github.auth.value",
		}, false)
		require.NoError(t, err)
	})
}

// TestInstructAndVerifyEnvVar_ConfirmedButUnset — confirm "yes" while the env
// var is absent fails verification with the documented hint.
func TestInstructAndVerifyEnvVar_ConfirmedButUnset(t *testing.T) {
	// Ensure the variable is not set in this process.
	require.NoError(t, os.Unsetenv("UNSET_TOKEN_FOR_TEST"))

	withScriptedStdin(t, "y\n", func() {
		err := instructAndVerifyEnvVar("UNSET_TOKEN_FOR_TEST", literalCredential{
			Key: "github.auth.value",
		}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "is not set in the current environment")
	})
}

// TestInstructAndVerifyEnvVar_Declined — confirm "no" aborts the migration.
func TestInstructAndVerifyEnvVar_Declined(t *testing.T) {
	withScriptedStdin(t, "n\n", func() {
		err := instructAndVerifyEnvVar("GITHUB_TOKEN", literalCredential{
			Key: "github.auth.value",
		}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "aborted by user")
	})
}

// TestInstructAndVerifyEnvVar_DualCredentialExportLine — a partner key renders
// the second export instruction; confirm "yes" with both vars set passes.
func TestInstructAndVerifyEnvVar_DualCredentialExportLine(t *testing.T) {
	t.Setenv("BITBUCKET_USERNAME", "alice")

	withScriptedStdin(t, "y\n", func() {
		err := instructAndVerifyEnvVar("BITBUCKET_USERNAME", literalCredential{
			Key:        "bitbucket.username",
			PartnerKey: "bitbucket.app_password",
		}, false)
		require.NoError(t, err)
	})
}
