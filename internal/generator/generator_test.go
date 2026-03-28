package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

func TestGenerateAssetFiles_SkipExistingConfig(t *testing.T) {
	// Setup
	fs := afero.NewMemMapFs()
	l := logger.NewNoop()
	p := &props.Props{
		FS:     fs,
		Logger: l,
	}

	cfg := &Config{
		Name:       "test-cmd",
		WithAssets: true,
	}

	g := New(p, cfg)

	// Pre-create the config file
	cmdDir := "test-cmd"
	assetDir := filepath.Join(cmdDir, "assets", "init")
	require.NoError(t, fs.MkdirAll(assetDir, os.ModePerm))

	configPath := filepath.Join(assetDir, "config.yaml")
	originalContent := []byte("original: content")
	require.NoError(t, afero.WriteFile(fs, configPath, originalContent, 0644))

	// Run
	err := g.generateAssetFiles(cmdDir)
	require.NoError(t, err)

	// Verify content hasn't changed
	content, err := afero.ReadFile(fs, configPath)
	require.NoError(t, err)
	assert.Equal(t, string(originalContent), string(content))
}

func TestGenerateAssetFiles_CreateNewConfig(t *testing.T) {
	// Setup
	fs := afero.NewMemMapFs()
	l := logger.NewNoop()
	p := &props.Props{
		FS:     fs,
		Logger: l,
	}

	cfg := &Config{
		Name:       "new-cmd",
		WithAssets: true,
	}

	g := New(p, cfg)

	// Run
	cmdDir := "new-cmd"
	err := g.generateAssetFiles(cmdDir)
	require.NoError(t, err)

	// Verify
	configPath := filepath.Join(cmdDir, "assets", "init", "config.yaml")
	exists, err := afero.Exists(fs, configPath)
	require.NoError(t, err)
	assert.True(t, exists)

	content, err := afero.ReadFile(fs, configPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "new-cmd:")
}

func TestCheckBreakingChanges(t *testing.T) {
	// Setup
	fs := afero.NewMemMapFs()
	// Capture logs? It's harder with charmbracelet/log as it writes to os.Stderr by default.
	// We can use a buffer.
	var buf strings.Builder
	l := logger.NewCharm(&buf)
	p := &props.Props{
		FS:     fs,
		Logger: l,
	}

	g := New(p, &Config{})

	// Mock breaking changes for test
	originalChanges := BreakingChanges
	BreakingChanges = map[string]string{
		"v2.0.0": "Major update v2",
		"v2.1.0": "Minor update v2.1",
	}
	defer func() { BreakingChanges = originalChanges }()

	tests := []struct {
		name        string
		manifestVer string
		cliVer      string
		wantLog     []string
		wantNoLog   []string
	}{
		{
			name:        "CLI newer than manifest, breaking change in between",
			manifestVer: "v1.9.0",
			cliVer:      "v2.0.1",
			wantLog:     []string{"Major update v2"},
			wantNoLog:   []string{"Minor update v2.1"},
		},
		{
			name:        "CLI newer than manifest, multiple breaking changes",
			manifestVer: "v1.9.0",
			cliVer:      "v2.2.0",
			wantLog:     []string{"Major update v2", "Minor update v2.1"},
		},
		{
			name:        "CLI matches manifest, no warnings",
			manifestVer: "v2.0.0",
			cliVer:      "v2.0.0",
			wantNoLog:   []string{"Major update v2", "Minor update v2.1"},
		},
		{
			name:        "Manifest newer than CLI (should not happen in verifyProject but good to test logic)",
			manifestVer: "v2.2.0",
			cliVer:      "v2.0.0",
			wantNoLog:   []string{"Major update v2", "Minor update v2.1"},
		},
		{
			name:        "Exact match on breaking version",
			manifestVer: "v1.9.9",
			cliVer:      "v2.0.0",
			wantLog:     []string{"Major update v2"},
			wantNoLog:   []string{"Minor update v2.1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			g.checkBreakingChanges(tt.manifestVer, tt.cliVer)
			output := buf.String()

			for _, want := range tt.wantLog {
				assert.Contains(t, output, want)
			}
			for _, wantNot := range tt.wantNoLog {
				assert.NotContains(t, output, wantNot)
			}
		})
	}
}
