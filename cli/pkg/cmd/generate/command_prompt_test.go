package generate

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/internal/formtest"
	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

const commandProjectRoot = "/proj"

// commandProject is a minimal generated project on an in-memory FS: a go.mod,
// a root command and a manifest, enough for `generate command` to add to.
func commandProject(t *testing.T) (*props.Props, afero.Fs) {
	t.Helper()

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll(filepath.Join(commandProjectRoot, "pkg/cmd/root"), 0o755))
	require.NoError(t, fs.MkdirAll(filepath.Join(commandProjectRoot, ".gtb"), 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(commandProjectRoot, "go.mod"),
		[]byte("module github.com/test-org/test-project\n"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(commandProjectRoot, "pkg/cmd/root/cmd.go"), []byte(`package root

import (
	"github.com/spf13/cobra"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func NewCmdRoot(props *props.Props) *cobra.Command {
	return &cobra.Command{Use: "test-project"}
}
`), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(commandProjectRoot, ".gtb/manifest.yaml"),
		[]byte("properties:\n  name: test-project\nversion:\n  gtb: v1.0.0\n"), 0o644))

	return &props.Props{FS: fs, Logger: logger.NewNoop(), Config: testutil.StoreFromYAML(t, ""), Version: version.NewInfo("v1.0.0", "", "")}, fs
}

func loadCommandManifest(t *testing.T, p *props.Props) *generator.Manifest {
	t.Helper()

	m, err := generator.New(p, &generator.Config{Path: commandProjectRoot}).LoadManifest()
	require.NoError(t, err)

	return m
}

func findManifestCommand(t *testing.T, m *generator.Manifest, name string) generator.ManifestCommand {
	t.Helper()

	for _, c := range m.Commands {
		if c.Name == name {
			return c
		}
	}

	require.Failf(t, "command not in manifest", "%q", name)

	return generator.ManifestCommand{}
}

func TestNewCmdCommand_TriStateFlagsReachTheManifest(t *testing.T) {
	t.Parallel()

	p, fs := commandProject(t)

	cmd := NewCmdCommand(p, &SharedFlags{})
	cmd.SetArgs([]string{"--name", "deploy", "--short", "Deploy things", "--path", commandProjectRoot,
		"--protected", "--mcp-enabled=false", "--flag", "env:string:Target environment"})
	require.NoError(t, cmd.ExecuteContext(context.Background()))

	exists, err := afero.Exists(fs, filepath.Join(commandProjectRoot, "pkg/cmd/deploy/cmd.go"))
	require.NoError(t, err)
	assert.True(t, exists)

	got := findManifestCommand(t, loadCommandManifest(t, p), "deploy")
	require.NotNil(t, got.Protected)
	assert.True(t, *got.Protected)
	require.NotNil(t, got.MCPEnabled)
	assert.False(t, *got.MCPEnabled)
}

func TestNewCmdCommand_InvalidFlagsAreAUsageError(t *testing.T) {
	t.Parallel()

	p, _ := commandProject(t)

	cmd := NewCmdCommand(p, &SharedFlags{})
	cmd.SetArgs([]string{"--name", "Bad Name", "--path", commandProjectRoot})
	err := cmd.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, generator.ErrInvalidInput)
}

func TestProtectAndUnprotect(t *testing.T) {
	t.Parallel()

	p, _ := commandProject(t)
	require.NoError(t, (&CommandOptions{Name: "deploy", Short: "Deploy", Parent: "root", Path: commandProjectRoot}).
		Run(context.Background(), p))

	protect := NewCmdProtect(p)
	protect.SetArgs([]string{"deploy", "--path", commandProjectRoot})
	require.NoError(t, protect.ExecuteContext(context.Background()))

	got := findManifestCommand(t, loadCommandManifest(t, p), "deploy")
	require.NotNil(t, got.Protected)
	assert.True(t, *got.Protected)

	unprotect := NewCmdUnprotect(p)
	unprotect.SetArgs([]string{"deploy", "--path", commandProjectRoot})
	require.NoError(t, unprotect.ExecuteContext(context.Background()))

	got = findManifestCommand(t, loadCommandManifest(t, p), "deploy")
	require.NotNil(t, got.Protected)
	assert.False(t, *got.Protected)
}

func TestProtectAndUnprotect_UnknownCommandFails(t *testing.T) {
	t.Parallel()

	p, _ := commandProject(t)

	protect := NewCmdProtect(p)
	protect.SetArgs([]string{"missing", "--path", commandProjectRoot})
	require.Error(t, protect.ExecuteContext(context.Background()))

	unprotect := NewCmdUnprotect(p)
	unprotect.SetArgs([]string{"missing", "--path", commandProjectRoot})
	require.Error(t, unprotect.ExecuteContext(context.Background()))
}

