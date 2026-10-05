package generate

import (
	"bytes"
	"context"
	"testing"

	"charm.land/huh/v2"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	icmd "gitlab.com/phpboyscout/go-tool-base/cli/pkg/cmd"
	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/internal/formtest"
	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestSkeletonValidateFields_RefusesEachField walks the flag-path validators
// one bad field at a time, from a baseline that passes.
func TestSkeletonValidateFields_RefusesEachField(t *testing.T) {
	t.Parallel()

	baseline := func() SkeletonOptions { return SkeletonOptions{Name: "mytool", Repo: "org/mytool"} }
	require.NoError(t, func() error { o := baseline(); return o.validateFields() }())

	tests := []struct {
		name    string
		breakIt func(o *SkeletonOptions)
	}{
		{name: "repository", breakIt: func(o *SkeletonOptions) { o.Repo = "org//mytool" }},
		{name: "host", breakIt: func(o *SkeletonOptions) { o.Host = "bad host" }},
		{name: "credential forge", breakIt: func(o *SkeletonOptions) { o.ForgeCredentials = []string{"nope"} }},
		{name: "feature", breakIt: func(o *SkeletonOptions) { o.Features = []string{"bogus"} }},
		{name: "env prefix", breakIt: func(o *SkeletonOptions) { o.EnvPrefix = "1FOO" }},
		{name: "mcp mode", breakIt: func(o *SkeletonOptions) { o.MCPMode = "bogus" }},
		{name: "signing key source", breakIt: func(o *SkeletonOptions) { o.Signing, o.SigningKeySource = true, "nope" }},
		{name: "signing backend", breakIt: func(o *SkeletonOptions) { o.Signing, o.SigningBackend = true, "nope" }},
		{name: "slack team", breakIt: func(o *SkeletonOptions) {
			o.HelpType, o.SlackChannel, o.SlackTeam = "slack", "#help", "bad\x00team"
		}},
		{name: "description", breakIt: func(o *SkeletonOptions) { o.Description = "bad\x00desc" }},
		{name: "slack channel", breakIt: func(o *SkeletonOptions) { o.SlackChannel = "bad\x00channel" }},
		{name: "signing kms region", breakIt: func(o *SkeletonOptions) { o.Signing, o.SigningKMSRegion = true, "bad region!" }},
		{name: "signing key id", breakIt: func(o *SkeletonOptions) { o.Signing, o.SigningKeyID = true, "bad\x00id" }},
		{name: "teams channel", breakIt: func(o *SkeletonOptions) { o.TeamsChannel = "bad\x00channel" }},
		{name: "teams team", breakIt: func(o *SkeletonOptions) {
			o.HelpType, o.TeamsChannel, o.TeamsTeam = "teams", "Support", "bad\x00team"
		}},
		{name: "module path for an unhosted project", breakIt: func(o *SkeletonOptions) {
			o.NoForge, o.Repo, o.Module = true, "", "bad module"
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			o := baseline()
			tt.breakIt(&o)
			require.Error(t, o.validateFields())
		})
	}
}

func TestIsCIEnv(t *testing.T) {
	t.Parallel()

	assert.False(t, isCIEnv(&props.Props{}), "no config is not CI")
	assert.False(t, isCIEnv(&props.Props{Config: testutil.StoreFromYAML(t, "")}))
	assert.True(t, isCIEnv(&props.Props{Config: testutil.StoreFromYAML(t, "ci: true\n")}))
}

func TestResolveTemplateSources(t *testing.T) {
	t.Parallel()

	const remote = "https://git.invalid/acme/templates.git@v1.2.0"

	t.Run("none", func(t *testing.T) {
		t.Parallel()

		got, err := (&SkeletonOptions{}).resolveTemplateSources(context.Background(), &props.Props{})
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("a remote source is trusted under CI", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll("/tpl", 0o755))

		p := &props.Props{FS: fs, Logger: logger.NewNoop(), Config: testutil.StoreFromYAML(t, "ci: true\n")}
		got, err := (&SkeletonOptions{Templates: []string{remote, "/tpl"}}).resolveTemplateSources(context.Background(), p)
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, generator.TemplateSourceGit, got[0].Type)
		assert.Equal(t, "v1.2.0", got[0].Ref)
		assert.Equal(t, "/tpl", got[1].Location)
	})

	t.Run("a malformed spec is refused", func(t *testing.T) {
		t.Parallel()

		p := &props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop()}
		_, err := (&SkeletonOptions{Templates: []string{"  "}}).resolveTemplateSources(context.Background(), p)
		require.Error(t, err)
	})

	t.Run("declining a remote source stops the run", func(t *testing.T) {
		t.Parallel()

		p := &props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop(), IO: formtest.AccessibleTTY(formtest.Answers("n"))}
		o := &SkeletonOptions{Name: "mytool", Repo: "org/mytool", Templates: []string{remote}}
		require.ErrorIs(t, o.Run(context.Background(), p), icmd.ErrRemoteTemplateDeclined)
	})
}

