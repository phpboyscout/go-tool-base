package update_test

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/exectest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/update"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

func TestUpdate_SemVerValidation(t *testing.T) {
	t.Parallel()

	mu := &mockUpdater{
		latestVersion: "v1.2.3",
		binPath:       "/tmp/new-bin",
	}

	props := &p.Props{
		Logger:  logger.NewNoop(),
		FS:      afero.NewMemMapFs(),
		Version: version.NewInfo("v1.0.0", "head", "now"),
	}

	cmd := update.NewCmdUpdate(props, update.WithUpdater(
		func(_ context.Context, _ *p.Props, _ string, _ bool) (update.Updater, error) {
			return mu, nil
		},
	))
	cmd.SetContext(context.Background())

	tests := []struct {
		name    string
		version string
		wantErr bool
	}{
		{"valid version", "v1.2.3", false},
		{"valid with suffix", "v1.2.3-alpha", false},
		{"invalid format", "1.2.3", true},
		{"garbage", "not-a-version", true},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cmd.Flags().Set("version", tt.version)
			require.NoError(t, err)

			err = cmd.RunE(cmd.Command, nil)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid version format")
			} else if err != nil {
				assert.NotContains(t, err.Error(), "invalid version format")
			}
		})
	}
}

func TestUpdateConfig(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	props := &p.Props{
		FS: fs,
		Tool: p.Tool{
			Name: "test-tool",
		},
		Logger: logger.NewNoop(),
	}

	t.Run("success_path", func(t *testing.T) {
		t.Parallel()

		localFS := afero.NewMemMapFs()
		localProps := &p.Props{
			FS:     localFS,
			Tool:   p.Tool{Name: "test-tool"},
			Logger: logger.NewNoop(),
		}

		var executedCommands []string
		_ = localFS.MkdirAll(setup.GetDefaultConfigDir(localFS, "test-tool"), 0755)
		_ = localFS.MkdirAll("/etc/test-tool", 0755)

		update.UpdateConfig(context.Background(), localProps, "/bin/new-tool",
			update.WithExecCommand(exectest.TrackingCommand(&executedCommands)),
		)

		assert.Len(t, executedCommands, 2)
		assert.Contains(t, executedCommands[0], "/bin/new-tool")
		assert.Contains(t, executedCommands[1], "/bin/new-tool")
	})

	t.Run("skips_when_init_disabled", func(t *testing.T) {
		t.Parallel()

		localProps := &p.Props{
			FS:     afero.NewMemMapFs(),
			Tool:   p.Tool{Name: "test-tool", Features: p.SetFeatures(p.Disable(p.InitCmd))},
			Logger: logger.NewNoop(),
		}

		var executedCommands []string
		update.UpdateConfig(context.Background(), localProps, "/bin/new-tool",
			update.WithExecCommand(exectest.TrackingCommand(&executedCommands)),
		)
		assert.Empty(t, executedCommands)
	})

	t.Run("handles_init_error", func(t *testing.T) {
		t.Parallel()

		localFS := afero.NewMemMapFs()
		_ = localFS.MkdirAll(setup.GetDefaultConfigDir(localFS, "test-tool"), 0755)

		buf := logger.NewBuffer()
		localProps := &p.Props{
			FS:     localFS,
			Tool:   p.Tool{Name: "test-tool", Features: p.SetFeatures(p.Enable(p.InitCmd))},
			Logger: buf,
		}

		update.UpdateConfig(context.Background(), localProps, "/bin/new-tool",
			update.WithExecCommand(exectest.FailCommand()),
		)

		assert.True(t, buf.ContainsLevel(logger.WarnLevel, "could not update config"))
	})

	_ = props // keep the outer props in scope for linting
	_ = fs
}

type mockUpdater struct {
	latestVersion   string
	binPath         string
	releaseNotes    string
	updateErr       error
	notesErr        error
	fromFileErr     error
	fromFileBinPath string
}

