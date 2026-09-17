package doctor

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
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
		Feature:     "posturetest",
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

// TestCheckNoLiteralCredentials_NoConfig covers the skip branch.
func TestCheckNoLiteralCredentials_NoConfig(t *testing.T) {
	t.Parallel()

	result := checkNoLiteralCredentials(context.Background(), &p.Props{})
	assert.Equal(t, "Credential storage", result.Name)
	assert.Equal(t, CheckSkip, result.Status)
}

// TestCheckNoLiteralCredentials_Clean covers the pass branch.
func TestCheckNoLiteralCredentials_Clean(t *testing.T) {
	t.Parallel()

	result := checkNoLiteralCredentials(context.Background(), &p.Props{Config: testutil.StoreFromYAML(t, "{}\n")})
	assert.Equal(t, CheckPass, result.Status)
	assert.Equal(t, "no literal credentials in config", result.Message)
}

// TestCheckNoLiteralCredentials_Leaked covers the warn branch where one or
// more literal credentials are present. It also asserts the details name the
// leaked KEYS only and never echo the secret VALUE.
func TestCheckNoLiteralCredentials_Leaked(t *testing.T) {
	const secret = "super-secret-value"

	// The inventory is what bundles declared, so this test declares the forge
	// credential it asserts on rather than relying on pkg/setup/forge being
	// linked into doctor's own test binary. That coupling is the point: a
	// credential is reported because something declared it, which is also why a
	// tool with no GitLab feature is no longer warned about gitlab.auth.value.
	// Register replaces by owner and key, so these carry the features the
	// real descriptors carry or they would untag them for the rest of the
	// package's tests.
	credentialposture.Register(credentialposture.Descriptor{
		Owner:      "forge:gitlab",
		Label:      "GitLab credential",
		Feature:    string(forge.GitlabFeature),
		EnvKey:     "gitlab.auth.env",
		LiteralKey: "gitlab.auth.value",
	})
	credentialposture.Register(credentialposture.Descriptor{
		Owner:      "chat:anthropic",
		Label:      "Anthropic API key",
		Feature:    string(p.AiCmd),
		EnvKey:     chat.ConfigKeyClaudeEnv,
		LiteralKey: chat.ConfigKeyClaudeKey,
	})

	// Whitespace-padded values still count as leaked (the check trims).
	store := testutil.StoreFromYAML(t,
		"anthropic:\n  api:\n    key: \"  "+secret+"  \"\ngitlab:\n  auth:\n    value: \"  "+secret+"  \"\n")

	result := checkNoLiteralCredentials(context.Background(), &p.Props{Config: store})
	assert.Equal(t, CheckWarn, result.Status)
	assert.Contains(t, result.Message, "2 literal credential(s)")
	assert.Contains(t, result.Details, chat.ConfigKeyClaudeKey)
	assert.Contains(t, result.Details, "gitlab.auth.value")
	assert.NotContains(t, result.Details, secret, "details must never echo the secret value")
}
