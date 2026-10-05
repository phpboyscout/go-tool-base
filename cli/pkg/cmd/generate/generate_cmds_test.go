package generate

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/internal/formtest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

func TestNewCmdGenerate_RegistersTheScaffoldingSurface(t *testing.T) {
	t.Parallel()

	cmd := NewCmdGenerate(&props.Props{Logger: logger.NewNoop()})

	var names []string
	for _, c := range cmd.Commands() {
		names = append(names, c.Name())
	}

	assert.ElementsMatch(t, []string{"project", "command", "add-flag", "docs", "man"}, names)

	for _, flag := range []string{"provider", "model", "dry-run"} {
		assert.NotNil(t, cmd.PersistentFlags().Lookup(flag), "persistent flag %q", flag)
	}
}

func TestSharedFlags_Accessors(t *testing.T) {
	t.Parallel()

	var none *SharedFlags
	assert.False(t, none.dryRun())
	assert.Empty(t, none.aiProvider())
	assert.Empty(t, none.aiModel())

	set := &SharedFlags{AIProvider: "claude", AIModel: "opus", DryRun: true}
	assert.True(t, set.dryRun())
	assert.Equal(t, "claude", set.aiProvider())
	assert.Equal(t, "opus", set.aiModel())
}

// commandProjectWithDeploy is commandProject with a generated `deploy`
// command for add-flag and docs to target.
func commandProjectWithDeploy(t *testing.T) (*props.Props, afero.Fs) {
	t.Helper()

	p, fs := commandProject(t)
	require.NoError(t, (&CommandOptions{Name: "deploy", Short: "Deploy things", Parent: "root", Path: commandProjectRoot}).
		Run(context.Background(), p))

	return p, fs
}

func TestNewCmdAddFlag_AddsTheFlagAndRegenerates(t *testing.T) {
	t.Parallel()

	p, fs := commandProjectWithDeploy(t)

	cmd := NewCmdAddFlag(p)
	cmd.SetArgs([]string{"-c", "deploy", "-n", "env", "-t", "string", "-d", "Target environment", "-s", "e", "--path", commandProjectRoot})
	require.NoError(t, cmd.ExecuteContext(context.Background()))

	got := findManifestCommand(t, loadCommandManifest(t, p), "deploy")
	require.Len(t, got.Flags, 1)
	assert.Equal(t, "env", got.Flags[0].Name)
	assert.Equal(t, "e", got.Flags[0].Shorthand)

	src, err := afero.ReadFile(fs, filepath.Join(commandProjectRoot, "pkg/cmd/deploy/cmd.go"))
	require.NoError(t, err)
	assert.Contains(t, string(src), `"env"`)
}

func TestNewCmdAddFlag_InvalidFlagsAreAUsageError(t *testing.T) {
	t.Parallel()

	p, _ := commandProjectWithDeploy(t)

	cmd := NewCmdAddFlag(p)
	cmd.SetArgs([]string{"-c", "deploy", "-n", "env", "-t", "notatype", "--path", commandProjectRoot})
	err := cmd.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, generator.ErrInvalidInput)
}

func TestAddFlagRun_Failures(t *testing.T) {
	t.Parallel()

	t.Run("not a project", func(t *testing.T) {
		t.Parallel()

		p := &props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop()}
		err := (&AddFlagOptions{CommandName: "deploy", FlagName: "env", FlagType: "string", Path: "/nowhere"}).Run(context.Background(), p)
		require.ErrorIs(t, err, generator.ErrNotGoToolBaseProject)
	})

	t.Run("unknown command", func(t *testing.T) {
		t.Parallel()

		p, _ := commandProjectWithDeploy(t)
		err := (&AddFlagOptions{CommandName: "missing", FlagName: "env", FlagType: "string", Path: commandProjectRoot}).Run(context.Background(), p)
		require.Error(t, err)
	})

	t.Run("manifest cannot be written", func(t *testing.T) {
		t.Parallel()

		p, fs := commandProjectWithDeploy(t)
		p.FS = afero.NewReadOnlyFs(fs)

		err := (&AddFlagOptions{CommandName: "deploy", FlagName: "env", FlagType: "string", Path: commandProjectRoot}).Run(context.Background(), p)
		require.Error(t, err)

		got := findManifestCommand(t, loadCommandManifest(t, p), "deploy")
		assert.Empty(t, got.Flags, "nothing was recorded")
	})

	t.Run("command cannot be regenerated", func(t *testing.T) {
		t.Parallel()

		p, fs := commandProjectWithDeploy(t)
		p.FS = refuseWritesTo{Fs: fs, name: filepath.Join(commandProjectRoot, "pkg/cmd/deploy/cmd.go")}

		err := (&AddFlagOptions{CommandName: "deploy", FlagName: "env", FlagType: "string", Path: commandProjectRoot}).Run(context.Background(), p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to regenerate command files")
	})
}

// refuseWritesTo fails every write to one file and passes everything else
// through.
type refuseWritesTo struct {
	afero.Fs
	name string
}

