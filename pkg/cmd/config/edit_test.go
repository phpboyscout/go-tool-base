package config_test

import (
	"bytes"
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gtbconfig "gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// fakeEditor returns an editorRunner that overwrites the temp file with the
// given bytes — a deterministic stand-in for an interactive editor.
func fakeEditor(fs afero.Fs, newContent string) func(context.Context, []string, string) error {
	return func(_ context.Context, _ []string, path string) error {
		return afero.WriteFile(fs, path, []byte(newContent), 0o600)
	}
}

func alwaysInteractive() bool { return true }

func TestCmdEdit_HappyPath(t *testing.T) {
	t.Parallel()

	p, fs, path := newFileConfig(t, "log:\n  level: info\n")
	edited := "log:\n  level: debug\nfeature:\n  enabled: true\n"

	cmd := config.NewCmdEdit(p,
		config.WithEditorRunner(fakeEditor(fs, edited)),
		config.WithInteractiveCheck(alwaysInteractive),
	)
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "saved")

	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "debug")
	// Live config reflects the reload.
	assert.Equal(t, "debug", p.Config.GetString("log.level"))
}

func TestCmdEdit_NonInteractiveRefused(t *testing.T) {
	t.Parallel()

	p, _, _ := newFileConfig(t, "log:\n  level: info\n")

	cmd := config.NewCmdEdit(p,
		config.WithEditorRunner(fakeEditor(nil, "")),
		config.WithInteractiveCheck(func() bool { return false }),
	)
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "interactive terminal")
}

func TestCmdEdit_EditorNonZeroExit(t *testing.T) {
	t.Parallel()

	p, fs, path := newFileConfig(t, "log:\n  level: info\n")
	runner := func(context.Context, []string, string) error {
		return assert.AnError
	}

	cmd := config.NewCmdEdit(p,
		config.WithEditorRunner(runner),
		config.WithInteractiveCheck(alwaysInteractive),
	)
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "left unchanged")

	// Original intact, temp discarded.
	data, rerr := afero.ReadFile(fs, path)
	require.NoError(t, rerr)
	assert.Contains(t, string(data), "info")
	exists, _ := afero.Exists(fs, path+".edit.tmp")
	assert.False(t, exists)
}

func TestCmdEdit_InvalidYAMLAborts(t *testing.T) {
	t.Parallel()

	p, fs, path := newFileConfig(t, "log:\n  level: info\n")

	cmd := config.NewCmdEdit(p,
		config.WithEditorRunner(fakeEditor(fs, "log:\n  level: : : bad\n  - nope\n")),
		config.WithInteractiveCheck(alwaysInteractive),
	)
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "valid YAML")

	// Original intact; temp retained for recovery.
	data, rerr := afero.ReadFile(fs, path)
	require.NoError(t, rerr)
	assert.Contains(t, string(data), "info")
	exists, _ := afero.Exists(fs, path+".edit.tmp")
	assert.True(t, exists)
}

func TestCmdEdit_SchemaInvalidAborts(t *testing.T) {
	t.Parallel()

	p, fs, path := newFileConfig(t, "log:\n  level: info\n")

	// Valid YAML, but removes the required log.level key.
	cmd := config.NewCmdEdit(p,
		config.WithEditorRunner(fakeEditor(fs, "feature:\n  enabled: true\n")),
		config.WithInteractiveCheck(alwaysInteractive),
	)
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid")

	data, rerr := afero.ReadFile(fs, path)
	require.NoError(t, rerr)
	assert.Contains(t, string(data), "info")
	exists, _ := afero.Exists(fs, path+".edit.tmp")
	assert.True(t, exists)
}

func TestCmdEdit_UnchangedNoOp(t *testing.T) {
	t.Parallel()

	original := "log:\n  level: info\n"
	p, fs, _ := newFileConfig(t, original)

	cmd := config.NewCmdEdit(p,
		config.WithEditorRunner(fakeEditor(fs, original)),
		config.WithInteractiveCheck(alwaysInteractive),
	)
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "no changes made")
}

