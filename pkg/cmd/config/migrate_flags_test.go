package config

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
)

// TestNewCmdMigrate_DryRunFlag drives the cobra command end-to-end in
// dry-run mode, covering the RunE wiring (flag→opts mapping, Migrate
// call, PrintResult).
func TestNewCmdMigrate_DryRunFlag(t *testing.T) {
	t.Parallel()

	p := newMigrateFixture(t, anthropicSeed("sk-ant-cmd"))
	cmd := NewCmdMigrate(p, WithModeEnvironment(credentialposture.ModeEnvironment{}))

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--dry-run", "--yes"})

	require.NoError(t, cmd.Execute())
	out := buf.String()
	assert.Contains(t, out, "dry run")
	assert.Contains(t, out, "anthropic.api.key")
	// Dry-run must not mutate.
	assert.Equal(t, "sk-ant-cmd", p.Config.View().GetString("anthropic.api.key"))
}

// TestNewCmdMigrate_EnvVarFlag exercises the --env-var flag plumbed
// through parseEnvVarMap into opts and applied.
func TestNewCmdMigrate_EnvVarFlag(t *testing.T) {
	t.Parallel()

	p := newMigrateFixture(t, anthropicSeed("sk-ant-flag"))
	cmd := NewCmdMigrate(p, WithModeEnvironment(credentialposture.ModeEnvironment{}))

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--yes", "--env-var", "anthropic.api.key=FLAG_ANTHROPIC"})

	require.NoError(t, cmd.Execute())
	assert.Equal(t, "FLAG_ANTHROPIC", p.Config.View().GetString("anthropic.api.env"))
}

// TestNewCmdMigrate_BadEnvVarFlag — a malformed --env-var entry
// surfaces parseEnvVarMap's error through RunE.
func TestNewCmdMigrate_BadEnvVarFlag(t *testing.T) {
	t.Parallel()

	p := newMigrateFixture(t, anthropicSeed("sk-ant"))
	cmd := NewCmdMigrate(p, WithModeEnvironment(credentialposture.ModeEnvironment{}))
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
	cmd := NewCmdMigrate(p, WithModeEnvironment(credentialposture.ModeEnvironment{}))
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
	cmd := NewCmdMigrate(p, WithModeEnvironment(credentialposture.ModeEnvironment{}))

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--yes"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "No literal credentials found")
}
