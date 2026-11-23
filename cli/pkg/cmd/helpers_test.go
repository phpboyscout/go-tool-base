package cmd

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/internal/formtest"
	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

const projectRoot = "/work"

// nobodyTyping is a piped stdin with no accessible mode: not promptable.
func nobodyTyping() props.IO {
	return props.StdIO{Stdin: io.LimitReader(nil, 0), Stdout: io.Discard, Stderr: io.Discard}
}

// newSkeletonProps generates a real skeleton project at projectRoot in an
// in-memory filesystem and returns props whose logger records what was said.
func newSkeletonProps(t *testing.T) (*props.Props, interface{ Contains(string) bool }) {
	t.Helper()

	buf := logger.NewBuffer()
	p := &props.Props{
		FS:      afero.NewMemMapFs(),
		Logger:  buf,
		Version: version.NewInfo("v1.0.0", "", ""),
		IO:      nobodyTyping(),
	}

	require.NoError(t, generator.New(p, &generator.Config{Path: projectRoot, Overwrite: "allow"}).GenerateSkeleton(
		context.Background(),
		generator.SkeletonConfig{Name: "mytool", Repo: "acme/mytool", Host: "github.com", Path: projectRoot},
	))

	return p, buf
}

// newToggleCmd is a command carrying the global --ci flag the toggles honour.
func newToggleCmd(ctx context.Context, ci bool) *cobra.Command {
	c := &cobra.Command{Use: "toggle"}
	c.Flags().Bool("ci", ci, "")
	c.SetContext(ctx)

	return c
}

func featureOn(t *testing.T, p *props.Props, name string) bool {
	t.Helper()

	on, err := generator.New(p, &generator.Config{Path: projectRoot}).FeatureEnabled(name)
	require.NoError(t, err)

	return on
}

func TestResolveProjectPath(t *testing.T) {
	t.Parallel()

	cwd, err := os.Getwd()
	require.NoError(t, err)

	t.Run("explicit path is returned unchanged", func(t *testing.T) {
		t.Parallel()

		p := &props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop()}
		assert.Equal(t, "/somewhere", ResolveProjectPath(p, "/somewhere"))
	})

	t.Run("dot without a workspace stays dot", func(t *testing.T) {
		t.Parallel()

		p := &props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop()}
		assert.Equal(t, ".", ResolveProjectPath(p, "."))
	})

	t.Run("dot resolves to the workspace root holding the manifest", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll(filepath.Join(cwd, ".gtb"), 0o755))
		require.NoError(t, afero.WriteFile(fs, filepath.Join(cwd, ".gtb", "manifest.yaml"), []byte("properties:\n  name: x\n"), 0o644))

		p := &props.Props{FS: fs, Logger: logger.NewNoop()}
		assert.Equal(t, cwd, ResolveProjectPath(p, "."))
	})
}

func TestRunFeatureToggle_ByName(t *testing.T) {
	t.Parallel()

	p, log := newSkeletonProps(t)
	require.True(t, featureOn(t, p, "update"))

	require.NoError(t, RunFeatureToggle(newToggleCmd(context.Background(), false), p, projectRoot, []string{"update"}, false))

	assert.False(t, featureOn(t, p, "update"))
	assert.True(t, log.Contains(`Feature "update" disabled.`))
	assert.True(t, log.Contains("ForcedUpdate"), "disabling update must warn about the self-update check")

	require.NoError(t, RunFeatureToggle(newToggleCmd(context.Background(), false), p, projectRoot, []string{"update"}, false))
	assert.True(t, log.Contains("Already disabled; nothing changed."))

	require.NoError(t, RunFeatureToggle(newToggleCmd(context.Background(), false), p, projectRoot, []string{"update"}, true))
	assert.True(t, featureOn(t, p, "update"))
	assert.True(t, log.Contains(`Feature "update" enabled.`))
}

