package config

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/phpboyscout/go-tool-base/pkg/chat"
	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/credentials"
	"github.com/phpboyscout/go-tool-base/pkg/credentials/credtest"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

// newMigrateFixture produces Props with a memory-backed config
// container bound to a synthetic config file path. Seeds the config
// from an initial YAML tree written to the file so every key the
// test provides is a "real" file value rather than an in-memory
// Viper override — otherwise Viper's override map would survive the
// migrate's ReadInConfig reload and mask the rewrite.
//
// seed is nested YAML-compatible data (typically a map[string]any);
// pass nil for an empty config.
func newMigrateFixture(t *testing.T, seed map[string]any) *props.Props {
	t.Helper()

	fs := afero.NewMemMapFs()
	configPath := "/test/config.yaml"

	var payload []byte

	if seed != nil {
		data, err := yaml.Marshal(seed)
		require.NoError(t, err)

		payload = data
	} else {
		payload = []byte("{}")
	}

	require.NoError(t, afero.WriteFile(fs, configPath, payload, 0o600))

	v := viper.New()
	v.SetFs(fs)
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")
	require.NoError(t, v.ReadInConfig())

	return &props.Props{
		FS:     fs,
		Config: config.NewContainerFromViper(nil, v),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}
}

// anthropicSeed is the common shape for a pre-migration config with
// a single literal Anthropic API key. Kept as a helper so tests
// across the file stay consistent and short.
func anthropicSeed(value string) map[string]any {
	return map[string]any{
		"anthropic": map[string]any{
			"api": map[string]any{"key": value},
		},
	}
}

// bitbucketPairSeed is the pre-migration shape for the dual-
// credential Bitbucket case.
func bitbucketPairSeed(user, pw string) map[string]any {
	return map[string]any{
		"bitbucket": map[string]any{
			"username":     user,
			"app_password": pw,
		},
	}
}

// TestMigrate_NoCandidates — empty config should return a result
// with zero actions and no write flag.
func TestMigrate_NoCandidates(t *testing.T) {
	p := newMigrateFixture(t, nil)

	result, err := Migrate(t.Context(), p, MigrateOptions{DryRun: true, AssumeYes: true})
	require.NoError(t, err)
	assert.Empty(t, result.Actions)
	assert.False(t, result.WroteConfig)
}

// TestMigrate_DryRunEnvVarAIProvider — single-value credential
// with dry-run produces the expected action without mutating the
// config.
func TestMigrate_DryRunEnvVarAIProvider(t *testing.T) {
	p := newMigrateFixture(t, anthropicSeed("sk-ant-original"))

	result, err := Migrate(t.Context(), p, MigrateOptions{DryRun: true, AssumeYes: true})
	require.NoError(t, err)
	require.Len(t, result.Actions, 1)

	a := result.Actions[0]
	assert.Equal(t, chat.ConfigKeyClaudeKey, a.SourceKey)
	assert.Equal(t, chat.ConfigKeyClaudeEnv, a.DestKey)
	assert.Equal(t, chat.EnvClaudeKey, a.DestValue)
	assert.Equal(t, credentials.ModeEnvVar, a.Target)
	assert.False(t, a.Skipped)
	assert.False(t, result.WroteConfig)

	// Literal still present — dry run must not mutate.
	assert.Equal(t, "sk-ant-original", p.Config.GetString(chat.ConfigKeyClaudeKey))
	assert.Empty(t, p.Config.GetString(chat.ConfigKeyClaudeEnv))
}

// TestMigrate_AssumeYesEnvVarWritesConfig — full path: AssumeYes
// skips prompts, env ref written, literal cleared.
func TestMigrate_AssumeYesEnvVarWritesConfig(t *testing.T) {
	p := newMigrateFixture(t, map[string]any{
		"openai": map[string]any{"api": map[string]any{"key": "sk-openai-original"}},
	})

	result, err := Migrate(t.Context(), p, MigrateOptions{AssumeYes: true})
	require.NoError(t, err)
	require.Len(t, result.Actions, 1)

	assert.Equal(t, "OPENAI_API_KEY", p.Config.GetString(chat.ConfigKeyOpenAIEnv))
	assert.Empty(t, p.Config.GetString(chat.ConfigKeyOpenAIKey))
}

