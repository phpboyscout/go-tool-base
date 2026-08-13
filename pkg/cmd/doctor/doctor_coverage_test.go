package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	gtbconfig "gitlab.com/phpboyscout/go/config"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	ver "gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and returns
// everything written to the real stdout during the call. NewCmdDoctor's
// RunE writes to os.Stdout directly, so this is the only way to assert on
// its rendered report.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	orig := os.Stdout

	r, w, err := os.Pipe()
	require.NoError(t, err)

	os.Stdout = w

	defer func() { os.Stdout = orig }()

	done := make(chan string, 1)

	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		done <- buf.String()
	}()

	fn()

	require.NoError(t, w.Close())

	return <-done
}

// emptyCredsConfig returns a store with no credential or API-key config keys
// set, so the built-in checks run cleanly under RunChecks.
func emptyCredsConfig(t *testing.T) *gtbconfig.Store {
	t.Helper()

	return testutil.StoreFromYAML(t, "{}\n")
}

// TestNewCmdDoctor_TextOutput exercises the cobra RunE closure end-to-end
// for the default (text) renderer.
func TestNewCmdDoctor_TextOutput(t *testing.T) {
	// Not parallel: redirects the process-global os.Stdout.
	props := &p.Props{
		Tool:    p.Tool{Name: "doctor-text-tool"},
		Version: ver.NewInfo("v9.9.9", "", ""),
		Config:  emptyCredsConfig(t),
		Logger:  logger.NewNoop(),
		FS:      afero.NewMemMapFs(),
	}

	cmd := NewCmdDoctor(props)
	require.NotNil(t, cmd)
	assert.Equal(t, p.DoctorCmd, cmd.Feature)
	cmd.SetContext(context.Background())

	out := captureStdout(t, func() {
		require.NoError(t, cmd.RunE(cmd.Command, nil))
	})

	assert.Contains(t, out, "doctor-text-tool v9.9.9")
	assert.Contains(t, out, "Go version")
	assert.Contains(t, out, "Configuration")
}

// TestNewCmdDoctor_JSONOutput exercises the RunE closure with the --output
// json flag set, asserting the machine-readable branch renders a parseable
// DoctorReport.
func TestNewCmdDoctor_JSONOutput(t *testing.T) {
	// Not parallel: redirects the process-global os.Stdout.
	props := &p.Props{
		Tool:    p.Tool{Name: "doctor-json-tool"},
		Version: ver.NewInfo("v1.2.3", "", ""),
		Config:  emptyCredsConfig(t),
		Logger:  logger.NewNoop(),
		FS:      afero.NewMemMapFs(),
	}

	cmd := NewCmdDoctor(props)
	require.NotNil(t, cmd)
	cmd.SetContext(context.Background())

	// The doctor command reads the "output" flag; register it locally so the
	// RunE closure resolves the JSON renderer branch.
	cmd.Flags().String("output", "json", "output format")

	out := captureStdout(t, func() {
		require.NoError(t, cmd.RunE(cmd.Command, nil))
	})

	// The JSON renderer wraps the report in an output.Response envelope, so the
	// DoctorReport lives under the "data" key.
	var envelope struct {
		Command string       `json:"command"`
		Data    DoctorReport `json:"data"`
	}

	require.NoError(t, json.Unmarshal([]byte(out), &envelope),
		"doctor JSON output should unmarshal: %q", out)
	assert.Equal(t, "doctor", envelope.Command)
	assert.Equal(t, "doctor-json-tool", envelope.Data.Tool)
	assert.Equal(t, "v1.2.3", envelope.Data.Version)
	assert.NotEmpty(t, envelope.Data.Checks)
}

// TestRunChecks_NoVersion covers the branch where props.Version is nil so the
// report's Version stays empty.
func TestRunChecks_NoVersion(t *testing.T) {
	t.Parallel()

	props := &p.Props{
		Tool:   p.Tool{Name: "no-version-tool"},
		Config: emptyCredsConfig(t),
		Logger: logger.NewNoop(),
		FS:     afero.NewMemMapFs(),
	}

	report := RunChecks(context.Background(), props)
	assert.Equal(t, "no-version-tool", report.Tool)
	assert.Empty(t, report.Version)
}

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
	credentialposture.Register(credentialposture.Descriptor{
		Owner:      "forge:gitlab",
		Label:      "GitLab credential",
		EnvKey:     "gitlab.auth.env",
		LiteralKey: "gitlab.auth.value",
	})
	credentialposture.Register(credentialposture.Descriptor{
		Owner:      "chat:anthropic",
		Label:      "Anthropic API key",
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

// TestCheckPermissions_NoConfigDir covers the branch where the config dir
// cannot be determined (os.UserHomeDir fails because HOME is empty).
func TestCheckPermissions_NoConfigDir(t *testing.T) {
	// Not parallel: mutates HOME via t.Setenv to force os.UserHomeDir to fail.
	t.Setenv("HOME", "")

	// On some platforms UserHomeDir consults other variables; only assert the
	// targeted branch when it actually returned empty.
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		t.Skip("UserHomeDir still resolved a home directory; cannot exercise empty-dir branch")
	}

	props := &p.Props{
		Tool: p.Tool{Name: "no-home-tool"},
		FS:   afero.NewMemMapFs(),
	}

	result := checkPermissions(context.Background(), props)
	assert.Equal(t, "Permissions", result.Name)
	assert.Equal(t, CheckWarn, result.Status)
	assert.Contains(t, result.Message, "unable to determine config directory")
}

// TestCheckPermissions_StatError covers the generic (non-IsNotExist) stat
// failure branch, which reports a FAIL.
func TestCheckPermissions_StatError(t *testing.T) {
	t.Parallel()

	fs := &mockStatFs{
		Fs: afero.NewMemMapFs(),
		statFunc: func(_ string) (os.FileInfo, error) {
			return nil, os.ErrPermission
		},
	}

	props := &p.Props{
		Tool: p.Tool{Name: "stat-error-tool"},
		FS:   fs,
	}

	result := checkPermissions(context.Background(), props)
	assert.Equal(t, "Permissions", result.Name)
	assert.Equal(t, CheckFail, result.Status)
	assert.Contains(t, result.Message, "unable to stat config directory")
}
