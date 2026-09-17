package config

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/credentials"
	credtest "gitlab.com/phpboyscout/go/credentials/test"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestMigrate_NilConfig — Migrate with no loaded config returns the
// "no configuration loaded" error before doing any work.
func TestMigrate_NilConfig(t *testing.T) {
	t.Parallel()

	_, err := Migrate(t.Context(), &props.Props{}, MigrateOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no configuration loaded")
}

// TestMigrate_LiteralTargetRejected — the literal target is rejected
// with a tailored message that explains migrate moves OFF literal.
func TestMigrate_LiteralTargetRejected(t *testing.T) {
	t.Parallel()

	p := newMigrateFixture(t, anthropicSeed("sk-ant"))

	_, err := Migrate(t.Context(), p, MigrateOptions{
		AssumeYes: true,
		Target:    credentials.ModeLiteral,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not onto it")
}

// TestMigrate_InteractiveSkipVerifyWritesConfig — the interactive
// (AssumeYes=false) env-var path is exercised with SkipVerify=true so
// instructAndVerifyEnvVar prints instructions and returns without
// prompting. An EnvVarOverride avoids the huh name prompt.
func TestMigrate_InteractiveSkipVerifyWritesConfig(t *testing.T) {
	t.Parallel()

	p := newMigrateFixture(t, anthropicSeed("sk-ant-interactive"))

	opts := MigrateOptions{
		AssumeYes:  false,
		SkipVerify: true,
		EnvVarOverrides: map[string]string{
			"anthropic.api.key": "MY_ANTHROPIC_INTERACTIVE",
		},
	}

	result, err := Migrate(t.Context(), p, opts)
	require.NoError(t, err)
	require.Len(t, result.Actions, 1)
	assert.True(t, result.WroteConfig)
	assert.Equal(t, "MY_ANTHROPIC_INTERACTIVE", p.Config.View().GetString("anthropic.api.env"))
	assert.Empty(t, p.Config.View().GetString("anthropic.api.key"))
}

// TestMigrate_InteractiveBitbucketSkipVerify — the dual-credential
// branch of instructAndVerifyEnvVar (partner export line) plus the
// partner staging in migrateToEnvVar are exercised interactively.
func TestMigrate_InteractiveBitbucketSkipVerify(t *testing.T) {
	t.Parallel()

	// Sibling key keeps the bitbucket mapping non-empty mid-edit — see
	// TestMigrate_BitbucketPairOnlyDocumentEnvVar for the pair-only gap.
	seed := bitbucketPairSeed("alice", "s3cret")
	seed["bitbucket"].(map[string]any)["workspace"] = "acme"

	p := newMigrateFixture(t, seed)

	opts := MigrateOptions{
		AssumeYes:  false,
		SkipVerify: true,
		EnvVarOverrides: map[string]string{
			"bitbucket.username": "MY_BB_USER",
		},
	}

	result, err := Migrate(t.Context(), p, opts)
	require.NoError(t, err)
	require.Len(t, result.Actions, 1)
	assert.Equal(t, "MY_BB_USER", p.Config.View().GetString("bitbucket.username.env"))
	// Partner falls back to the default name (no override supplied).
	assert.Equal(t, "BITBUCKET_APP_PASSWORD", p.Config.View().GetString("bitbucket.app_password.env"))
}

// TestResolveEnvVarName_DryRunInteractive — the dry-run interactive
// branch returns the default name without prompting.
func TestResolveEnvVarName_DryRunInteractive(t *testing.T) {
	t.Parallel()

	name, err := resolveEnvVarName(t.Context(), &props.Props{}, MigrateOptions{DryRun: true}, literalCredential{
		Key: "github.auth.value",
	})
	require.NoError(t, err)
	assert.Equal(t, "GITHUB_TOKEN", name)
}

// TestResolveEnvVarName_OverrideWins — an override pins the name even
// in interactive non-dry-run mode (no prompt reached).
func TestResolveEnvVarName_OverrideWins(t *testing.T) {
	t.Parallel()

	name, err := resolveEnvVarName(t.Context(), &props.Props{}, MigrateOptions{
		EnvVarOverrides: map[string]string{"github.auth.value": "CUSTOM"},
	}, literalCredential{Key: "github.auth.value"})
	require.NoError(t, err)
	assert.Equal(t, "CUSTOM", name)
}

// TestMigrateToKeychain_EmptyServiceRejected — keychain migration with
// an empty tool name and no override service is refused.
func TestMigrateToKeychain_EmptyServiceRejected(t *testing.T) {
	t.Parallel()

	credtest.Install(t)

	p := newMigrateFixture(t, map[string]any{
		"github": map[string]any{"auth": map[string]any{"value": "ghp_x"}},
	})
	p.Tool.Name = ""

	_, err := Migrate(t.Context(), p, MigrateOptions{
		AssumeYes: true,
		Target:    credentials.ModeKeychain,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool name is empty")
}

// TestMigrate_NoWritableLayer — committing the staged plan errors when the
// store has no writable file layer to route the changes to (a reader-only
// store: in-memory sources are never writable).
func TestMigrate_NoWritableLayer(t *testing.T) {
	t.Parallel()

	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Config: testutil.StoreFromYAML(t, "anthropic:\n  api:\n    key: sk-literal\n"),
		Logger: logger.NewNoop(),
	}

	_, err := Migrate(t.Context(), p, MigrateOptions{AssumeYes: true})
	require.Error(t, err)
	assert.True(t, errors.Is(err, config.ErrNoWritableLayer), "got: %v", err)
	assert.Contains(t, err.Error(), "rewriting config")
}