func TestRunFeatureToggle_Errors(t *testing.T) {
	t.Parallel()

	t.Run("unknown feature name", func(t *testing.T) {
		t.Parallel()

		p, _ := newSkeletonProps(t)
		err := RunFeatureToggle(newToggleCmd(context.Background(), false), p, projectRoot, []string{"no-such-feature"}, true)
		require.Error(t, err)
	})

	t.Run("not a project", func(t *testing.T) {
		t.Parallel()

		p := &props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop()}
		err := RunFeatureToggle(newToggleCmd(context.Background(), false), p, "/empty", []string{"update"}, false)
		require.Error(t, err)
	})

	t.Run("no name and nobody to ask", func(t *testing.T) {
		t.Parallel()

		p, _ := newSkeletonProps(t)
		err := RunFeatureToggle(newToggleCmd(context.Background(), false), p, projectRoot, nil, true)
		require.ErrorIs(t, err, ErrFeatureNameRequired)
	})

	t.Run("no name under the ci flag", func(t *testing.T) {
		t.Parallel()

		p, _ := newSkeletonProps(t)
		p.IO = formtest.AccessibleTTY(formtest.Answers("1", "0"))
		err := RunFeatureToggle(newToggleCmd(context.Background(), true), p, projectRoot, nil, true)
		require.ErrorIs(t, err, ErrFeatureNameRequired)
	})

	t.Run("no name under the ci config key", func(t *testing.T) {
		t.Parallel()

		p, _ := newSkeletonProps(t)
		p.IO = formtest.AccessibleTTY(formtest.Answers("1", "0"))
		p.Config = testutil.StoreFromYAML(t, "ci: true\n")
		err := RunFeatureToggle(&cobra.Command{Use: "noflag"}, p, projectRoot, nil, true)
		require.ErrorIs(t, err, ErrFeatureNameRequired)
	})

	t.Run("picker on a path that is not a project", func(t *testing.T) {
		t.Parallel()

		p := &props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop(), IO: formtest.AccessibleTTY(formtest.Answers("0"))}
		err := RunFeatureToggle(newToggleCmd(context.Background(), false), p, "/empty", nil, true)
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrFeatureNameRequired)
	})
}

func TestRunFeatureToggle_Picker(t *testing.T) {
	t.Parallel()

	t.Run("selected candidate is toggled", func(t *testing.T) {
		t.Parallel()

		p, log := newSkeletonProps(t)

		gen := generator.New(p, &generator.Config{Path: projectRoot})
		candidates, err := featureCandidates(gen, false)
		require.NoError(t, err)
		require.NotEmpty(t, candidates)

		p.IO = formtest.AccessibleTTY(formtest.Answers("1", "0"))
		require.NoError(t, RunFeatureToggle(newToggleCmd(context.Background(), false), p, projectRoot, nil, false))

		assert.False(t, featureOn(t, p, candidates[0]))
		assert.True(t, log.Contains(candidates[0]))
	})

	t.Run("nothing selected changes nothing", func(t *testing.T) {
		t.Parallel()

		p, log := newSkeletonProps(t)
		p.IO = formtest.AccessibleTTY(formtest.Answers("0"))
		require.NoError(t, RunFeatureToggle(newToggleCmd(context.Background(), false), p, projectRoot, nil, false))

		assert.True(t, log.Contains("No features selected; nothing changed."))
		assert.True(t, featureOn(t, p, "update"))
	})

	t.Run("no candidates skips the prompt", func(t *testing.T) {
		t.Parallel()

		p, log := newSkeletonProps(t)
		require.NoError(t, RunFeatureToggle(newToggleCmd(context.Background(), false), p, projectRoot, generator.ToggleableFeatures, true))

		// Stdin holds no answers: reaching the prompt would select nothing,
		// but the empty candidate list must return before it opens.
		p.IO = formtest.AccessibleTTY(formtest.Answers())
		require.NoError(t, RunFeatureToggle(newToggleCmd(context.Background(), false), p, projectRoot, nil, true))
		assert.True(t, log.Contains("No features selected; nothing changed."))
	})

	t.Run("a form that fails is reported", func(t *testing.T) {
		t.Parallel()

		p, _ := newSkeletonProps(t)
		p.IO = formtest.TUI(formtest.Answers())

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := RunFeatureToggle(newToggleCmd(ctx, false), p, projectRoot, nil, false)
		require.Error(t, err)
	})
}

func TestFeatureToggleIsCI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cmd  *cobra.Command
		cfg  string
		want bool
	}{
		{name: "flag set", cmd: newToggleCmd(context.Background(), true), want: true},
		{name: "flag unset, no config", cmd: newToggleCmd(context.Background(), false), want: false},
		{name: "no flag, config ci", cmd: &cobra.Command{}, cfg: "ci: true\n", want: true},
		{name: "no flag, config not ci", cmd: &cobra.Command{}, cfg: "ci: false\n", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &props.Props{}
			if tt.cfg != "" {
				p.Config = testutil.StoreFromYAML(t, tt.cfg)
			}

			assert.Equal(t, tt.want, featureToggleIsCI(tt.cmd, p))
		})
	}
}