func (m *mockUpdater) GetLatestVersionString(ctx context.Context) (string, error) {
	return m.latestVersion, nil
}

func (m *mockUpdater) Update(ctx context.Context) (string, error) {
	return m.binPath, m.updateErr
}

func (m *mockUpdater) UpdateFromFile(_ context.Context, filePath string) (string, error) {
	if m.fromFileErr != nil {
		return "", m.fromFileErr
	}

	return m.fromFileBinPath, nil
}

func (m *mockUpdater) GetReleaseNotes(ctx context.Context, from, to string) (string, error) {
	return m.releaseNotes, m.notesErr
}

func (m *mockUpdater) GetCurrentVersion() string {
	return "v1.0.0"
}

func TestUpdate(t *testing.T) {
	t.Parallel()

	props := &p.Props{
		FS:      afero.NewMemMapFs(),
		Tool:    p.Tool{Name: "test-tool"},
		Logger:  logger.NewNoop(),
		Version: version.NewInfo("v1.0.0", "head", "now"),
	}

	t.Run("successful_update", func(t *testing.T) {
		t.Parallel()

		mu := &mockUpdater{
			latestVersion: "v1.1.0",
			binPath:       "/tmp/new-bin",
			releaseNotes:  "New features!",
		}

		result, err := update.Update(context.Background(), props, "", false, io.Discard, update.WithUpdater(
			func(_ context.Context, _ *p.Props, _ string, _ bool) (update.Updater, error) {
				return mu, nil
			},
		))
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.Updated)
		assert.Equal(t, "v1.0.0", result.PreviousVersion)
		assert.Equal(t, "v1.1.0", result.NewVersion)
	})

	t.Run("updater_creation_failure", func(t *testing.T) {
		t.Parallel()

		_, err := update.Update(context.Background(), props, "", false, io.Discard, update.WithUpdater(
			func(_ context.Context, _ *p.Props, _ string, _ bool) (update.Updater, error) {
				return nil, fmt.Errorf("failed to create updater")
			},
		))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create updater")
	})

	t.Run("update_execution_failure", func(t *testing.T) {
		t.Parallel()

		mu := &mockUpdater{
			updateErr: fmt.Errorf("download failed"),
		}

		_, err := update.Update(context.Background(), props, "", false, io.Discard, update.WithUpdater(
			func(_ context.Context, _ *p.Props, _ string, _ bool) (update.Updater, error) {
				return mu, nil
			},
		))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "download failed")
	})
}

// TestUpdate_WithUpdaterOption proves the parallel-safe injection path:
// WithUpdater supplies the factory directly per call site, so concurrent
// tests cannot clobber one another and the test is safe under t.Parallel.
func TestUpdate_WithUpdaterOption(t *testing.T) {
	t.Parallel()

	props := &p.Props{
		FS:      afero.NewMemMapFs(),
		Tool:    p.Tool{Name: "test-tool"},
		Logger:  logger.NewNoop(),
		Version: version.NewInfo("v1.0.0", "head", "now"),
	}

	mu := &mockUpdater{latestVersion: "v1.2.0", binPath: "/tmp/new-bin"}

	result, err := update.Update(context.Background(), props, "", false, io.Discard,
		update.WithUpdater(func(_ context.Context, _ *p.Props, _ string, _ bool) (update.Updater, error) {
			return mu, nil
		}),
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Updated)
	assert.Equal(t, "v1.2.0", result.NewVersion)
}

// TestUpdateFromFile_WithOfflineUpdaterOption proves the offline path
// honours the injected factory via NewCmdUpdate + the --from-file flag,
// without mutating any package var, and is therefore parallel-safe.
func TestUpdateFromFile_WithOfflineUpdaterOption(t *testing.T) {
	t.Parallel()

	props := &p.Props{
		FS:      afero.NewMemMapFs(),
		Tool:    p.Tool{Name: "test-tool"},
		Logger:  logger.NewNoop(),
		Version: version.NewInfo("v1.0.0", "head", "now"),
	}

	called := false
	mu := &mockUpdater{fromFileBinPath: "/usr/local/bin/test-tool"}

	cmd := update.NewCmdUpdate(props, update.WithOfflineUpdater(func(_ *p.Props) update.Updater {
		called = true

		return mu
	}))
	cmd.SetArgs([]string{"--from-file", "/tmp/release.tar.gz"})

	require.NoError(t, cmd.Execute())
	assert.True(t, called, "injected offline updater factory must be consulted")
}

