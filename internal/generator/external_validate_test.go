package generator_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
)

// sigillumAttach is the real-world declarative attachment this feature exists to
// support: the sign/keys command tree from go/signing-cli, each constructor
// taking the narrow logger seam and returning *cobra.Command (wrap: true).
func sigillumExternalCommand() generator.ManifestExternalCommand {
	return generator.ManifestExternalCommand{
		Module:  "gitlab.com/phpboyscout/go/signing-cli",
		Version: "v0.1.0",
		Attach: []generator.ManifestExternalAttach{
			{Constructor: "NewCmdSign", Args: []string{"logger"}, Wrap: true},
			{Constructor: "NewCmdKeys", Args: []string{"logger"}, Wrap: true},
		},
	}
}

func TestValidateExternalCommand(t *testing.T) {
	t.Parallel()

	valid := func(mut func(*generator.ManifestExternalCommand)) generator.ManifestExternalCommand {
		ec := sigillumExternalCommand()
		if mut != nil {
			mut(&ec)
		}

		return ec
	}

	tests := []struct {
		name    string
		ec      generator.ManifestExternalCommand
		wantErr bool
	}{
		{name: "sigillum shape", ec: valid(nil), wantErr: false},
		{
			name: "props-style constructor",
			ec: valid(func(ec *generator.ManifestExternalCommand) {
				ec.Attach = []generator.ManifestExternalAttach{
					{Constructor: "NewCmdFoo", Args: []string{"props"}, Wrap: false},
				}
			}),
			wantErr: false,
		},
		{
			name: "zero-arg constructor",
			ec: valid(func(ec *generator.ManifestExternalCommand) {
				ec.Attach = []generator.ManifestExternalAttach{
					{Constructor: "NewCmdBar", Wrap: false},
				}
			}),
			wantErr: false,
		},
		{
			name: "every vocabulary token",
			ec: valid(func(ec *generator.ManifestExternalCommand) {
				ec.Attach = []generator.ManifestExternalAttach{
					{Constructor: "NewCmdAll", Args: []string{"logger", "props", "config", "fs", "version"}},
				}
			}),
			wantErr: false,
		},
		{
			name: "explicit import path + alias",
			ec: valid(func(ec *generator.ManifestExternalCommand) {
				ec.ImportPath = "gitlab.com/phpboyscout/go/signing-cli/cmd"
				ec.Alias = "signingcli"
			}),
			wantErr: false,
		},
		{
			name: "pseudo-version pin",
			ec: valid(func(ec *generator.ManifestExternalCommand) {
				ec.Version = "v0.0.0-20200101000000-abcdef123456"
			}),
			wantErr: false,
		},

		{
			name:    "empty module",
			ec:      valid(func(ec *generator.ManifestExternalCommand) { ec.Module = "" }),
			wantErr: true,
		},
		{
			name:    "module traversal",
			ec:      valid(func(ec *generator.ManifestExternalCommand) { ec.Module = "gitlab.com/../evil" }),
			wantErr: true,
		},
		{
			name:    "module backslash",
			ec:      valid(func(ec *generator.ManifestExternalCommand) { ec.Module = `gitlab.com\phpboyscout` }),
			wantErr: true,
		},
		{
			name:    "module non-ascii",
			ec:      valid(func(ec *generator.ManifestExternalCommand) { ec.Module = "gitlab.com/phpbоyscout/x" }), // cyrillic o
			wantErr: true,
		},
		{
			name:    "missing version",
			ec:      valid(func(ec *generator.ManifestExternalCommand) { ec.Version = "" }),
			wantErr: true,
		},
		{
			name:    "non-semver version",
			ec:      valid(func(ec *generator.ManifestExternalCommand) { ec.Version = "latest" }),
			wantErr: true,
		},
		{
			name:    "version without v prefix",
			ec:      valid(func(ec *generator.ManifestExternalCommand) { ec.Version = "1.2.3" }),
			wantErr: true,
		},
		{
			name:    "version injection",
			ec:      valid(func(ec *generator.ManifestExternalCommand) { ec.Version = "v1.2.3\nrequire evil" }),
			wantErr: true,
		},
		{
			name:    "no attach entries",
			ec:      valid(func(ec *generator.ManifestExternalCommand) { ec.Attach = nil }),
			wantErr: true,
		},
		{
			name: "unexported constructor",
			ec: valid(func(ec *generator.ManifestExternalCommand) {
				ec.Attach = []generator.ManifestExternalAttach{{Constructor: "newCmdSign", Args: []string{"logger"}}}
			}),
			wantErr: true,
		},
		{
			name: "constructor with call syntax",
			ec: valid(func(ec *generator.ManifestExternalCommand) {
				ec.Attach = []generator.ManifestExternalAttach{{Constructor: "NewCmdSign()"}}
			}),
			wantErr: true,
		},
		{
			name: "unknown injection token",
			ec: valid(func(ec *generator.ManifestExternalCommand) {
				ec.Attach = []generator.ManifestExternalAttach{{Constructor: "NewCmdSign", Args: []string{"secrets"}}}
			}),
			wantErr: true,
		},
		{
			name: "bad alias",
			ec: valid(func(ec *generator.ManifestExternalCommand) {
				ec.Alias = "signing-cli" // hyphen is not a Go identifier
			}),
			wantErr: true,
		},
		{
			name: "bad collision name",
			ec: valid(func(ec *generator.ManifestExternalCommand) {
				ec.Attach[0].Name = "Sign" // command names are lowercase-first
			}),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := generator.ValidateExternalCommand(&tc.ec)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, generator.ErrInvalidInput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestValidateManifest_ExternalCommands proves the block is wired into the
// top-level ValidateManifest gate and that a duplicate (module, constructor)
// across entries is rejected.
func TestValidateManifest_ExternalCommands(t *testing.T) {
	t.Parallel()

	base := func() *generator.Manifest {
		return &generator.Manifest{
			Properties: generator.ManifestProperties{
				Name:             "widget",
				ExternalCommands: []generator.ManifestExternalCommand{sigillumExternalCommand()},
			},
		}
	}

	t.Run("valid passes the gate", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, generator.ValidateManifest(base()))
	})

	t.Run("invalid entry fails the gate", func(t *testing.T) {
		t.Parallel()
		m := base()
		m.Properties.ExternalCommands[0].Version = ""
		require.Error(t, generator.ValidateManifest(m))
	})

	t.Run("duplicate module+constructor rejected", func(t *testing.T) {
		t.Parallel()
		m := base()
		dup := sigillumExternalCommand()
		dup.Attach = []generator.ManifestExternalAttach{
			{Constructor: "NewCmdSign", Args: []string{"logger"}, Wrap: true},
		}
		m.Properties.ExternalCommands = append(m.Properties.ExternalCommands, dup)
		require.Error(t, generator.ValidateManifest(m))
	})
}

// TestManifestExternalCommands_RoundTrip proves the block serialises and decodes
// stably, including the adapter flag, and that an omitted block stays omitted.
func TestManifestExternalCommands_RoundTrip(t *testing.T) {
	t.Parallel()

	m := generator.Manifest{
		Properties: generator.ManifestProperties{
			Name:                    "widget",
			ExternalCommands:        []generator.ManifestExternalCommand{sigillumExternalCommand()},
			ExternalCommandsAdapter: true,
		},
	}

	data, err := yaml.Marshal(m)
	require.NoError(t, err)

	out := string(data)
	require.Contains(t, out, "external_commands:")
	require.Contains(t, out, "gitlab.com/phpboyscout/go/signing-cli")
	require.Contains(t, out, "constructor: NewCmdSign")
	require.Contains(t, out, "external_commands_adapter: true")

	var back generator.Manifest
	require.NoError(t, yaml.Unmarshal(data, &back))
	require.Equal(t, m.Properties.ExternalCommands, back.Properties.ExternalCommands)
	require.True(t, back.Properties.ExternalCommandsAdapter)

	// Omitted block stays omitted (omitempty), so existing manifests are
	// unchanged by the new fields.
	bare, err := yaml.Marshal(generator.Manifest{Properties: generator.ManifestProperties{Name: "bare"}})
	require.NoError(t, err)
	require.NotContains(t, string(bare), "external_commands")
}

func TestValidateExternalCommand_LongModuleRejected(t *testing.T) {
	t.Parallel()

	ec := sigillumExternalCommand()
	ec.Module = "gitlab.com/" + strings.Repeat("a", 600)
	require.Error(t, generator.ValidateExternalCommand(&ec))
}
