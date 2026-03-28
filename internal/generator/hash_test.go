package generator

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/phpboyscout/go-tool-base/internal/generator/templates"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/version"
)

func TestHashUpdateOnRegeneration(t *testing.T) {
	// 1. Setup MemFS
	fs := afero.NewMemMapFs()
	// Use a buffer for logs or discard
	l := logger.NewNoop()
	p := &props.Props{
		FS:      fs,
		Logger:  l,
		Version: version.NewInfo("v1.0.0", "", ""),
	}

	projectName := "test-project"
	cmdName := "test-cmd"
	cmdPkg := "test_cmd"

	// 2. Initial Content Generation (Simulated)
	// Create manifest with a known hash
	// We first need to generate CONTENT to know its hash.
	data := templates.CommandData{
		Package:    cmdPkg,
		PascalName: "TestCmd",
		Name:       cmdName,
	}
	regFile := templates.CommandRegistration(data)
	content := []byte(regFile.GoString())

	initialHash := CalculateHash(content)

	// Create manifest
	manifest := Manifest{
		Properties: ManifestProperties{
			Name: "test-project",
		},
		Version: ManifestVersion{
			GoToolBase: "v1.0.0",
		},
		Commands: []ManifestCommand{
			{
				Name:   cmdName,
				Hashes: map[string]string{"cmd.go": initialHash},
			},
		},
	}

	manifestBytes, err := yaml.Marshal(manifest)
	require.NoError(t, err)

	require.NoError(t, fs.MkdirAll(".gtb", 0755))
	require.NoError(t, afero.WriteFile(fs, ".gtb/manifest.yaml", manifestBytes, 0644))
	require.NoError(t, afero.WriteFile(fs, "go.mod", []byte("module github.com/test/project\n\ngo 1.22\n"), 0644))

	// Write the file to disk matchiing the hash
	cmdDir := filepath.Join("pkg", "cmd", cmdName)
	require.NoError(t, fs.MkdirAll(cmdDir, 0755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(cmdDir, "cmd.go"), content, 0644))
	// Dummy main.go
	require.NoError(t, afero.WriteFile(fs, filepath.Join(cmdDir, "main.go"), []byte("package main"), 0644))

	// 3. Setup Generator Logic
	// We want to simulate "RegenerateProject" but strictly for this command.
	// But to change the hash, we need to CHANGE the manifest so that generation produces DIFFERENT content.
	// We add an alias "tc".

	// Modify manifest on disk
	manifest.Commands[0].Aliases = []string{"tc"}
	updatedManifestBytes, err := yaml.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, ".gtb/manifest.yaml", updatedManifestBytes, 0644))

	// 4. Run Regeneration
	cfg := &Config{
		Path: ".",         // Current directory is root of memfs
		Name: projectName, // Project name
	}
	g := New(p, cfg)

	// We can call RegenerateProject directly.
	// But verifyProject might fail on version checks if we are not careful?
	// verifyProject reads manifest version. We set it to v1.0.0. Props version is v1.0.0. Should pass.

	err = g.RegenerateProject(context.Background())
	require.NoError(t, err)

	// 5. Verify Results

	// Check Manifest Hash updated
	m, err := g.loadManifest()
	require.NoError(t, err)

	newHash := m.Commands[0].Hashes["cmd.go"]
	assert.NotEqual(t, initialHash, newHash, "Hash should have changed")

	// Check file content updated
	updatedContent, err := afero.ReadFile(fs, filepath.Join(cmdDir, "cmd.go"))
	require.NoError(t, err)

	assert.NotEqual(t, content, updatedContent, "File content should have changed")
	assert.Contains(t, string(updatedContent), "\"tc\"", "File should contain alias")

	// 6. Verify Correct Hash Stored
	// Re-calculate hash of updated content
	calculatedNewHash := CalculateHash(updatedContent)
	assert.Equal(t, calculatedNewHash, newHash, "Stored hash should match new content hash")
}

func TestVerifyHash(t *testing.T) {
	// Prevent interactive prompts from hanging the test
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	fs := afero.NewMemMapFs()
	l := logger.NewNoop()
	p := &props.Props{
		FS:     fs,
		Logger: l,
	}

	cmdName := "test-cmd"
	cmdDir := filepath.Join("pkg", "cmd", cmdName)
	cmdPath := filepath.Join(cmdDir, "cmd.go")

	content := []byte("// original content")
	hashValue := CalculateHash(content)

	tests := []struct {
		name        string
		setup       func()
		force       bool
		wantError   bool
		errContains string
	}{
		{
			name: "No conflict (hashes match)",
			setup: func() {
				require.NoError(t, fs.MkdirAll(cmdDir, 0755))
				require.NoError(t, afero.WriteFile(fs, cmdPath, content, 0644))
				// Manifest matches
				manifestContent := "commands:\n- name: " + cmdName + "\n  hashes:\n    cmd.go: " + hashValue
				require.NoError(t, fs.MkdirAll(".gtb", 0755))
				require.NoError(t, afero.WriteFile(fs, ".gtb/manifest.yaml", []byte(manifestContent), 0644))
			},
			force:     false,
			wantError: false,
		},
		{
			name: "Conflict detected (manual change)",
			setup: func() {
				modifiedContent := []byte("// modified content")
				require.NoError(t, afero.WriteFile(fs, cmdPath, modifiedContent, 0644))
				// Manifest still has original hash
			},
			force:       false,
			wantError:   true,
			errContains: "overwrite skipped by user", // Default behavior without interactive prompt is fail
		},
		{
			name: "Conflict ignored with Force",
			setup: func() {
				// Same as above
			},
			force:     true,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()
			g := New(p, &Config{Path: ".", Name: cmdName, Force: tt.force})
			err := VerifyHash(g, cmdPath)
			if tt.wantError {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