// TestMigrate_EnvVarOverride — --env-var flag pins a custom name,
// overriding the default.
func TestMigrate_EnvVarOverride(t *testing.T) {
	p := newMigrateFixture(t, map[string]any{
		"gemini": map[string]any{"api": map[string]any{"key": "gemini-original"}},
	})

	opts := MigrateOptions{
		AssumeYes: true,
		EnvVarOverrides: map[string]string{
			chat.ConfigKeyGeminiKey: "MYAPP_GEMINI_KEY",
		},
	}

	result, err := Migrate(t.Context(), p, opts)
	require.NoError(t, err)
	require.Len(t, result.Actions, 1)

	assert.Equal(t, "MYAPP_GEMINI_KEY", p.Config.GetString(chat.ConfigKeyGeminiEnv))
}

// TestMigrate_SkipsAlreadyMigrated — a prior run's env ref causes
// the candidate to be skipped instead of re-prompted.
func TestMigrate_SkipsAlreadyMigrated(t *testing.T) {
	p := newMigrateFixture(t, map[string]any{
		"anthropic": map[string]any{"api": map[string]any{
			"key": "sk-ant-leftover",
			"env": "ALREADY_SET",
		}},
	})

	result, err := Migrate(t.Context(), p, MigrateOptions{DryRun: true, AssumeYes: true})
	require.NoError(t, err)
	require.Len(t, result.Actions, 1)
	assert.True(t, result.Actions[0].Skipped)
	assert.Equal(t, "already migrated", result.Actions[0].Reason)
}

// TestMigrate_KeychainSingleValue — keychain mode for a single-value
// credential stores the secret and records the reference.
func TestMigrate_KeychainSingleValue(t *testing.T) {
	credtest.Install(t)

	p := newMigrateFixture(t, map[string]any{
		"github": map[string]any{"auth": map[string]any{"value": "ghp_kc_original"}},
	})

	opts := MigrateOptions{
		AssumeYes: true,
		Target:    credentials.ModeKeychain,
	}

	result, err := Migrate(t.Context(), p, opts)
	require.NoError(t, err)
	require.Len(t, result.Actions, 1)

	assert.Equal(t, "testtool/github.auth", p.Config.GetString("github.auth.keychain"))
	assert.Empty(t, p.Config.GetString("github.auth.value"))

	stored, err := credentials.Retrieve(t.Context(), "testtool", "github.auth")
	require.NoError(t, err)
	assert.Equal(t, "ghp_kc_original", stored)
}