func TestCmdEdit_EditorArgvSplit(t *testing.T) {
	t.Parallel()

	p, fs, _ := newFileConfig(t, "log:\n  level: info\n")

	var gotArgv []string
	runner := func(_ context.Context, argv []string, path string) error {
		gotArgv = argv

		return afero.WriteFile(fs, path, []byte("log:\n  level: info\n"), 0o600)
	}

	cmd := config.NewCmdEdit(p,
		config.WithEditorRunner(runner),
		config.WithInteractiveCheck(alwaysInteractive),
	)
	cmd.SetArgs([]string{"--editor", `myeditor --wait "/opt/My Editor/bin"`})

	require.NoError(t, cmd.Execute())
	assert.Equal(t, []string{"myeditor", "--wait", "/opt/My Editor/bin"}, gotArgv)
}

func TestCmdEdit_NilConfig(t *testing.T) {
	t.Parallel()

	cmd := config.NewCmdEdit(&props.Props{Config: nil},
		config.WithInteractiveCheck(alwaysInteractive))
	assert.Error(t, cmd.Execute())
}

// TestCmdEdit_SeedsNewFile covers the not-yet-existing-file path: the temp file
// is seeded with a header comment (never the full effective config), the editor
// adds valid content, and it is persisted at the default writable path. No file
// is bound, so reload is skipped — matching "config set".
func TestCmdEdit_SeedsNewFile(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	c := gtbconfig.NewReaderContainer(fs, gtbconfig.WithLogger(logger.ToSlog(logger.NewNoop())),
		gtbconfig.WithConfigFormat("yaml"),
		gtbconfig.WithConfigReaders(strings.NewReader("log:\n  level: info\n")))
	p := &props.Props{Config: c, FS: fs, Tool: props.Tool{Name: "seedtool"}}

	var seenSeed string
	runner := func(_ context.Context, _ []string, path string) error {
		data, _ := afero.ReadFile(fs, path)
		seenSeed = string(data)

		return afero.WriteFile(fs, path, []byte("log:\n  level: warn\n"), 0o600)
	}

	cmd := config.NewCmdEdit(p,
		config.WithEditorRunner(runner),
		config.WithInteractiveCheck(alwaysInteractive),
	)
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	require.NoError(t, cmd.Execute())
	// The seed is a header comment, not the effective config (no "info").
	assert.Contains(t, seenSeed, "# seedtool configuration")
	assert.NotContains(t, seenSeed, "info")

	want := filepath.Join(setup.GetDefaultConfigDir(fs, "seedtool"), setup.DefaultConfigFilename)
	data, err := afero.ReadFile(fs, want)
	require.NoError(t, err)
	assert.Contains(t, string(data), "warn")
}

// TestCmdEdit_RealEditorRunner exercises the default (non-injected) editor
// runner against a harmless real command, proving the subprocess wiring works.
func TestCmdEdit_RealEditorRunner(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses the unix 'true' binary")
	}
	t.Parallel()

	p, _, _ := newFileConfig(t, "log:\n  level: info\n")

	// No editor runner injected — defaultEditorRunner runs `true`, which exits 0
	// without touching the file, so the content is unchanged → no-op.
	cmd := config.NewCmdEdit(p, config.WithInteractiveCheck(alwaysInteractive))
	cmd.SetArgs([]string{"--editor", "true"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "no changes made")
}

func TestResolveEditor_Precedence(t *testing.T) {
	// Not parallel: mutates process environment via t.Setenv.
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")

	p, fs, _ := newFileConfig(t, "log:\n  level: info\n")
	var gotArgv []string
	runner := func(_ context.Context, argv []string, path string) error {
		gotArgv = argv

		return afero.WriteFile(fs, path, []byte("log:\n  level: info\n"), 0o600)
	}

	run := func() []string {
		cmd := config.NewCmdEdit(p,
			config.WithEditorRunner(runner),
			config.WithInteractiveCheck(alwaysInteractive))
		require.NoError(t, cmd.Execute())

		return gotArgv
	}

	t.Setenv("EDITOR", "ed-from-editor")
	assert.Equal(t, "ed-from-editor", run()[0])

	t.Setenv("VISUAL", "ed-from-visual")
	assert.Equal(t, "ed-from-visual", run()[0], "$VISUAL takes precedence over $EDITOR")
}
