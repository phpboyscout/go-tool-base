package config_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cfg "gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestCmdValidate_ValidConfig(t *testing.T) {
	t.Parallel()

	p := newTestProps(t, "log:\n  level: info\n")
	cmd := config.NewCmdValidate(p)

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "valid")
}

func TestCmdValidate_InvalidConfig(t *testing.T) {
	t.Parallel()

	// The base schema requires log.level; a config without it is invalid.
	p := newTestProps(t, "feature:\n  enabled: true\n")
	cmd := config.NewCmdValidate(p)

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, buf.String(), "error:")
	assert.Contains(t, buf.String(), "log.level")
}

func TestCmdValidate_WarningDoesNotFail(t *testing.T) {
	t.Parallel()

	// An unknown key in a user-authored (file) layer yields a warning, not an
	// error. Provenance matters: the same key in an embedded-defaults reader
	// layer is filtered from the warnings.
	p := &props.Props{Config: testutil.FileStoreFromYAML(t, "log:\n  level: info\nunknown:\n  key: surplus\n")}
	cmd := config.NewCmdValidate(p)

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "warning:")
}

// TestCmdValidate_FrameworkKeyNotFlagged pins the #2 fix: a user-authored key
// under a framework section (server.grpc.reflection) is a recognised key, not
// a typo the base schema fails to enumerate, so it produces no unknown-key
// warning even though the schema only types log.level.
func TestCmdValidate_FrameworkKeyNotFlagged(t *testing.T) {
	t.Parallel()

	p := &props.Props{Config: testutil.FileStoreFromYAML(t,
		"log:\n  level: info\nserver:\n  grpc:\n    reflection: true\n")}
	cmd := config.NewCmdValidate(p)

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	require.NoError(t, cmd.Execute())
	assert.NotContains(t, buf.String(), "server.grpc.reflection")
	assert.NotContains(t, buf.String(), "warning:")
	assert.Contains(t, buf.String(), "valid")
}

// TestCmdValidate_GenuineUnknownStillWarns pins that #2 does not over-suppress:
// a key under no framework section and declared nowhere is still flagged.
func TestCmdValidate_GenuineUnknownStillWarns(t *testing.T) {
	t.Parallel()

	p := &props.Props{Config: testutil.FileStoreFromYAML(t,
		"log:\n  level: info\nweirdsection:\n  typo: value\n")}
	cmd := config.NewCmdValidate(p)

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "weirdsection.typo")
	assert.Contains(t, buf.String(), "unknown configuration key")
}

func TestCmdValidate_NilConfig(t *testing.T) {
	t.Parallel()

	p := &props.Props{Config: nil}
	cmd := config.NewCmdValidate(p)

	err := cmd.Execute()
	assert.Error(t, err)
}

// A value the environment supplied is reported with the variable that
// supplied it, so the reader renames a shell variable rather than searching a
// config file that is fine (spec 0205 D1).
func TestCmdValidate_NamesTheVariableBehindAKey(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, opts ...cfg.StoreOption) string {
		t.Helper()

		store, err := cfg.NewStore(t.Context(), opts...)
		require.NoError(t, err)

		cmd := config.NewCmdValidate(&props.Props{Config: store})

		var buf bytes.Buffer
		cmd.SetOut(&buf)
		_ = cmd.Execute()

		return buf.String()
	}

	file := cfg.WithReaders(cfg.NamedSource{Name: "file", Content: []byte("log:\n  level: info\n")})

	t.Run("an invalid value from the environment names its variable", func(t *testing.T) {
		t.Parallel()

		out := run(t, file, cfg.WithEnv("TOOLTEST", cfg.WithEnviron(func() []string { return []string{"TOOLTEST_LOG_LEVEL=loud"} })))
		assert.Contains(t, out, "log.level")
		assert.Contains(t, out, "from environment variable TOOLTEST_LOG_LEVEL")
	})

	t.Run("the same value from a file names no variable", func(t *testing.T) {
		t.Parallel()

		out := run(t, cfg.WithReaders(cfg.NamedSource{Name: "file", Content: []byte("log:\n  level: loud\n")}))
		assert.Contains(t, out, "log.level")
		assert.NotContains(t, out, "environment variable")
	})
}