// TestMigrate_KeychainNoBackendRefuses — keychain target without a
// registered backend returns an actionable error.
func TestMigrate_KeychainNoBackendRefuses(t *testing.T) {
	p := newMigrateFixture(t, anthropicSeed("sk-ant"))

	_, err := Migrate(t.Context(), p, MigrateOptions{
		AssumeYes: true,
		Target:    credentials.ModeKeychain,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no keychain-capable Backend")
}

// TestMigrate_BitbucketDualCredentialEnvVar — username + app_password
// migrate as a pair; both refs written, both literals cleared.
func TestMigrate_BitbucketDualCredentialEnvVar(t *testing.T) {
	p := newMigrateFixture(t, bitbucketPairSeed("alice", "s3cret"))

	result, err := Migrate(t.Context(), p, MigrateOptions{AssumeYes: true})
	require.NoError(t, err)
	require.Len(t, result.Actions, 1)

	a := result.Actions[0]
	assert.Equal(t, "bitbucket.username", a.SourceKey)
	assert.Equal(t, "bitbucket.app_password", a.PartnerKey)

	assert.Equal(t, "BITBUCKET_USERNAME", p.Config.GetString("bitbucket.username.env"))
	assert.Equal(t, "BITBUCKET_APP_PASSWORD", p.Config.GetString("bitbucket.app_password.env"))
	assert.Empty(t, p.Config.GetString("bitbucket.username"))
	assert.Empty(t, p.Config.GetString("bitbucket.app_password"))
}

// TestMigrate_BitbucketDualCredentialKeychain — both halves stored
// as a single JSON blob under one keychain entry.
func TestMigrate_BitbucketDualCredentialKeychain(t *testing.T) {
	credtest.Install(t)

	p := newMigrateFixture(t, bitbucketPairSeed("alice", "s3cret"))

	opts := MigrateOptions{
		AssumeYes: true,
		Target:    credentials.ModeKeychain,
	}

	result, err := Migrate(t.Context(), p, opts)
	require.NoError(t, err)
	require.Len(t, result.Actions, 1)

	assert.Equal(t, "testtool/bitbucket.auth", p.Config.GetString("bitbucket.keychain"))

	raw, err := credentials.Retrieve(t.Context(), "testtool", "bitbucket.auth")
	require.NoError(t, err)

	var blob map[string]string
	require.NoError(t, json.Unmarshal([]byte(raw), &blob))
	assert.Equal(t, "alice", blob["username"])
	assert.Equal(t, "s3cret", blob["app_password"])
}

// TestMigrate_ConfigDefaultTargetCascades — when --target is
// omitted, the command honours the `credentials.migrate.default_target`
// config key.
func TestMigrate_ConfigDefaultTargetCascades(t *testing.T) {
	credtest.Install(t)

	p := newMigrateFixture(t, map[string]any{
		"anthropic": map[string]any{"api": map[string]any{"key": "sk-ant-cascade"}},
		"credentials": map[string]any{"migrate": map[string]any{
			"default_target": string(credentials.ModeKeychain),
		}},
	})

	// Note: opts.Target deliberately unset.
	result, err := Migrate(t.Context(), p, MigrateOptions{AssumeYes: true})
	require.NoError(t, err)
	require.Len(t, result.Actions, 1)
	assert.Equal(t, credentials.ModeKeychain, result.Actions[0].Target)
}

// TestMigrate_InvalidTargetRejected — an unknown --target value
// returns a hinted error rather than falling through.
func TestMigrate_InvalidTargetRejected(t *testing.T) {
	p := newMigrateFixture(t, nil)

	_, err := Migrate(t.Context(), p, MigrateOptions{
		AssumeYes: true,
		Target:    credentials.Mode("bogus"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid migration target")
}

// TestMigrate_KeychainServiceOverride — --keychain-service pins the
// service half of the <service>/<account> reference.
func TestMigrate_KeychainServiceOverride(t *testing.T) {
	credtest.Install(t)

	p := newMigrateFixture(t, map[string]any{
		"openai": map[string]any{"api": map[string]any{"key": "sk-openai"}},
	})

	opts := MigrateOptions{
		AssumeYes:       true,
		Target:          credentials.ModeKeychain,
		KeychainService: "customsvc",
	}

	_, err := Migrate(t.Context(), p, opts)
	require.NoError(t, err)
	assert.Equal(t, "customsvc/openai.api", p.Config.GetString(chat.ConfigKeyOpenAIKeychain))
}

// TestParseEnvVarMap covers the flag parser used by the cobra
// command.
func TestParseEnvVarMap(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		m, err := parseEnvVarMap([]string{
			"anthropic.api.key=MY_ANTHROPIC",
			"openai.api.key=MY_OPENAI",
		})
		require.NoError(t, err)
		assert.Equal(t, "MY_ANTHROPIC", m["anthropic.api.key"])
		assert.Equal(t, "MY_OPENAI", m["openai.api.key"])
	})

	t.Run("empty input returns nil", func(t *testing.T) {
		t.Parallel()

		m, err := parseEnvVarMap(nil)
		require.NoError(t, err)
		assert.Nil(t, m)
	})

	t.Run("missing equals rejected", func(t *testing.T) {
		t.Parallel()

		_, err := parseEnvVarMap([]string{"anthropic.api.key"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be <config-key>=<env-var-name>")
	})

	t.Run("empty value rejected", func(t *testing.T) {
		t.Parallel()

		_, err := parseEnvVarMap([]string{"anthropic.api.key="})
		require.Error(t, err)
	})
}

// TestPrintResult spot-checks the human-readable output format for
// the three interesting states: empty, dry-run plan, and applied.
func TestPrintResult(t *testing.T) {
	t.Parallel()

	var w bytes.Buffer

	t.Run("empty", func(t *testing.T) {
		w.Reset()
		PrintResult(&w, &MigrateResult{}, true)
		assert.Contains(t, w.String(), "No literal credentials")
	})

	t.Run("dry-run plan", func(t *testing.T) {
		w.Reset()
		PrintResult(&w, &MigrateResult{
			Actions: []MigrationAction{
				{
					SourceKey: chat.ConfigKeyClaudeKey,
					DestKey:   chat.ConfigKeyClaudeEnv,
					DestValue: chat.EnvClaudeKey,
					Target:    credentials.ModeEnvVar,
				},
			},
		}, true)

		output := w.String()
		assert.Contains(t, output, "dry run")
		assert.Contains(t, output, chat.ConfigKeyClaudeKey)
		assert.Contains(t, output, chat.EnvClaudeKey)
	})

	t.Run("applied with skip", func(t *testing.T) {
		w.Reset()
		PrintResult(&w, &MigrateResult{
			Actions: []MigrationAction{
				{
					SourceKey: chat.ConfigKeyClaudeKey,
					Skipped:   true,
					Reason:    "already migrated",
				},
			},
		}, false)

		output := w.String()
		assert.Contains(t, output, "Migration complete")
		assert.Contains(t, output, "SKIP")
		assert.Contains(t, output, "already migrated")
	})
}

// TestScanBitbucketPair — isolated scanner coverage for the dual-
// credential pairing, including the partial-pair case.
func TestScanBitbucketPair(t *testing.T) {
	t.Run("both present", func(t *testing.T) {
		v := viper.New()
		v.Set("bitbucket.username", "alice")
		v.Set("bitbucket.app_password", "s3cret")

		pair := scanBitbucketPair(config.NewContainerFromViper(nil, v))
		require.NotNil(t, pair)
		assert.Equal(t, "bitbucket.username", pair.Key)
		assert.Equal(t, "bitbucket.app_password", pair.PartnerKey)
		assert.Equal(t, "alice", pair.Value)
	})

	t.Run("only username present", func(t *testing.T) {
		v := viper.New()
		v.Set("bitbucket.username", "alice")

		pair := scanBitbucketPair(config.NewContainerFromViper(nil, v))
		require.NotNil(t, pair, "partial pair still surfaces as a candidate")
		assert.Equal(t, "alice", pair.Value)
	})

	t.Run("neither present", func(t *testing.T) {
		v := viper.New()

		pair := scanBitbucketPair(config.NewContainerFromViper(nil, v))
		assert.Nil(t, pair)
	})

	t.Run("whitespace-only treated as absent", func(t *testing.T) {
		v := viper.New()
		v.Set("bitbucket.username", "   ")
		v.Set("bitbucket.app_password", "\t")

		pair := scanBitbucketPair(config.NewContainerFromViper(nil, v))
		assert.Nil(t, pair)
	})
}

// TestDefaultEnvVarName covers the known-key lookup plus the
// sanitised fallback for unknown keys.
func TestDefaultEnvVarName(t *testing.T) {
	t.Parallel()

	type pair struct {
		configKey  string
		envVarName string
	}

	cases := []pair{
		{chat.ConfigKeyClaudeKey, chat.EnvClaudeKey},
		{chat.ConfigKeyOpenAIKey, chat.EnvOpenAIKey},
		{chat.ConfigKeyGeminiKey, chat.EnvGeminiKey},
		{"github.auth.value", "GITHUB_TOKEN"},
		{"gitlab.auth.value", "GITLAB_TOKEN"},
		{"bitbucket.username", "BITBUCKET_USERNAME"},
		{"bitbucket.app_password", "BITBUCKET_APP_PASSWORD"},
		{"unknown.custom.value", "UNKNOWN_CUSTOM_VALUE"},
	}

	for _, tc := range cases {
		t.Run(tc.configKey, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.envVarName, defaultEnvVarName(tc.configKey))
		})
	}
}

// TestValidateEnvVarName pins the accepted / rejected shapes —
// matches the constraint used by the setup wizards.
func TestValidateEnvVarName(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		in      string
		wantErr bool
	}{
		"empty":       {"", true},
		"lowercase":   {"github_token", true},
		"valid":       {"GITHUB_TOKEN", false},
		"with digit":  {"TOKEN_2", false},
		"leads digit": {"2TOKEN", true},
		"too long":    {"A" + strings.Repeat("B", 65), true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := validateEnvVarName(tc.in)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