func TestNewCmdUpdate_MutualExclusion(t *testing.T) {
	t.Parallel()

	props := &p.Props{
		Tool:   p.Tool{Name: "test-tool"},
		Logger: logger.NewNoop(),
	}

	cmd := update.NewCmdUpdate(props)
	require.NoError(t, cmd.Flags().Set("from-file", "/tmp/release.tar.gz"))
	require.NoError(t, cmd.Flags().Set("version", "v1.0.0"))

	err := cmd.ValidateFlagGroups()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "from-file")
}

func TestUpdateFromFile_ViaCommand(t *testing.T) {
	t.Parallel()

	mu := &mockUpdater{
		fromFileBinPath: "/usr/local/bin/test-tool",
	}

	props := &p.Props{
		FS:     afero.NewMemMapFs(),
		Tool:   p.Tool{Name: "test-tool"},
		Logger: logger.NewNoop(),
	}

	cmd := update.NewCmdUpdate(props, update.WithOfflineUpdater(
		func(_ *p.Props) update.Updater {
			return mu
		},
	))
	cmd.SetArgs([]string{"--from-file", "/tmp/release.tar.gz"})

	err := cmd.Execute()
	assert.NoError(t, err)
}

// #95: an updater that finds the binary already current replaced nothing,
// and the command used to say "Update complete" with Updated: true.
func TestUpdate_AlreadyCurrentIsReportedAsNoUpdate(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	props := &p.Props{
		FS:      afero.NewMemMapFs(),
		Tool:    p.Tool{Name: "test-tool"},
		Logger:  buf,
		Version: version.NewInfo("v1.0.0", "head", "now"),
	}

	mu := &mockUpdater{latestVersion: "v1.0.0", updateErr: setup.ErrAlreadyCurrent}

	var executed []string

	result, err := update.Update(context.Background(), props, "", false, io.Discard,
		update.WithUpdater(func(_ context.Context, _ *p.Props, _ string, _ bool) (update.Updater, error) { return mu, nil }),
		update.WithExecCommand(exectest.TrackingCommand(&executed)),
	)
	require.NoError(t, err, "already current is not a failure")
	assert.False(t, result.Updated)
	assert.Equal(t, "v1.0.0", result.PreviousVersion)
	assert.Equal(t, "v1.0.0", result.NewVersion)
	assert.False(t, buf.Contains("Update complete"), "nothing completed: %s", buf.String())
	assert.True(t, buf.Contains("already"), buf.String())
	assert.Empty(t, executed, "no config refresh when nothing was replaced")
}

// #96: the config refresh after an update ran `init --skip-login --skip-key`,
// and --skip-login is the GitHub profile's flag alone; a GitLab tool's init
// refused it and the refresh silently never ran. The refresh names no
// profile's flag: init skips every credential wizard off a terminal.
func TestUpdateConfig_NamesNoProfileFlag(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	props := &p.Props{FS: fs, Tool: p.Tool{Name: "test-tool"}, Logger: logger.NewNoop()}
	_ = fs.MkdirAll(setup.GetDefaultConfigDir(fs, "test-tool"), 0o755)

	var executed []string

	update.UpdateConfig(context.Background(), props, "/bin/new-tool", update.WithExecCommand(exectest.TrackingCommand(&executed)))

	require.Len(t, executed, 1)
	assert.NotContains(t, executed[0], "--skip-login")
	assert.Contains(t, executed[0], "--ci")
	assert.Contains(t, executed[0], "--skip-key")
}