func writeCommandManifest(t *testing.T, fs afero.Fs) {
	t.Helper()

	require.NoError(t, fs.MkdirAll(projectRoot+"/.gtb", 0o755))
	manifest := "commands:\n  - name: post\n    description: publish\n"
	require.NoError(t, afero.WriteFile(fs, projectRoot+"/.gtb/manifest.yaml", []byte(manifest), 0o644))
}

func disableMCP(p *props.Props, paths ...string) error {
	return RunMCPCommand(newToggleCmd(context.Background(), false), p, projectRoot, "ask", paths, false)
}

func enableMCP(p *props.Props, paths ...string) error {
	return RunMCPCommand(newToggleCmd(context.Background(), false), p, projectRoot, "ask", paths, true)
}

func TestRunMCPCommand(t *testing.T) {
	t.Parallel()

	t.Run("no command path toggles the mcp feature", func(t *testing.T) {
		t.Parallel()

		p, log := newSkeletonProps(t)
		require.True(t, featureOn(t, p, "mcp"))

		require.NoError(t, disableMCP(p))

		assert.False(t, featureOn(t, p, "mcp"))
		assert.True(t, log.Contains(`Feature "mcp" disabled.`))
	})

	t.Run("command paths are withheld then exposed", func(t *testing.T) {
		t.Parallel()

		buf := logger.NewBuffer()
		fs := afero.NewMemMapFs()
		p := &props.Props{FS: fs, Logger: buf}
		writeCommandManifest(t, fs)

		require.NoError(t, disableMCP(p, "post"))

		cmdGo, err := afero.ReadFile(fs, projectRoot+"/pkg/cmd/post/cmd.go")
		require.NoError(t, err)
		assert.Contains(t, string(cmdGo), "setup.ExcludeFromMCP(cmd)")
		assert.True(t, buf.Contains("1 command(s) withheld from the MCP tool surface"))

		require.NoError(t, enableMCP(p, "post"))

		cmdGo, err = afero.ReadFile(fs, projectRoot+"/pkg/cmd/post/cmd.go")
		require.NoError(t, err)
		assert.NotContains(t, string(cmdGo), "setup.ExcludeFromMCP(cmd)")
		assert.True(t, buf.Contains("1 command(s) exposed on the MCP tool surface"))
	})

	t.Run("a path gtb did not generate goes on the withhold list", func(t *testing.T) {
		t.Parallel()

		p, log := newSkeletonProps(t)

		require.NoError(t, disableMCP(p, "sign", "config/trust"))

		assert.Equal(t, []string{"config/trust", "sign"}, withheldPaths(t, p))
		assert.True(t, log.Contains("2 command(s) withheld from the MCP tool surface"))
		assert.True(t, log.Contains("Withheld by path: sign, config/trust."))
		assert.True(t, log.Contains("`mytool mcp tools`"), "the author is told where to confirm it")

		root, err := afero.ReadFile(p.FS, projectRoot+"/pkg/cmd/root/cmd.go")
		require.NoError(t, err)
		assert.Contains(t, string(root), `Withhold: []string{"config/trust", "sign"}`)

		require.NoError(t, disableMCP(p, "sign"))
		assert.True(t, log.Contains("Already withheld: sign."))

		require.NoError(t, enableMCP(p, "sign"))

		assert.Equal(t, []string{"config/trust"}, withheldPaths(t, p))
		assert.True(t, log.Contains("1 command(s) exposed on the MCP tool surface"))
	})

	t.Run("the withhold list outlives the mcp feature", func(t *testing.T) {
		t.Parallel()

		p, _ := newSkeletonProps(t)

		require.NoError(t, disableMCP(p, "sign"))
		require.NoError(t, disableMCP(p))
		require.False(t, featureOn(t, p, "mcp"))
		require.NoError(t, enableMCP(p))

		assert.Equal(t, []string{"sign"}, withheldPaths(t, p), "turning MCP back on must not re-expose sign")

		root, err := afero.ReadFile(p.FS, projectRoot+"/pkg/cmd/root/cmd.go")
		require.NoError(t, err)
		assert.Contains(t, string(root), `Withhold: []string{"sign"}`)
	})

	t.Run("exposing a path that is neither generated nor listed says so and changes nothing", func(t *testing.T) {
		t.Parallel()

		p, log := newSkeletonProps(t)

		before, err := afero.ReadFile(p.FS, projectRoot+"/.gtb/manifest.yaml")
		require.NoError(t, err)

		require.NoError(t, enableMCP(p, "missing"))

		after, err := afero.ReadFile(p.FS, projectRoot+"/.gtb/manifest.yaml")
		require.NoError(t, err)
		assert.Equal(t, string(before), string(after))
		assert.True(t, log.Contains("Already exposed, nothing to change: missing."))
		assert.False(t, log.Contains("command(s) exposed on"), "nothing changed, so nothing is counted")
	})

	t.Run("a path under a withheld command is refused on enable", func(t *testing.T) {
		t.Parallel()

		p, _ := newSkeletonProps(t)

		require.NoError(t, disableMCP(p, "keys"))

		err := enableMCP(p, "keys/publish")
		require.ErrorIs(t, err, generator.ErrMCPPathStillWithheld)
		assert.Equal(t, []string{"keys"}, withheldPaths(t, p))
	})

	t.Run("a malformed path is refused before anything is written", func(t *testing.T) {
		t.Parallel()

		p, _ := newSkeletonProps(t)

		err := disableMCP(p, "sign", "Config/Trust")
		require.ErrorIs(t, err, generator.ErrInvalidInput)
		assert.Empty(t, withheldPaths(t, p))
	})

	// The several-paths call hud made (#167), at the overwrite mode the
	// command really uses.
	t.Run("a kept cmd.go does not cost later commands their children", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		log := logger.NewBuffer()
		p := &props.Props{FS: fs, Logger: log, IO: nobodyTyping()}

		require.NoError(t, fs.MkdirAll(projectRoot+"/.gtb", 0o755))
		require.NoError(t, afero.WriteFile(fs, projectRoot+"/go.mod", []byte("module example.com/tool\n\ngo 1.27\n"), 0o644))

		manifest := "commands:\n" +
			"  - name: hook\n" +
			"    description: hooks\n" +
			"  - name: worklist\n" +
			"    description: worklists\n" +
			"    commands:\n" +
			"      - name: list\n" +
			"        description: list them\n"
		require.NoError(t, afero.WriteFile(fs, projectRoot+"/.gtb/manifest.yaml", []byte(manifest), 0o644))

		handEdited := "package hook\n\n// registers a child by hand\n"
		require.NoError(t, fs.MkdirAll(projectRoot+"/pkg/cmd/hook", 0o755))
		require.NoError(t, afero.WriteFile(fs, projectRoot+"/pkg/cmd/hook/cmd.go", []byte(handEdited), 0o644))

		run := func(overwrite string) error {
			return RunMCPCommand(newToggleCmd(context.Background(), true), p, projectRoot, overwrite, []string{"hook", "worklist"}, false)
		}

		require.ErrorIs(t, run("ask"), generator.ErrCommandFileKept)
		assert.True(t, log.Contains("1 command(s) withheld"), "the summary counts the commands written")

		kept, err := afero.ReadFile(fs, projectRoot+"/pkg/cmd/hook/cmd.go")
		require.NoError(t, err)
		assert.Equal(t, handEdited, string(kept))

		worklist, err := afero.ReadFile(fs, projectRoot+"/pkg/cmd/worklist/cmd.go")
		require.NoError(t, err)
		assert.Contains(t, string(worklist), "setup.ExcludeFromMCP(cmd)")
		assert.Contains(t, string(worklist), "list.NewCmdList(")

		require.NoError(t, run("allow"))

		replaced, err := afero.ReadFile(fs, projectRoot+"/pkg/cmd/hook/cmd.go")
		require.NoError(t, err)
		assert.Contains(t, string(replaced), "setup.ExcludeFromMCP(cmd)")
		assert.True(t, log.Contains("2 command(s) withheld"))
	})

	t.Run("a summary with nothing written is not printed", func(t *testing.T) {
		t.Parallel()

		log := logger.NewBuffer()
		reportMCPExposure(&props.Props{Logger: log}, generator.MCPExposureReport{Kept: []string{"hook"}}, false)
		assert.Empty(t, log.Entries())
	})

	t.Run("overwrite is checked, and needs a command path", func(t *testing.T) {
		t.Parallel()

		p, _ := newSkeletonProps(t)
		cmd := newToggleCmd(context.Background(), true)

		require.ErrorIs(t, RunMCPCommand(cmd, p, projectRoot, "sometimes", []string{"post"}, false), generator.ErrInvalidOverwriteMode)

		var overwrite string

		cmd.Flags().StringVar(&overwrite, "overwrite", "ask", "")
		require.NoError(t, cmd.Flags().Set("overwrite", "allow"))
		require.ErrorIs(t, RunMCPCommand(cmd, p, projectRoot, overwrite, nil, false), ErrOverwriteWithoutPaths)
		assert.True(t, featureOn(t, p, "mcp"), "nothing was toggled")
	})
}

