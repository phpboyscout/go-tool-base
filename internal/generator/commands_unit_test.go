package generator

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/phpboyscout/go-tool-base/internal/generator/templates"
	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

func TestCollectAncestoralPersistentFlags(t *testing.T) {
	g := &Generator{}

	rootCmd := ManifestCommand{
		Name: "root",
		Flags: []ManifestFlag{
			{Name: "verbose", Persistent: true, Description: "Enable verbose output"},
			{Name: "config", Persistent: true, Description: "Config file"},
			{Name: "local", Persistent: false, Description: "Local flag"},
		},
		Commands: []ManifestCommand{
			{
				Name: "child",
				Flags: []ManifestFlag{
					{Name: "debug", Persistent: true, Description: "Enable debug"},
					{Name: "verbose", Persistent: true, Description: "Override verbose"}, // Should be deduped/handled? Code logic: if seen, skip.
					// The code iterates from root down?
					// Wait, the code iterates `pathParts`.
					// `collectAncestoralPersistentFlags(commands []ManifestCommand, pathParts []string)`
					// It starts at `current = commands` (root list)
					// For each part in pathParts:
					//   Find command matching part
					//   Add its persistent flags (if not seen)
					//   Descend
					// So it collects from Root -> Child -> ... -> Parent of Target?
					// No, `pathParts` are the ancestors.
				},
				Commands: []ManifestCommand{
					{
						Name: "grandchild",
					},
				},
			},
		},
	}

	// We need to simulate the "commands" slice passed to the function.
	// Usually this is the full manifest.Commands list.
	manifestCommands := []ManifestCommand{rootCmd}

	tests := []struct {
		name      string
		pathParts []string
		expected  []string // Just checking names for simplicity
	}{
		{
			name:      "Root only path",
			pathParts: []string{"root"},
			expected:  []string{"verbose", "config"},
		},
		{
			name:      "Path to child",
			pathParts: []string{"root", "child"},
			expected:  []string{"verbose", "config", "debug"},
		},
		{
			name:      "Path with non-existent node (should stop)",
			pathParts: []string{"root", "non-existent"},
			expected:  []string{"verbose", "config"},
		},
		{
			name:      "Empty path",
			pathParts: []string{},
			expected:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flags := g.collectAncestoralPersistentFlags(manifestCommands, tt.pathParts)
			var names []string
			for _, f := range flags {
				names = append(names, f.Name)
			}
			assert.Equal(t, tt.expected, names)
		})
	}
}

func TestHandleDocumentationGeneration_Fallback(t *testing.T) {
	fs := afero.NewMemMapFs()
	l := logger.NewNoop()
	sl := logger.NewNoop()
	conf := config.NewFilesContainer(sl, fs)

	p := &props.Props{
		FS:     fs,
		Logger: l,
		Config: conf,
	}

	g := &Generator{
		props: p,
		config: &Config{
			Path: "/work",
			Name: "mycmd",
		},
	}

	cmdDir := "/work/pkg/cmd/mycmd"
	_ = fs.MkdirAll(cmdDir, 0755)

	data := templates.CommandData{
		Name: "mycmd",
	}

	// This should fail GenerateDocs (missing source) and fallback to boilerplate
	g.handleDocumentationGeneration(context.Background(), data, cmdDir)

	exists, _ := afero.Exists(fs, "/work/docs/commands/mycmd/index.md")
	assert.True(t, exists)
}

func TestCheckProtection(t *testing.T) {
	fs := afero.NewMemMapFs()
	l := logger.NewNoop()
	sl := logger.NewNoop()
	conf := config.NewFilesContainer(sl, fs)

	// Create a manifest with one protected command and one unprotected command
	manifestPath := "/work/.gtb/manifest.yaml"
	_ = fs.MkdirAll("/work/.gtb", 0755)

	trueVal := true
	manifestLogic := Manifest{
		Version: ManifestVersion{GoToolBase: "v1"},
		Commands: []ManifestCommand{
			{
				Name:      "protected-cmd",
				Protected: &trueVal,
			},
			{
				Name: "unprotected-cmd",
				// Protected is nil or false
			},
		},
	}

	data, _ := yaml.Marshal(manifestLogic)
	_ = afero.WriteFile(fs, manifestPath, data, 0644)

	p := &props.Props{
		FS:     fs,
		Logger: l,
		Config: conf,
	}

	tests := []struct {
		name          string
		cmdName       string
		configProtect *bool // nil, true, or false
		wantErr       bool
		errContains   string
	}{
		{
			name:          "Protected command, config=nil -> Error",
			cmdName:       "protected-cmd",
			configProtect: nil, // Default behavior checks protection
			wantErr:       true,
			errContains:   "command is protected",
		},
		{
			name:          "Protected command, config=true -> Error (already protected)",
			cmdName:       "protected-cmd",
			configProtect: &trueVal,
			wantErr:       true,
			errContains:   "already protected",
		},
		{
			name:          "Protected command, config=false -> OK (unprotecting)",
			cmdName:       "protected-cmd",
			configProtect: func() *bool { b := false; return &b }(),
			wantErr:       false,
		},
		{
			name:          "Unprotected command, config=nil -> OK",
			cmdName:       "unprotected-cmd",
			configProtect: nil,
			wantErr:       false,
		},
		{
			name:          "New command (not in manifest), config=nil -> OK",
			cmdName:       "new-cmd",
			configProtect: nil,
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &Generator{
				props: p,
				config: &Config{
					Path:      "/work",
					Name:      tt.cmdName,
					Protected: tt.configProtect,
				},
			}

			err := g.checkProtection()
			if tt.wantErr {
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

func TestPrepareAndVerify(t *testing.T) {
	fs := afero.NewMemMapFs()
	l := logger.NewNoop()
	sl := logger.NewNoop()
	conf := config.NewFilesContainer(sl, fs)

	// Setup: root command exists
	_ = fs.MkdirAll("/work/pkg/cmd", 0755)

	p := &props.Props{
		FS:     fs,
		Logger: l,
		Config: conf,
	}

	t.Run("Root command name -> Error", func(t *testing.T) {
		g := &Generator{
			props: p,
			config: &Config{
				Path: "/work",
				Name: "root",
			},
		}
		_, err := g.prepareAndVerify()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot create a command named 'root'")
	})

	t.Run("Valid command -> OK", func(t *testing.T) {
		g := &Generator{
			props: p,
			config: &Config{
				Path: "/work",
				Name: "new-cmd",
			},
		}
		dir, err := g.prepareAndVerify()
		require.NoError(t, err)
		assert.Equal(t, "/work/pkg/cmd/new-cmd", dir)
	})

	t.Run("Protected command -> Error (ErrCommandProtected)", func(t *testing.T) {
		// Mock manifest with protected command
		manifestPath := "/work/.gtb/manifest.yaml"
		_ = fs.MkdirAll("/work/.gtb", 0755)

		trueVal := true
		manifestLogic := Manifest{
			Version: ManifestVersion{GoToolBase: "v1"},
			Commands: []ManifestCommand{
				{
					Name:      "protected-cmd",
					Protected: &trueVal,
				},
			},
		}
		data, _ := yaml.Marshal(manifestLogic)
		_ = afero.WriteFile(fs, manifestPath, data, 0644)

		g := &Generator{
			props: p,
			config: &Config{
				Path: "/work",
				Name: "protected-cmd",
			},
		}
		_, err := g.prepareAndVerify()
		assert.ErrorIs(t, err, ErrCommandProtected)
	})
}
