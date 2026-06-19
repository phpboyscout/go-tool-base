package config

import (
	"bytes"
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/credentials"
	"gitlab.com/phpboyscout/go-tool-base/pkg/credentials/credtest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestMigrate_NilConfig — Migrate with no loaded config returns the
// "no configuration loaded" error before doing any work.
func TestMigrate_NilConfig(t *testing.T) {
	t.Parallel()

	_, err := Migrate(context.Background(), &props.Props{}, MigrateOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no configuration loaded")
}

// TestMigrate_LiteralTargetRejected — the literal target is rejected
// with a tailored message that explains migrate moves OFF literal.
func TestMigrate_LiteralTargetRejected(t *testing.T) {
	t.Parallel()

	p := newMigrateFixture(t, anthropicSeed("sk-ant"))

	_, err := Migrate(context.Background(), p, MigrateOptions{
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

	result, err := Migrate(context.Background(), p, opts)
	require.NoError(t, err)
	require.Len(t, result.Actions, 1)
	assert.True(t, result.WroteConfig)
	assert.Equal(t, "MY_ANTHROPIC_INTERACTIVE", p.Config.GetString("anthropic.api.env"))
	assert.Empty(t, p.Config.GetString("anthropic.api.key"))
}

// TestMigrate_InteractiveBitbucketSkipVerify — the dual-credential
// branch of instructAndVerifyEnvVar (partner export line) plus the
// partner staging in migrateToEnvVar are exercised interactively.
func TestMigrate_InteractiveBitbucketSkipVerify(t *testing.T) {
	t.Parallel()

	p := newMigrateFixture(t, bitbucketPairSeed("alice", "s3cret"))

	opts := MigrateOptions{
		AssumeYes:  false,
		SkipVerify: true,
		EnvVarOverrides: map[string]string{
			"bitbucket.username": "MY_BB_USER",
		},
	}

	result, err := Migrate(context.Background(), p, opts)
	require.NoError(t, err)
	require.Len(t, result.Actions, 1)
	assert.Equal(t, "MY_BB_USER", p.Config.GetString("bitbucket.username.env"))
	// Partner falls back to the default name (no override supplied).
	assert.Equal(t, "BITBUCKET_APP_PASSWORD", p.Config.GetString("bitbucket.app_password.env"))
}

// TestResolveEnvVarName_DryRunInteractive — the dry-run interactive
// branch returns the default name without prompting.
func TestResolveEnvVarName_DryRunInteractive(t *testing.T) {
	t.Parallel()

	name, err := resolveEnvVarName(MigrateOptions{DryRun: true}, literalCredential{
		Key: "github.auth.value",
	})
	require.NoError(t, err)
	assert.Equal(t, "GITHUB_TOKEN", name)
}

// TestResolveEnvVarName_OverrideWins — an override pins the name even
// in interactive non-dry-run mode (no prompt reached).
func TestResolveEnvVarName_OverrideWins(t *testing.T) {
	t.Parallel()

	name, err := resolveEnvVarName(MigrateOptions{
		EnvVarOverrides: map[string]string{"github.auth.value": "CUSTOM"},
	}, literalCredential{Key: "github.auth.value"})
	require.NoError(t, err)
	assert.Equal(t, "CUSTOM", name)
}

// TestAlreadyMigrated covers each target branch including the keychain
// reference and the literal/unknown no-op fall-throughs.
func TestAlreadyMigrated(t *testing.T) {
	t.Parallel()

	c := literalCredential{
		EnvTargetKey:      "anthropic.api.env",
		KeychainTargetKey: "anthropic.api.keychain",
	}

	t.Run("env target set", func(t *testing.T) {
		t.Parallel()

		v := viper.New()
		v.Set("anthropic.api.env", "X")
		cfg := config.NewContainerFromViper(nil, v)
		assert.True(t, alreadyMigrated(cfg, c, credentials.ModeEnvVar))
	})

	t.Run("keychain target set", func(t *testing.T) {
		t.Parallel()

		v := viper.New()
		v.Set("anthropic.api.keychain", "svc/acct")
		cfg := config.NewContainerFromViper(nil, v)
		assert.True(t, alreadyMigrated(cfg, c, credentials.ModeKeychain))
	})

	t.Run("env target empty", func(t *testing.T) {
		t.Parallel()

		v := viper.New()
		cfg := config.NewContainerFromViper(nil, v)
		assert.False(t, alreadyMigrated(cfg, c, credentials.ModeEnvVar))
	})

	t.Run("literal target is never already migrated", func(t *testing.T) {
		t.Parallel()

		v := viper.New()
		cfg := config.NewContainerFromViper(nil, v)
		assert.False(t, alreadyMigrated(cfg, c, credentials.ModeLiteral))
	})

	t.Run("unknown mode falls through to false", func(t *testing.T) {
		t.Parallel()

		v := viper.New()
		cfg := config.NewContainerFromViper(nil, v)
		assert.False(t, alreadyMigrated(cfg, c, credentials.Mode("bogus")))
	})
}

// TestDefaultVCSEnvVarName_RemainingKeys covers the gitea / codeberg /
// direct branches plus the empty fallback for an unknown key.
func TestDefaultVCSEnvVarName_RemainingKeys(t *testing.T) {
	t.Parallel()

	for key, want := range map[string]string{
		"gitea.auth.value":    "GITEA_TOKEN",
		"codeberg.auth.value": "CODEBERG_TOKEN",
		"direct.auth.value":   "DIRECT_TOKEN",
		"unknown.key":         "",
	} {
		assert.Equal(t, want, defaultVCSEnvVarName(key), key)
	}
}

// TestSecretForKeychain covers single-value (raw) and dual-credential
// (JSON blob) shapes, plus jsonBlobFieldFor's default branch.
func TestSecretForKeychain(t *testing.T) {
	t.Parallel()

	t.Run("single value returns raw secret", func(t *testing.T) {
		t.Parallel()

		v := viper.New()
		cfg := config.NewContainerFromViper(nil, v)

		secret, err := secretForKeychain(cfg, literalCredential{Value: "ghp_raw"})
		require.NoError(t, err)
		assert.Equal(t, "ghp_raw", secret)
	})

	t.Run("dual credential returns json blob", func(t *testing.T) {
		t.Parallel()

		v := viper.New()
		v.Set("bitbucket.app_password", "pw")
		cfg := config.NewContainerFromViper(nil, v)

		secret, err := secretForKeychain(cfg, literalCredential{
			Key:        "bitbucket.username",
			Value:      "alice",
			PartnerKey: "bitbucket.app_password",
		})
		require.NoError(t, err)
		assert.Contains(t, secret, `"username":"alice"`)
		assert.Contains(t, secret, `"app_password":"pw"`)
	})
}

// TestJsonBlobFieldFor covers the default (passthrough) branch for an
// unmapped key.
func TestJsonBlobFieldFor(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "username", jsonBlobFieldFor("bitbucket.username"))
	assert.Equal(t, "app_password", jsonBlobFieldFor("bitbucket.app_password"))
	assert.Equal(t, "other.key", jsonBlobFieldFor("other.key"))
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

	_, err := Migrate(context.Background(), p, MigrateOptions{
		AssumeYes: true,
		Target:    credentials.ModeKeychain,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool name is empty")
}

// TestApplyPlan_NoConfigFilePath — applyPlan errors when the bound
// viper has no config file path to rewrite.
func TestApplyPlan_NoConfigFilePath(t *testing.T) {
	t.Parallel()

	v := viper.New() // no SetConfigFile
	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Config: config.NewContainerFromViper(nil, v),
		Logger: logger.NewNoop(),
	}

	plan := &rewritePlan{sets: map[string]any{"a.b": "c"}, deletes: map[string]struct{}{}}

	err := applyPlan(p, plan)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no config file path")
}

// TestWriteConfigAtomic_NilFSDefaultsToOsFs — a nil fs falls back to
// the OS filesystem; write to a temp dir to keep it real but cheap.
func TestWriteConfigAtomic_NilFSDefaultsToOsFs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := dir + "/sub/config.yaml"

	require.NoError(t, writeConfigAtomic(nil, path, []byte("k: v\n")))

	data, err := afero.ReadFile(afero.NewOsFs(), path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "k: v")
}

// TestDeleteNestedKey_NonMapPath — deleting through a non-map node is
// a safe no-op.
func TestDeleteNestedKey_NonMapPath(t *testing.T) {
	t.Parallel()

	m := map[string]any{"a": "scalar"}
	deleteNestedKey(m, "a.b.c")
	assert.Equal(t, "scalar", m["a"], "non-map traversal must not mutate")
}

// TestPrintAction_DualKeys covers the partner-key rendering branch.
func TestPrintAction_DualKeys(t *testing.T) {
	t.Parallel()

	var w bytes.Buffer
	printAction(&w, MigrationAction{
		SourceKey:  "bitbucket.username",
		PartnerKey: "bitbucket.app_password",
		DestKey:    "bitbucket.username.env",
		DestValue:  "BITBUCKET_USERNAME",
		Target:     credentials.ModeEnvVar,
	})
	out := w.String()
	assert.Contains(t, out, "bitbucket.username + bitbucket.app_password")
	assert.Contains(t, out, "BITBUCKET_USERNAME")
}

// --- NewCmdMigrate command-level coverage ---

// TestNewCmdMigrate_DryRunFlag drives the cobra command end-to-end in
// dry-run mode, covering the RunE wiring (flag→opts mapping, Migrate
// call, PrintResult).
func TestNewCmdMigrate_DryRunFlag(t *testing.T) {
	t.Parallel()

	p := newMigrateFixture(t, anthropicSeed("sk-ant-cmd"))
	cmd := NewCmdMigrate(p)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--dry-run", "--yes"})

	require.NoError(t, cmd.Execute())
	out := buf.String()
	assert.Contains(t, out, "dry run")
	assert.Contains(t, out, "anthropic.api.key")
	// Dry-run must not mutate.
	assert.Equal(t, "sk-ant-cmd", p.Config.GetString("anthropic.api.key"))
}

// TestNewCmdMigrate_EnvVarFlag exercises the --env-var flag plumbed
// through parseEnvVarMap into opts and applied.
func TestNewCmdMigrate_EnvVarFlag(t *testing.T) {
	t.Parallel()

	p := newMigrateFixture(t, anthropicSeed("sk-ant-flag"))
	cmd := NewCmdMigrate(p)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--yes", "--env-var", "anthropic.api.key=FLAG_ANTHROPIC"})

	require.NoError(t, cmd.Execute())
	assert.Equal(t, "FLAG_ANTHROPIC", p.Config.GetString("anthropic.api.env"))
}

// TestNewCmdMigrate_BadEnvVarFlag — a malformed --env-var entry
// surfaces parseEnvVarMap's error through RunE.
func TestNewCmdMigrate_BadEnvVarFlag(t *testing.T) {
	t.Parallel()

	p := newMigrateFixture(t, anthropicSeed("sk-ant"))
	cmd := NewCmdMigrate(p)
	cmd.SetArgs([]string{"--yes", "--env-var", "noequalshere"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be <config-key>=<env-var-name>")
}

// TestNewCmdMigrate_TargetFlagInvalid — an invalid --target value
// propagates Migrate's validation error.
func TestNewCmdMigrate_TargetFlagInvalid(t *testing.T) {
	t.Parallel()

	p := newMigrateFixture(t, anthropicSeed("sk-ant"))
	cmd := NewCmdMigrate(p)
	cmd.SetArgs([]string{"--yes", "--target", "bogus"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid migration target")
}

// TestNewCmdMigrate_NoCandidates — empty config prints the
// nothing-to-migrate message.
func TestNewCmdMigrate_NoCandidates(t *testing.T) {
	t.Parallel()

	p := newMigrateFixture(t, nil)
	cmd := NewCmdMigrate(p)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--yes"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "No literal credentials found")
}