func TestNewCmdSkeleton_RunE(t *testing.T) {
	t.Parallel()

	t.Run("invalid flags are a usage error", func(t *testing.T) {
		t.Parallel()

		cmd := NewCmdSkeleton(nobodyTyping(), &SharedFlags{})
		cmd.SetArgs([]string{"--name", "Bad Name", "--repo", "org/tool"})
		err := cmd.ExecuteContext(context.Background())
		require.ErrorIs(t, err, generator.ErrInvalidInput)
	})

	t.Run("valid flags reach the run", func(t *testing.T) {
		t.Parallel()

		cmd := NewCmdSkeleton(nobodyTyping(), &SharedFlags{})
		cmd.SetArgs([]string{"--name", "tool", "--repo", "org/tool", "--overwrite", "bad-value"})
		require.ErrorIs(t, cmd.ExecuteContext(context.Background()), ErrInvalidOverwriteValue)
	})
}

func TestFeatureGlossAndLabel_Fallbacks(t *testing.T) {
	t.Parallel()

	assert.Empty(t, featureGloss("not-a-feature"))
	assert.Equal(t, "not-a-feature", featureLabel("not-a-feature"))
}

func TestConfirmedOrCancelled(t *testing.T) {
	t.Parallel()

	require.ErrorIs(t, (&SkeletonOptions{}).confirmedOrCancelled(), huh.ErrUserAborted)
	require.NoError(t, (&SkeletonOptions{confirmed: true}).confirmedOrCancelled())
}

func TestWizardRevisit_Command(t *testing.T) {
	t.Parallel()

	t.Run("a stdin nobody types at is refused with the hint", func(t *testing.T) {
		t.Parallel()

		p, _ := revisitProject(t)
		p.IO = nobodyTyping().IO

		cmd := NewCmdWizard(p)
		cmd.SetArgs([]string{"--path", "/work", "--dry-run"})
		err := cmd.ExecuteContext(context.Background())
		require.ErrorIs(t, err, ErrNonInteractive)
	})

	t.Run("accessible prompts accept every page", func(t *testing.T) {
		t.Parallel()

		p, _ := revisitProject(t)
		p.IO = formtest.AccessibleTTY(formtest.Answers())

		var out bytes.Buffer
		require.NoError(t, (&WizardOptions{Path: "/work", DryRun: true}).Run(context.Background(), p, &out))
		assert.NotContains(t, out.String(), "description", "an untouched page changes nothing it shows")
	})
}

func TestWizardRevisit_Failures(t *testing.T) {
	t.Parallel()

	t.Run("an invalid answer is refused", func(t *testing.T) {
		t.Parallel()

		p, _ := revisitProject(t)
		o := &WizardOptions{Path: "/work", runForm: func(so *SkeletonOptions) error {
			so.EnvPrefix = "1FOO"

			return nil
		}}
		require.Error(t, o.Run(context.Background(), p, &bytes.Buffer{}))
	})

	t.Run("a dry run with nothing changed says so", func(t *testing.T) {
		t.Parallel()

		p, _ := revisitProject(t)
		o := &WizardOptions{Path: "/work", DryRun: true, runForm: func(*SkeletonOptions) error { return nil }}

		var out bytes.Buffer
		require.NoError(t, o.Run(context.Background(), p, &out))
		assert.Equal(t, "no changes\n", out.String())
	})

	t.Run("settings that cannot be written fail", func(t *testing.T) {
		t.Parallel()

		p, fs := revisitProject(t)
		p.FS = afero.NewReadOnlyFs(fs)
		o := &WizardOptions{Path: "/work", runForm: func(so *SkeletonOptions) error {
			so.Description = "new"

			return nil
		}}
		require.Error(t, o.Run(context.Background(), p, &bytes.Buffer{}))
	})

	t.Run("an mcp surface change to an unknown command fails", func(t *testing.T) {
		t.Parallel()

		p, _ := revisitProject(t)
		o := &WizardOptions{Path: "/work", runForm: func(so *SkeletonOptions) error {
			so.Features = append(so.Features, string(props.McpCmd))
			so.mcpCommands = []mcpCommandChoice{{Path: "ghost", Exposed: true}}
			so.MCPExposed = nil

			return nil
		}}
		require.Error(t, o.Run(context.Background(), p, &bytes.Buffer{}))
	})
}