func withheldPaths(t *testing.T, p *props.Props) []string {
	t.Helper()

	m, err := generator.New(p, &generator.Config{Path: projectRoot}).LoadManifest()
	require.NoError(t, err)

	return m.Properties.MCP.Withhold
}

func TestConfirmRemoteTemplate_SkipsThePrompt(t *testing.T) {
	t.Parallel()

	git := generator.TemplateSource{Type: generator.TemplateSourceGit, Location: "https://gitlab.com/acme/tmpl", Ref: "v1"}

	tests := []struct {
		name     string
		ts       generator.TemplateSource
		ci       bool
		io       props.IO
		wantWarn bool
	}{
		{name: "local source", ts: generator.TemplateSource{Type: generator.TemplateSourceLocal, Location: "/tmpl"}, io: nobodyTyping()},
		{name: "git under ci", ts: git, ci: true, io: formtest.AccessibleTTY(formtest.Answers("n")), wantWarn: true},
		{name: "git with nobody to ask", ts: git, io: nobodyTyping(), wantWarn: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			buf := logger.NewBuffer()
			p := &props.Props{Logger: buf, IO: tt.io}

			require.NoError(t, ConfirmRemoteTemplate(context.Background(), p, tt.ci, tt.ts))
			assert.Equal(t, tt.wantWarn, buf.Contains("trusting remote template source"))
		})
	}
}

