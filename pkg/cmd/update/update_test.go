package update_test

import (
	"context"
	"fmt"
	"os/exec"
	"testing"

	"github.com/phpboyscout/go-tool-base/pkg/cmd/update"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	p "github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/setup"
	"github.com/phpboyscout/go-tool-base/pkg/version"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCmdUpdate(t *testing.T) {
	t.Parallel()

	props := &p.Props{
		Tool: p.Tool{
			Name: "test-tool",
		},
		Logger: logger.NewNoop(),
	}

	cmd := update.NewCmdUpdate(props)
	assert.NotNil(t, cmd)
	assert.Equal(t, "update", cmd.Use)

	// Check flags
	force, _ := cmd.Flags().GetBool("force")
	assert.False(t, force)

	ver, _ := cmd.Flags().GetString("version")
	assert.Equal(t, "", ver)
}

func TestUpdate_SemVerValidation(t *testing.T) {
	t.Parallel()

	props := &p.Props{
		Logger: logger.NewNoop(),
	}

	cmd := update.NewCmdUpdate(props)

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

			err = cmd.RunE(cmd, nil)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "invalid version format")
			} else {
				// It will fail later because we haven't mocked the updater, 
				// but we only care about the semVer validation here.
				if err != nil {
					assert.NotContains(t, err.Error(), "invalid version format")
				}
			}
		})
	}
}

// We need to be able to mock setup.NewUpdater to test Update function properly.
// Since setup.NewUpdater returns a struct pointer (*SelfUpdater), we can't easily mock it
// without an interface or refactoring pkg/setup.
// However, we can test the internal updateConfig logic by mocking the execCommand and osStat variables
// that we just added.

func TestUpdateConfig(t *testing.T) {
	// Not using t.Parallel() because we are modifying package-level variables
	
	oldExec := update.ExportExecCommand
	defer func() {
		update.ExportExecCommand = oldExec
	}()

	var executedCommands []string
	update.ExportExecCommand = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		executedCommands = append(executedCommands, fmt.Sprintf("%s %v", name, arg))
		// Return a command that does nothing
		return exec.Command("true")
	}

	fs := afero.NewMemMapFs()
	props := &p.Props{
		FS: fs,
		Tool: p.Tool{
			Name: "test-tool",
		},
		Logger: logger.NewNoop(),
	}

	t.Run("success_path", func(t *testing.T) {
		executedCommands = nil
		// Setup paths in mem FS
		_ = fs.MkdirAll(setup.GetDefaultConfigDir(fs, "test-tool"), 0755)
		_ = fs.MkdirAll("/etc/test-tool", 0755)

		update.UpdateConfig(context.Background(), props, "/bin/new-tool")
		
		assert.Len(t, executedCommands, 2)
		assert.Contains(t, executedCommands[0], "/bin/new-tool [init --dir")
		assert.Contains(t, executedCommands[1], "/bin/new-tool [init --dir")
	})

	t.Run("skips_when_init_disabled", func(t *testing.T) {
		executedCommands = nil
		props.Tool.Features = p.SetFeatures(p.Disable(p.InitCmd))
		
		update.UpdateConfig(context.Background(), props, "/bin/new-tool")
		assert.Empty(t, executedCommands)
	})

	t.Run("handles_init_error", func(t *testing.T) {
		update.ExportExecCommand = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
			// Return a command that fails
			return exec.Command("false")
		}
		// Ensure paths exist
		_ = fs.MkdirAll(setup.GetDefaultConfigDir(fs, "test-tool"), 0755)
		
		props.Tool.Features = p.SetFeatures(p.Enable(p.InitCmd))
		
		update.UpdateConfig(context.Background(), props, "/bin/new-tool")
		// Should just log a warning and continue
	})
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

func (m *mockUpdater) UpdateFromFile(filePath string) (string, error) {
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
	oldNewUpdater := update.ExportNewUpdater
	oldExec := update.ExportExecCommand
	defer func() {
		update.ExportNewUpdater = oldNewUpdater
		update.ExportExecCommand = oldExec
	}()

	update.ExportExecCommand = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		return exec.Command("true")
	}

	props := &p.Props{
		FS:     afero.NewMemMapFs(),
		Tool:   p.Tool{Name: "test-tool"},
		Logger: logger.NewNoop(),
		Version: func() version.Version {
			return &mockVersion{version: "v1.0.0"}
		}(),
	}

	t.Run("successful_update", func(t *testing.T) {
		mu := &mockUpdater{
			latestVersion: "v1.1.0",
			binPath:       "/tmp/new-bin",
			releaseNotes:  "New features!",
		}
		update.ExportNewUpdater = func(p *p.Props, version string, force bool) (update.Updater, error) {
			return mu, nil
		}

		result, err := update.Update(context.Background(), props, "", false)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.Updated)
		assert.Equal(t, "v1.0.0", result.PreviousVersion)
		assert.Equal(t, "v1.1.0", result.NewVersion)
	})

	t.Run("updater_creation_failure", func(t *testing.T) {
		update.ExportNewUpdater = func(p *p.Props, version string, force bool) (update.Updater, error) {
			return nil, fmt.Errorf("failed to create updater")
		}

		_, err := update.Update(context.Background(), props, "", false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create updater")
	})

	t.Run("update_execution_failure", func(t *testing.T) {
		mu := &mockUpdater{
			updateErr: fmt.Errorf("download failed"),
		}
		update.ExportNewUpdater = func(p *p.Props, version string, force bool) (update.Updater, error) {
			return mu, nil
		}

		_, err := update.Update(context.Background(), props, "", false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "download failed")
	})
}

type mockVersion struct {
	version string
}

func (m *mockVersion) GetVersion() string { return m.version }
func (m *mockVersion) GetCommit() string  { return "head" }
func (m *mockVersion) GetDate() string    { return "now" }
func (m *mockVersion) String() string     { return m.version }
func (m *mockVersion) Compare(other string) int { return version.CompareVersions(m.version, other) }
func (m *mockVersion) IsDevelopment() bool { return false }

func TestNewCmdUpdate_FromFileFlag(t *testing.T) {
	t.Parallel()

	props := &p.Props{
		Tool:   p.Tool{Name: "test-tool"},
		Logger: logger.NewNoop(),
	}

	cmd := update.NewCmdUpdate(props)

	fromFile, err := cmd.Flags().GetString("from-file")
	require.NoError(t, err)
	assert.Equal(t, "", fromFile)
}

func TestNewCmdUpdate_MutualExclusion(t *testing.T) {
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
	oldOfflineUpdater := update.ExportNewOfflineUpdater
	oldExec := update.ExportExecCommand
	defer func() {
		update.ExportNewOfflineUpdater = oldOfflineUpdater
		update.ExportExecCommand = oldExec
	}()

	update.ExportExecCommand = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		return exec.Command("true")
	}

	mu := &mockUpdater{
		fromFileBinPath: "/usr/local/bin/test-tool",
	}
	update.ExportNewOfflineUpdater = func(props *p.Props) update.Updater {
		return mu
	}

	props := &p.Props{
		FS:     afero.NewMemMapFs(),
		Tool:   p.Tool{Name: "test-tool"},
		Logger: logger.NewNoop(),
	}

	cmd := update.NewCmdUpdate(props)
	cmd.SetArgs([]string{"--from-file", "/tmp/release.tar.gz"})

	err := cmd.Execute()
	assert.NoError(t, err)
}