func (r refuseWritesTo) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if filepath.Clean(name) == r.name && flag&(os.O_WRONLY|os.O_RDWR) != 0 {
		return nil, os.ErrPermission
	}

	return r.Fs.OpenFile(name, flag, perm)
}

func (r refuseWritesTo) Create(name string) (afero.File, error) {
	if filepath.Clean(name) == r.name {
		return nil, os.ErrPermission
	}

	return r.Fs.Create(name)
}

func TestAddFlagPrompt_AccessibleRepromptsEmptyNames(t *testing.T) {
	t.Parallel()

	p := pipedAccessible(formtest.Answers("", "deploy", "", "env", "2", "Verbose", "v", "y", "/proj"))

	o := &AddFlagOptions{}
	require.NoError(t, o.ValidateOrPrompt(context.Background(), p))

	assert.Equal(t, "deploy", o.CommandName)
	assert.Equal(t, "env", o.FlagName)
	assert.Equal(t, "bool", o.FlagType)
	assert.True(t, o.Persistent)
	assert.Equal(t, "/proj", o.Path)
}

func TestNewCmdDocs_WritesBoilerplateDocs(t *testing.T) {
	t.Parallel()

	t.Run("a command", func(t *testing.T) {
		t.Parallel()

		p, fs := commandProjectWithDeploy(t)
		doc := filepath.Join(commandProjectRoot, "docs/commands/deploy/index.md")
		require.NoError(t, fs.Remove(doc))

		cmd := NewCmdDocs(p, &SharedFlags{})
		cmd.SetArgs([]string{"--command", "deploy", "--path", commandProjectRoot, "--agentless"})
		require.NoError(t, cmd.ExecuteContext(context.Background()))

		content, err := afero.ReadFile(fs, doc)
		require.NoError(t, err)
		assert.Contains(t, string(content), "Deploy things")
	})

	t.Run("the deprecated source flag", func(t *testing.T) {
		t.Parallel()

		p, fs := commandProjectWithDeploy(t)
		doc := filepath.Join(commandProjectRoot, "docs/commands/deploy/index.md")
		require.NoError(t, fs.Remove(doc))

		cmd := NewCmdDocs(p, &SharedFlags{})
		cmd.SetArgs([]string{"--source", "deploy", "--path", commandProjectRoot, "--agentless"})
		require.NoError(t, cmd.ExecuteContext(context.Background()))

		exists, err := afero.Exists(fs, doc)
		require.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("a package", func(t *testing.T) {
		t.Parallel()

		p, fs := commandProjectWithDeploy(t)
		require.NoError(t, afero.WriteFile(fs, filepath.Join(commandProjectRoot, "pkg/thing/thing.go"),
			[]byte("// Package thing does things.\npackage thing\n\n// Do does it.\nfunc Do() {}\n"), 0o644))

		before := docFiles(t, fs)

		cmd := NewCmdDocs(p, &SharedFlags{})
		cmd.SetArgs([]string{"--package", "pkg/thing", "--path", commandProjectRoot, "--agentless"})
		require.NoError(t, cmd.ExecuteContext(context.Background()))

		assert.Greater(t, len(docFiles(t, fs)), len(before), "a page was written for the package")
	})
}

func docFiles(t *testing.T, fs afero.Fs) []string {
	t.Helper()

	var files []string

	_ = afero.Walk(fs, filepath.Join(commandProjectRoot, "docs"), func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			files = append(files, path)
		}

		return nil
	})

	return files
}

func manRoot(t *testing.T, shared *SharedFlags, args ...string) (*bytes.Buffer, error) {
	t.Helper()

	p := &props.Props{Logger: logger.NewNoop(), Version: version.Info{Version: "9.9.9"}, Tool: props.Tool{Name: "demo"}}
	root := &cobra.Command{Use: "demo"}
	root.AddCommand(NewCmdMan(p, shared))

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(append([]string{"man"}, args...))

	return &buf, root.Execute()
}

func TestManOptions_Run_Branches(t *testing.T) {
	t.Parallel()

	t.Run("an empty section falls back to 1", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		_, err := manRoot(t, &SharedFlags{}, "--dir", dir, "--section", "", "--source", "src", "--date", "2026-06-21")
		require.NoError(t, err)
		assert.FileExists(t, filepath.Join(dir, "man1", "demo.1"))
	})

	t.Run("dry run lists and writes nothing", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		out, err := manRoot(t, &SharedFlags{DryRun: true}, "--dir", dir)
		require.NoError(t, err)
		assert.Contains(t, out.String(), filepath.Join(dir, "man1", "demo.1"))
		assert.NoDirExists(t, filepath.Join(dir, "man1"))
	})

	t.Run("a bad date is refused", func(t *testing.T) {
		t.Parallel()

		_, err := manRoot(t, &SharedFlags{}, "--dir", t.TempDir(), "--date", "yesterday")
		require.ErrorContains(t, err, "invalid --date")
	})

	t.Run("an unwritable directory fails", func(t *testing.T) {
		t.Parallel()

		file := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(file, nil, 0o600))

		_, err := manRoot(t, &SharedFlags{}, "--dir", file)
		require.Error(t, err)
	})
}
