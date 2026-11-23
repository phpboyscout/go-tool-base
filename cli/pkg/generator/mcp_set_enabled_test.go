package generator

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func newSetEnabledFixture(t *testing.T) (*props.Props, string, afero.Fs) {
	t.Helper()

	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop()}
	root := "/work"

	require.NoError(t, fs.MkdirAll(root+"/.gtb", 0o755))
	manifest := "commands:\n" +
		"  - name: post\n" +
		"    description: publish\n" +
		"  - name: secret\n" +
		"    description: secret\n" +
		"    protected: true\n"
	require.NoError(t, afero.WriteFile(fs, root+"/.gtb/manifest.yaml", []byte(manifest), 0o644))

	return p, root, fs
}

func TestSetMCPEnabled_DisableThenEnable(t *testing.T) {
	t.Parallel()

	p, root, fs := newSetEnabledFixture(t)
	g := New(p, &Config{Path: root})

	// Disable -> manifest records false and cmd.go stamps ExcludeFromMCP.
	require.NoError(t, g.SetMCPEnabled(context.Background(), "post", false))

	manifest, _ := afero.ReadFile(fs, root+"/.gtb/manifest.yaml")
	assert.Contains(t, string(manifest), "mcp_enabled: false")

	cmdGo, err := afero.ReadFile(fs, root+"/pkg/cmd/post/cmd.go")
	require.NoError(t, err)
	assert.Contains(t, string(cmdGo), "setup.ExcludeFromMCP(cmd)")
	assert.NotContains(t, string(cmdGo), "setup.IncludeInMCP(cmd)")

	// Enable -> manifest records true and cmd.go swaps to IncludeInMCP.
	require.NoError(t, g.SetMCPEnabled(context.Background(), "post", true))

	manifest, _ = afero.ReadFile(fs, root+"/.gtb/manifest.yaml")
	assert.Contains(t, string(manifest), "mcp_enabled: true")

	cmdGo, err = afero.ReadFile(fs, root+"/pkg/cmd/post/cmd.go")
	require.NoError(t, err)
	assert.Contains(t, string(cmdGo), "setup.IncludeInMCP(cmd)")
	assert.NotContains(t, string(cmdGo), "setup.ExcludeFromMCP(cmd)")
}

func TestSetMCPEnabled_RefusesProtected(t *testing.T) {
	t.Parallel()

	p, root, _ := newSetEnabledFixture(t)
	g := New(p, &Config{Path: root})

	err := g.SetMCPEnabled(context.Background(), "secret", false)
	require.ErrorIs(t, err, ErrCommandProtected)
}

func TestSetMCPEnabled_NotFound(t *testing.T) {
	t.Parallel()

	p, root, _ := newSetEnabledFixture(t)
	g := New(p, &Config{Path: root})

	err := g.SetMCPEnabled(context.Background(), "missing", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing")
}

// A kept cmd.go leaves the decision recorded and fails the edit, with one warning
// that says how to apply it (#174).
func TestEditManifestCommand_KeptFile(t *testing.T) {
	t.Parallel()

	const edited = "package hello\n\n// edited by hand\n"

	mcp := func(enabled bool) func(*Generator) error {
		return func(g *Generator) error { return g.SetMCPEnabled(context.Background(), "hello", enabled) }
	}
	annotate := func(g *Generator) error {
		return g.SetMCPHints(context.Background(), "hello", setup.MCPHints{Title: "Hello"}, false)
	}

	for name, tt := range map[string]struct {
		overwrite OverwriteMode
		ignore    string
		edit      func(*Generator) error
		recorded  string
		want      string
	}{
		"disable": {
			overwrite: OverwriteDeny, edit: mcp(false), recorded: "mcp_enabled: false",
			want: "hello: kept pkg/cmd/hello/cmd.go (not created by gtb, --overwrite deny), so its MCP exposure did not change. " +
				"The manifest records the change. To apply it and lose the edits in that file, run gtb disable mcp hello --overwrite allow.",
		},
		"enable": {
			overwrite: OverwriteDeny, edit: mcp(true), recorded: "mcp_enabled: true",
			want: "run gtb enable mcp hello --overwrite allow.",
		},
		"annotate": {
			overwrite: OverwriteDeny, edit: annotate, recorded: "title: Hello",
			want: "so its MCP annotations did not change",
		},
		"nobody to ask": {
			overwrite: OverwriteAsk, edit: mcp(false), recorded: "mcp_enabled: false",
			want: "kept pkg/cmd/hello/cmd.go (not created by gtb, ",
		},
		"ignored": {
			overwrite: OverwriteDeny, ignore: "pkg/cmd/hello/cmd.go\n", edit: mcp(false), recorded: "mcp_enabled: false",
			want: "hello: kept pkg/cmd/hello/cmd.go (covered by .gtb/ignore), so its MCP exposure did not change. The manifest records the change. " +
				"To apply it, take the file out of .gtb/ignore, then run gtb disable mcp hello --overwrite allow.",
		},
		"ignored outranks allow": {
			overwrite: OverwriteAllow, ignore: "pkg/cmd/hello/cmd.go\n", edit: mcp(false), recorded: "mcp_enabled: false",
			want: "(covered by .gtb/ignore)",
		},
		"allowed": {overwrite: OverwriteAllow, edit: mcp(false), recorded: "mcp_enabled: false"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g, fs, log := newPerimeterTestProject(t, wizardManifest)
			g.props.IO = props.StdIO{Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard}
			g.config.Overwrite = tt.overwrite
			cmdFile := "/work/pkg/cmd/hello/cmd.go"

			require.NoError(t, fs.MkdirAll("/work/pkg/cmd/hello", 0o755))
			require.NoError(t, afero.WriteFile(fs, cmdFile, []byte(edited), 0o644))
			require.NoError(t, afero.WriteFile(fs, "/work/.gtb/ignore", []byte(tt.ignore), 0o644))

			err := tt.edit(g)
			assert.Contains(t, manifestSource(t, fs), tt.recorded)

			src, readErr := afero.ReadFile(fs, cmdFile)
			require.NoError(t, readErr)

			if tt.want == "" {
				require.NoError(t, err)
				assert.NotEqual(t, edited, string(src))
				assert.Equal(t, []string{"overwriting modified file"}, warnings(t, log))

				return
			}

			require.ErrorIs(t, err, ErrCommandFileKept)
			require.ErrorContains(t, err, "hello")
			assert.Equal(t, edited, string(src))
			require.Len(t, warnings(t, log), 1)
			assert.Contains(t, warnings(t, log)[0], tt.want)
		})
	}
}

