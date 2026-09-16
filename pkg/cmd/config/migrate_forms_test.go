package config

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/formtest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// The migrate wizard's two prompts run on Props.IO (spec 0198, closing the
// 0106 stdin swap): a test answers them at accessible prompts and reads what
// they printed from the IO's output. Nothing global is touched, so these run
// in parallel.

func answering(lines ...string) (*props.Props, *bytes.Buffer) {
	out := &bytes.Buffer{}

	return &props.Props{IO: props.StdIO{Stdin: formtest.Answers(lines...), Stdout: out, Stderr: out, AccessibleMode: true}}, out
}

// TestResolveEnvVarName_InteractivePrompt drives the real huh input form: a
// scripted line becomes the chosen env var name.
func TestResolveEnvVarName_InteractivePrompt(t *testing.T) {
	t.Parallel()

	p, _ := answering("MY_CUSTOM_TOKEN")

	name, err := resolveEnvVarName(t.Context(), p, MigrateOptions{}, literalCredential{
		Key: "github.auth.value",
	})
	require.NoError(t, err)
	assert.Equal(t, "MY_CUSTOM_TOKEN", name)
}

// TestInstructAndVerifyEnvVar_ConfirmedAndSet: confirm "yes" with the env var
// exported passes verification.
func TestInstructAndVerifyEnvVar_ConfirmedAndSet(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_present")

	p, out := answering("y")

	err := instructAndVerifyEnvVar(t.Context(), p, "GITHUB_TOKEN", literalCredential{
		Key: "github.auth.value",
	}, false)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "export GITHUB_TOKEN=<paste the current github.auth.value value>")
}

// TestInstructAndVerifyEnvVar_ConfirmedButUnset: confirm "yes" while the env
// var is absent fails verification with the documented hint.
func TestInstructAndVerifyEnvVar_ConfirmedButUnset(t *testing.T) {
	// Ensure the variable is not set in this process.
	require.NoError(t, os.Unsetenv("UNSET_TOKEN_FOR_TEST"))

	p, _ := answering("y")

	err := instructAndVerifyEnvVar(t.Context(), p, "UNSET_TOKEN_FOR_TEST", literalCredential{
		Key: "github.auth.value",
	}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not set in the current environment")
}

// TestInstructAndVerifyEnvVar_Declined: confirm "no" aborts the migration.
func TestInstructAndVerifyEnvVar_Declined(t *testing.T) {
	t.Parallel()

	p, _ := answering("n")

	err := instructAndVerifyEnvVar(t.Context(), p, "GITHUB_TOKEN", literalCredential{
		Key: "github.auth.value",
	}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "aborted by user")
}

// TestInstructAndVerifyEnvVar_DualCredentialExportLine: a partner key renders
// the second export instruction; confirm "yes" with both vars set passes.
func TestInstructAndVerifyEnvVar_DualCredentialExportLine(t *testing.T) {
	t.Setenv("BITBUCKET_USERNAME", "alice")

	p, out := answering("y")

	err := instructAndVerifyEnvVar(t.Context(), p, "BITBUCKET_USERNAME", literalCredential{
		Key:        "bitbucket.username",
		PartnerKey: "bitbucket.app_password",
	}, false)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "export BITBUCKET_APP_PASSWORD=<paste the current bitbucket.app_password value>")
}

// TestInstructAndVerifyEnvVar_SkipVerifyAsksNothing: --skip-verify prints the
// instructions and asks no question.
func TestInstructAndVerifyEnvVar_SkipVerifyAsksNothing(t *testing.T) {
	t.Parallel()

	p, out := answering()

	require.NoError(t, instructAndVerifyEnvVar(t.Context(), p, "GITHUB_TOKEN", literalCredential{Key: "github.auth.value"}, true))
	assert.NotContains(t, out.String(), "Have you set the env var?")
}
