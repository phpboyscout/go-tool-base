package doctor

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
)

// Spec 0189 R1/R2 at the surface an operator actually reads.

func TestCheckCredentialResolution_SkipsWithoutConfig(t *testing.T) {
	t.Parallel()

	got := checkCredentialResolution(context.Background(), nil)

	assert.Equal(t, CheckSkip, got.Status)
}

func TestResolutionResult_ShadowedCopyWarnsAndSaysWhatToDo(t *testing.T) {
	t.Parallel()

	got := resolutionResult(
		[]string{"GitHub: resolves from auth.env; shadowed copies still present in github.auth.value"},
		1, 1, 0)

	assert.Equal(t, CheckWarn, got.Status)
	assert.Contains(t, got.Message, "shadowed")
	assert.Contains(t, got.Details, "config unset")
}

func TestResolutionResult_BrokenOutranksShadowed(t *testing.T) {
	t.Parallel()

	// A credential that does not resolve at all is the more urgent finding, and
	// must not be hidden behind a tidier one.
	got := resolutionResult(
		[]string{"GitHub: configured but does not resolve"}, 1, 1, 1)

	assert.Equal(t, CheckWarn, got.Status)
	assert.Contains(t, got.Message, "not resolving")
}

func TestResolutionResult_CleanResolutionPasses(t *testing.T) {
	t.Parallel()

	got := resolutionResult(
		[]string{"GitHub: resolves from auth.env"}, 1, 0, 0)

	assert.Equal(t, CheckPass, got.Status)
	assert.Contains(t, got.Message, "1 credential(s) resolve")
}

func TestResolutionResult_NothingConfiguredSkips(t *testing.T) {
	t.Parallel()

	got := resolutionResult(nil, 0, 0, 0)

	assert.Equal(t, CheckSkip, got.Status)
}

// The report is pasted into support bundles, so this is the assertion that
// keeps the never-log-values rule enforced rather than trusted.
func TestCheckCredentialResolution_NeverRendersAValue(t *testing.T) {
	t.Setenv("POSTURE_TEST_TOKEN", "super-secret-value")

	credentialposture.Register(credentialposture.Descriptor{
		Owner:       "test:posture",
		Label:       "Posture test credential",
		EnvKey:      "posturetest.api.env",
		KeychainKey: "posturetest.api.keychain",
		LiteralKey:  "posturetest.api.key",
	})

	results := credentialposture.ReportAll(context.Background(),
		fakeConfigReader{"posturetest.api.env": "POSTURE_TEST_TOKEN"})

	var rendered strings.Builder
	for _, r := range results {
		rendered.WriteString(r.Posture.String())
	}

	require.NotEmpty(t, rendered.String())
	assert.NotContains(t, rendered.String(), "super-secret-value")
}

type fakeConfigReader map[string]string

func (f fakeConfigReader) GetString(key string) string { return f[key] }
