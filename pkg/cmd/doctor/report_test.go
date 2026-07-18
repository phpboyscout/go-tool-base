package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gtbconfig "gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// reportRawSecrets are the raw secrets seeded into the fixture; none may appear
// in any rendering of the bundle.
var reportRawSecrets = []string{
	"sk-ant-SECRET123",
	"ghp_SECRETTOKEN456",
	"app-pass-SECRET789",
	"glpat-ABCDEFGHIJ1234567890", // realistic GitLab PAT body under a non-credential key
	"superuser:hunter2",          // URL userinfo
}

func reportSecretProps(t *testing.T) *props.Props {
	t.Helper()

	v := viper.New()
	v.Set("anthropic.api.key", "sk-ant-SECRET123")
	v.Set("github.auth.value", "ghp_SECRETTOKEN456")
	v.Set("bitbucket.app_password", "app-pass-SECRET789")
	v.Set("log.level", "debug")                      // non-secret — must survive
	v.Set("free.form", "glpat-ABCDEFGHIJ1234567890") // non-credential key, secret-shaped value
	v.Set("service.url", "https://superuser:hunter2@example.com/api")

	return &props.Props{
		Logger:  logger.NewNoop(),
		FS:      afero.NewMemMapFs(),
		Config:  gtbconfig.NewContainerFromViper(nil, v),
		Version: version.Info{Version: "1.2.3", Commit: "abc", Date: "2026-06-22"},
		Tool:    props.Tool{Name: "demo", Summary: "demo tool"},
	}
}

func TestCollectBundle_RedactsAllSecrets(t *testing.T) {
	t.Parallel()

	bundle := CollectBundle(context.Background(), reportSecretProps(t))

	jsonBytes, err := json.Marshal(bundle)
	require.NoError(t, err)

	var textBuf bytes.Buffer
	PrintBundle(&textBuf, bundle)

	for _, surface := range map[string]string{"json": string(jsonBytes), "text": textBuf.String()} {
		for _, secret := range reportRawSecrets {
			assert.NotContains(t, surface, secret, "raw secret leaked into a rendering")
		}
	}

	// Credential-shaped keys are dropped to the sentinel, not value-redacted.
	assert.Equal(t, redactedSentinel, bundle.Config["anthropic"].(map[string]any)["api"].(map[string]any)["key"])
	assert.Equal(t, redactedSentinel, bundle.Config["github"].(map[string]any)["auth"].(map[string]any)["value"])
	assert.Equal(t, redactedSentinel, bundle.Config["bitbucket"].(map[string]any)["app_password"])

	// Non-secret values survive.
	assert.Equal(t, "debug", bundle.Config["log"].(map[string]any)["level"])
}

func TestCollectBundle_PopulatesSections(t *testing.T) {
	t.Parallel()

	bundle := CollectBundle(context.Background(), reportSecretProps(t))

	assert.Equal(t, "demo", bundle.Tool.Name)
	assert.Equal(t, "1.2.3", bundle.Tool.Version)
	assert.Equal(t, "abc", bundle.Tool.Commit)
	assert.NotEmpty(t, bundle.Runtime.Go)
	assert.NotEmpty(t, bundle.Runtime.OS)
	assert.NotEmpty(t, bundle.Runtime.Arch)
	assert.NotNil(t, bundle.Doctor, "doctor section is reused, not reimplemented")
	assert.NotEmpty(t, bundle.Features)
}

func TestCollectBundle_NilConfigAndVersionIsSafe(t *testing.T) {
	t.Parallel()

	// No Config, no Version — must not panic and must omit those sections.
	bundle := CollectBundle(context.Background(), &props.Props{
		Logger: logger.NewNoop(),
		FS:     afero.NewMemMapFs(),
		Tool:   props.Tool{Name: "bare"},
	})

	assert.Equal(t, "bare", bundle.Tool.Name)
	assert.Empty(t, bundle.Tool.Version)
	assert.Empty(t, bundle.Config)
	assert.NotEmpty(t, bundle.Runtime.Go)
	assert.NotNil(t, bundle.Doctor)

	var buf bytes.Buffer
	PrintBundle(&buf, bundle)
	assert.Contains(t, buf.String(), "(none)")
	assert.Contains(t, buf.String(), "(not set)")
}

func TestNewCmdReport_JSONOutput(t *testing.T) {
	t.Parallel()

	cmd := NewCmdReport(reportSecretProps(t))
	cmd.Flags().String("output", "json", "")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{})
	require.NoError(t, cmd.Execute())

	out := buf.String()
	assert.Contains(t, out, `"command": "doctor report"`)

	for _, secret := range reportRawSecrets {
		assert.NotContains(t, out, secret)
	}

	var envelope struct {
		Data SupportBundle `json:"data"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &envelope))
	assert.Equal(t, "demo", envelope.Data.Tool.Name)
}

func TestNewCmdReport_TextOutput(t *testing.T) {
	t.Parallel()

	cmd := NewCmdReport(reportSecretProps(t))
	cmd.Flags().String("output", "text", "")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{})
	require.NoError(t, cmd.Execute())

	out := buf.String()
	assert.Contains(t, out, "Runtime:")
	assert.Contains(t, out, "Features:")
	assert.Contains(t, out, "Config (redacted):")
	assert.Contains(t, out, "Checks:")

	for _, secret := range reportRawSecrets {
		assert.NotContains(t, out, secret)
	}
}

func TestNewCmdDoctor_HasReportSubcommand(t *testing.T) {
	t.Parallel()

	cmd := NewCmdDoctor(&props.Props{Logger: logger.NewNoop()})

	var found bool
	for _, c := range cmd.Commands() {
		if c.Name() == "report" {
			found = true
		}
	}

	assert.True(t, found, "doctor must expose the report subcommand")
}