func TestConfirmRemoteTemplate_NonInteractiveEnv(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	buf := logger.NewBuffer()
	p := &props.Props{Logger: buf, IO: formtest.AccessibleTTY(formtest.Answers("n"))}
	ts := generator.TemplateSource{Type: generator.TemplateSourceGit, Location: "https://gitlab.com/acme/tmpl"}

	require.NoError(t, ConfirmRemoteTemplate(context.Background(), p, false, ts))
	assert.True(t, buf.Contains("trusting remote template source"))
}

func TestConfirmRemoteTemplate_Prompt(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "")

	ts := generator.TemplateSource{Type: generator.TemplateSourceGit, Location: "https://gitlab.com/acme/tmpl"}

	t.Run("accepted", func(t *testing.T) {
		p := &props.Props{Logger: logger.NewNoop(), IO: formtest.AccessibleTTY(formtest.Answers("y"))}
		require.NoError(t, ConfirmRemoteTemplate(context.Background(), p, false, ts))
	})

	t.Run("declined", func(t *testing.T) {
		p := &props.Props{Logger: logger.NewNoop(), IO: formtest.AccessibleTTY(formtest.Answers("n"))}
		err := ConfirmRemoteTemplate(context.Background(), p, false, ts)
		require.ErrorIs(t, err, ErrRemoteTemplateDeclined)
		assert.Contains(t, err.Error(), ts.Location)
	})

	t.Run("form failure", func(t *testing.T) {
		p := &props.Props{Logger: logger.NewNoop(), IO: formtest.TUI(formtest.Answers())}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := ConfirmRemoteTemplate(ctx, p, false, ts)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "remote template confirmation failed")
	})
}

func TestRemoteTemplateWarning_NamesSourceAndRef(t *testing.T) {
	t.Parallel()

	pinned := remoteTemplateWarning(generator.TemplateSource{Location: "https://gitlab.com/acme/tmpl", Ref: "v2"})
	assert.Contains(t, pinned, "https://gitlab.com/acme/tmpl @ v2")

	unpinned := remoteTemplateWarning(generator.TemplateSource{Location: "https://gitlab.com/acme/tmpl"})
	assert.Contains(t, unpinned, "@ (default branch)")
}
