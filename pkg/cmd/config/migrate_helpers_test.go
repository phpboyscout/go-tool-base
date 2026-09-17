package config

import (
	"bytes"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/credentials"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
)

// TestDefaultVCSEnvVarName_RemainingKeys covers the gitea / codeberg /
// direct branches plus the empty fallback for an unknown key.
func TestDefaultEnvVarName_ForgeKeys(t *testing.T) {
	t.Parallel()

	for key, want := range map[string]string{ //nolint:gosec // G101: config keys and env var names, not credentials
		"gitea.auth.value":       "GITEA_TOKEN",
		"codeberg.auth.value":    "CODEBERG_TOKEN",
		"direct.auth.value":      "DIRECT_TOKEN",
		"bitbucket.app_password": "BITBUCKET_APP_PASSWORD",
		"unknown.key":            "UNKNOWN_KEY",
	} {
		assert.Equal(t, want, defaultEnvVarName(key), key)
	}
}

// TestSecretForKeychain covers single-value (raw) and dual-credential
// (JSON blob) shapes, plus jsonBlobFieldFor's default branch.
func TestSecretForKeychain(t *testing.T) {
	t.Parallel()

	t.Run("single value returns raw secret", func(t *testing.T) {
		t.Parallel()

		cfg := testutil.ViewFromYAML(t, "other: value\n")

		secret, err := secretForKeychain(cfg, literalCredential{Value: "ghp_raw"})
		require.NoError(t, err)
		assert.Equal(t, "ghp_raw", secret)
	})

	t.Run("dual credential returns json blob", func(t *testing.T) {
		t.Parallel()

		cfg := testutil.ViewFromYAML(t, "bitbucket:\n  app_password: pw\n")

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

		cfg := testutil.FileViewFromYAML(t, "anthropic:\n  api:\n    env: X\n")
		assert.True(t, alreadyMigrated(cfg, c, credentials.ModeEnvVar))
	})

	t.Run("keychain target set", func(t *testing.T) {
		t.Parallel()

		cfg := testutil.FileViewFromYAML(t, "anthropic:\n  api:\n    keychain: svc/acct\n")
		assert.True(t, alreadyMigrated(cfg, c, credentials.ModeKeychain))
	})

	t.Run("env target empty", func(t *testing.T) {
		t.Parallel()

		cfg := testutil.FileViewFromYAML(t, "other: value\n")
		assert.False(t, alreadyMigrated(cfg, c, credentials.ModeEnvVar))
	})

	t.Run("defaults-supplied target does not count as migrated", func(t *testing.T) {
		t.Parallel()

		// The env target resolves from an embedded-defaults reader layer (the
		// github bundle ships auth.env: GITHUB_TOKEN this way) while the file
		// still holds the literal — the credential is NOT migrated.
		cfg := testutil.FileStoreFromYAML(t, "anthropic:\n  api:\n    key: sk-literal\n",
			config.WithReaders(config.NamedSource{Name: "embedded:defaults", Content: []byte("anthropic:\n  api:\n    env: ANTHROPIC_API_KEY\n")})).View()
		assert.False(t, alreadyMigrated(cfg, c, credentials.ModeEnvVar))
	})

	t.Run("literal target is never already migrated", func(t *testing.T) {
		t.Parallel()

		cfg := testutil.ViewFromYAML(t, "other: value\n")
		assert.False(t, alreadyMigrated(cfg, c, credentials.ModeLiteral))
	})

	t.Run("unknown mode falls through to false", func(t *testing.T) {
		t.Parallel()

		cfg := testutil.ViewFromYAML(t, "other: value\n")
		assert.False(t, alreadyMigrated(cfg, c, credentials.Mode("bogus")))
	})
}