func TestCommandValidateNonInteractive_RefusesEachField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts CommandOptions
	}{
		{name: "parent path", opts: CommandOptions{Name: "deploy", Parent: "../etc"}},
		{name: "short description", opts: CommandOptions{Name: "deploy", Parent: "root", Short: "bad\x00desc"}},
		{name: "long description", opts: CommandOptions{Name: "deploy", Parent: "root", Long: "bad\x00desc"}},
		{name: "flag definition", opts: CommandOptions{Name: "deploy", Parent: "root", Flags: []string{"Bad Flag:string"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			o := tt.opts
			require.Error(t, o.ValidateOrPrompt(context.Background(), nobodyTyping()))
		})
	}
}

func TestValidateFlagString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{name: "name only", in: "verbose"},
		{name: "every field", in: "out:string:Output path:false:o:true:x:false"},
		{name: "code default", in: "timeout:duration:Timeout:false:t:false:time.Second:true"},
		{name: "bad name", in: "Bad Name", wantErr: true},
		{name: "bad type", in: "out:notatype", wantErr: true},
		{name: "bad description", in: "out:string:bad\x00desc", wantErr: true},
		{name: "bad shorthand", in: "out:string:Output:false:oo", wantErr: true},
		{name: "bad code default", in: "out:string:Output:false:o:false:os.Exit(1); x:true", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateFlagString(tt.in)
			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
		})
	}
}

func TestCommandValidateOrPrompt_NonInteractiveSyncsOptions(t *testing.T) {
	t.Parallel()

	o := &CommandOptions{Name: "deploy", Parent: "root", Short: "Deploy", WithAssets: true, PersistentPreRun: true,
		PreRun: true, WithInitializer: true, WithConfigValidation: true, Flags: []string{"env:string:Target"}}
	require.NoError(t, o.ValidateOrPrompt(context.Background(), nobodyTyping()))
	assert.Equal(t, []string{"assets", "persistent-pre-run", "pre-run", "initializer", "config-validation"}, o.Options)
}

// TestCommandPrompt_Accessible drives the whole interactive path as line
// prompts: the main page, the AI prompt page and two passes of the flag loop.
func TestCommandPrompt_Accessible(t *testing.T) {
	t.Parallel()

	p := &props.Props{IO: formtest.AccessibleTTY(formtest.Answers(
		"deploy", "Deploy things", "", "d, dep", "", "ExactArgs(1)",
		"1", "3", "0", // options: assets, pre-run, done
		"n", "y", "y", // exclude from MCP, add flags, add prompt
		"do the deploy",
		"env", "1", "Target", "e", "prod", "n", "y", "y",
		"count", "3", "How many", "", "", "y", "n", "n",
	))}

	o := &CommandOptions{Parent: "root", Aliases: []string{"old"}}
	require.NoError(t, o.ValidateOrPrompt(context.Background(), p))

	assert.Equal(t, "deploy", o.Name)
	assert.Equal(t, "Deploy things", o.Short)
	assert.Equal(t, []string{"d", "dep"}, o.Aliases)
	assert.Equal(t, "root", o.Parent)
	assert.Equal(t, "ExactArgs(1)", o.Args)
	assert.True(t, o.WithAssets)
	assert.True(t, o.PreRun)
	assert.False(t, o.PersistentPreRun)
	require.NotNil(t, o.MCPEnabled)
	assert.False(t, *o.MCPEnabled)
	assert.Equal(t, "do the deploy", o.Prompt)
	assert.Equal(t, []string{
		"env:string:Target:false:e:true:prod:false",
		"count:int:How many:true::false::false",
	}, o.Flags)
}

// TestCommandPrompt_AccessibleValidatorsReprompt: an answer a validator
// refuses is asked again rather than accepted.
func TestCommandPrompt_AccessibleValidatorsReprompt(t *testing.T) {
	t.Parallel()

	p := &props.Props{IO: formtest.AccessibleTTY(formtest.Answers(
		"Bad Name", "deploy",
		"", "Deploy things",
		"bad\x00long", "",
		"",
		"../etc", "root",
		"",
		"0",
		"y", "y", "n",
		"do it",
		"", "Bad Flag", "env", "1", "bad\x00desc", "Target", "toolong", "e", "", "n", "n", "n",
	))}

	o := &CommandOptions{}
	require.NoError(t, o.ValidateOrPrompt(context.Background(), p))

	assert.Equal(t, "deploy", o.Name)
	assert.Equal(t, "Deploy things", o.Short)
	assert.Equal(t, "root", o.Parent)
	assert.Nil(t, o.MCPEnabled, "exposed is the default and is not recorded")
	assert.Equal(t, []string{"env:string:Target:false:e:false::false"}, o.Flags)
}