// Only the cmd.go a targeted edit re-renders is left to that edit's own warning.
func TestLogConflict(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		editing bool
		relPath string
		want    []string
	}{
		"the edited cmd.go":      {editing: true, relPath: "pkg/cmd/hello/cmd.go"},
		"another file of it":     {editing: true, relPath: "pkg/cmd/hello/init.go", want: []string{"kept"}},
		"a cmd.go, regenerating": {relPath: "pkg/cmd/hello/cmd.go", want: []string{"kept"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g, _, log := newPerimeterTestProject(t, wizardManifest)
			g.editingCommand = tt.editing
			g.logConflict(tt.relPath, "kept")

			assert.Equal(t, tt.want, warnings(t, log))
		})
	}
}

func warnings(t *testing.T, log logBuffer) []string {
	t.Helper()

	entries, ok := log.(interface{ Entries() []logger.Entry })
	require.True(t, ok)

	var messages []string

	for _, e := range entries.Entries() {
		if e.Level >= logger.WarnLevel {
			messages = append(messages, e.Message)
		}
	}

	return messages
}

// A cmd.go kept for one command must not stop a later command in the same run
// getting its children re-registered (#167).
func TestSetMCPEnabled_KeptFileDoesNotCostLaterCommandsTheirChildren(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop()}
	root := "/work"

	require.NoError(t, fs.MkdirAll(root+"/.gtb", 0o755))
	require.NoError(t, afero.WriteFile(fs, root+"/go.mod", []byte("module example.com/tool\n\ngo 1.27\n"), 0o644))

	manifest := "commands:\n" +
		"  - name: hook\n" +
		"    description: hooks\n" +
		"  - name: worklist\n" +
		"    description: worklists\n" +
		"    commands:\n" +
		"      - name: list\n" +
		"        description: list them\n"
	require.NoError(t, afero.WriteFile(fs, root+"/.gtb/manifest.yaml", []byte(manifest), 0o644))

	handEdited := "package hook\n\n// registers a child by hand\n"
	require.NoError(t, fs.MkdirAll(root+"/pkg/cmd/hook", 0o755))
	require.NoError(t, afero.WriteFile(fs, root+"/pkg/cmd/hook/cmd.go", []byte(handEdited), 0o644))

	g := New(p, &Config{Path: root, Overwrite: OverwriteDeny})

	require.ErrorIs(t, g.SetMCPEnabled(context.Background(), "hook", false), ErrCommandFileKept)
	require.NoError(t, g.SetMCPEnabled(context.Background(), "worklist", false))

	kept, err := afero.ReadFile(fs, root+"/pkg/cmd/hook/cmd.go")
	require.NoError(t, err)
	assert.Equal(t, handEdited, string(kept))

	worklist, err := afero.ReadFile(fs, root+"/pkg/cmd/worklist/cmd.go")
	require.NoError(t, err)
	assert.Contains(t, string(worklist), "setup.ExcludeFromMCP(cmd)")
	assert.Contains(t, string(worklist), `"example.com/tool/pkg/cmd/worklist/list"`)
	assert.Contains(t, string(worklist), "list.NewCmdList(")
}
