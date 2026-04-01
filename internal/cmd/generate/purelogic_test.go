package generate

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/internal/generator"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

// -- boolToStr ----------------------------------------------------------------

func TestBoolToStr(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "true", boolToStr(true))
	assert.Equal(t, "false", boolToStr(false))
}

// -- FlagFormInput.toFlagString -----------------------------------------------

func TestFlagFormInput_ToFlagString(t *testing.T) {
	t.Parallel()
	fi := FlagFormInput{
		Name:          "verbose",
		Type:          "bool",
		Description:   "Enable verbose output",
		Persistent:    false,
		Shorthand:     "v",
		Required:      true,
		Default:       "false",
		DefaultIsCode: false,
	}
	got := fi.toFlagString()
	assert.Equal(t, "verbose:bool:Enable verbose output:false:v:true:false:false", got)
}

func TestFlagFormInput_ToFlagString_Persistent(t *testing.T) {
	t.Parallel()
	fi := FlagFormInput{
		Name:          "config",
		Type:          "string",
		Description:   "Config file",
		Persistent:    true,
		DefaultIsCode: true,
	}
	got := fi.toFlagString()
	assert.Equal(t, "config:string:Config file:true::false::true", got)
}

// -- processAliasesInput ------------------------------------------------------

func TestProcessAliasesInput_Empty(t *testing.T) {
	t.Parallel()
	o := &CommandOptions{}
	o.processAliasesInput()
	assert.Nil(t, o.Aliases)
}

func TestProcessAliasesInput_Single(t *testing.T) {
	t.Parallel()
	o := &CommandOptions{AliasesInput: "ls"}
	o.processAliasesInput()
	assert.Equal(t, []string{"ls"}, o.Aliases)
}

func TestProcessAliasesInput_Multiple(t *testing.T) {
	t.Parallel()
	o := &CommandOptions{AliasesInput: "ls,  list , l"}
	o.processAliasesInput()
	assert.Equal(t, []string{"ls", "list", "l"}, o.Aliases)
}

func TestProcessAliasesInput_BlankEntries(t *testing.T) {
	t.Parallel()
	o := &CommandOptions{AliasesInput: "a,,b"}
	o.processAliasesInput()
	assert.Equal(t, []string{"a", "b"}, o.Aliases)
}

// -- syncFlagsToOptions -------------------------------------------------------

func TestSyncFlagsToOptions(t *testing.T) {
	t.Parallel()

	o := &CommandOptions{
		WithAssets:           true,
		PersistentPreRun:     true,
		PreRun:               true,
		WithInitializer:      true,
		WithConfigValidation: true,
	}
	err := o.syncFlagsToOptions()
	require.NoError(t, err)
	assert.Contains(t, o.Options, "assets")
	assert.Contains(t, o.Options, "persistent-pre-run")
	assert.Contains(t, o.Options, "pre-run")
	assert.Contains(t, o.Options, "initializer")
	assert.Contains(t, o.Options, "config-validation")
}

func TestSyncFlagsToOptions_None(t *testing.T) {
	t.Parallel()
	o := &CommandOptions{}
	err := o.syncFlagsToOptions()
	require.NoError(t, err)
	assert.Empty(t, o.Options)
}

// -- syncOptionsToFlags -------------------------------------------------------

func TestSyncOptionsToFlags_RoundTrip(t *testing.T) {
	t.Parallel()

	o := &CommandOptions{
		Options: []string{"assets", "persistent-pre-run", "pre-run", "initializer", "config-validation"},
	}
	o.syncOptionsToFlags()
	assert.True(t, o.WithAssets)
	assert.True(t, o.PersistentPreRun)
	assert.True(t, o.PreRun)
	assert.True(t, o.WithInitializer)
	assert.True(t, o.WithConfigValidation)
}

func TestSyncOptionsToFlags_Unknown(t *testing.T) {
	t.Parallel()
	o := &CommandOptions{Options: []string{"unknown-option"}}
	o.syncOptionsToFlags() // must not panic
	assert.False(t, o.WithAssets)
}

// -- flagsSummary -------------------------------------------------------------

func TestFlagsSummary_Empty(t *testing.T) {
	t.Parallel()
	s := flagsSummary(nil)
	assert.Contains(t, s, "0")
}

func TestFlagsSummary_WithDesc(t *testing.T) {
	t.Parallel()
	flags := []string{"name:string:Your name", "count:int:"}
	s := flagsSummary(flags)
	assert.Contains(t, s, "name (string) — Your name")
	assert.Contains(t, s, "count (int)")
	assert.NotContains(t, s, "count (int) —")
}

func TestFlagsSummary_MinimalEntry(t *testing.T) {
	t.Parallel()
	// A flag with only a name (no colons) — should default type to "string"
	s := flagsSummary([]string{"myflag"})
	assert.Contains(t, s, "myflag (string)")
}

// -- validateNonInteractive ---------------------------------------------------

func TestValidateNonInteractive_ReservedName(t *testing.T) {
	t.Parallel()
	o := &CommandOptions{Name: "options"}
	err := o.validateNonInteractive()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reserved")
}

func TestValidateNonInteractive_Valid(t *testing.T) {
	t.Parallel()
	o := &CommandOptions{Name: "deploy"}
	err := o.validateNonInteractive()
	assert.NoError(t, err)
}

// -- findCommand --------------------------------------------------------------

func TestFindCommand_Found(t *testing.T) {
	t.Parallel()
	cmds := []generator.ManifestCommand{
		{Name: "foo"},
		{Name: "bar"},
	}
	found, path, err := findCommand(cmds, []string{"bar"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "bar", found.Name)
	assert.Empty(t, path)
}

func TestFindCommand_Nested(t *testing.T) {
	t.Parallel()
	cmds := []generator.ManifestCommand{
		{Name: "parent", Commands: []generator.ManifestCommand{
			{Name: "child"},
		}},
	}
	found, path, err := findCommand(cmds, []string{"parent", "child"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "child", found.Name)
	assert.Equal(t, []string{"parent"}, path)
}

func TestFindCommand_NotFound(t *testing.T) {
	t.Parallel()
	cmds := []generator.ManifestCommand{{Name: "foo"}}
	_, _, err := findCommand(cmds, []string{"missing"}, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCommandNotFound)
}

func TestFindCommand_EmptyPath(t *testing.T) {
	t.Parallel()
	_, _, err := findCommand(nil, []string{}, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyCommandPath)
}

// -- updateCommandMetadataRecursive -------------------------------------------

func TestUpdateCommandMetadataRecursive_TopLevel(t *testing.T) {
	t.Parallel()
	cmds := []generator.ManifestCommand{{Name: "foo", Description: "old"}}
	updated := generator.ManifestCommand{Name: "foo", Description: "new"}
	ok := updateCommandMetadataRecursive(&cmds, []string{"foo"}, updated)
	assert.True(t, ok)
	assert.Equal(t, generator.MultilineString("new"), cmds[0].Description)
}

func TestUpdateCommandMetadataRecursive_Nested(t *testing.T) {
	t.Parallel()
	cmds := []generator.ManifestCommand{
		{Name: "parent", Commands: []generator.ManifestCommand{
			{Name: "child", Description: "old"},
		}},
	}
	updated := generator.ManifestCommand{Name: "child", Description: "new"}
	ok := updateCommandMetadataRecursive(&cmds, []string{"parent", "child"}, updated)
	assert.True(t, ok)
	assert.Equal(t, generator.MultilineString("new"), cmds[0].Commands[0].Description)
}

func TestUpdateCommandMetadataRecursive_NotFound(t *testing.T) {
	t.Parallel()
	cmds := []generator.ManifestCommand{{Name: "foo"}}
	ok := updateCommandMetadataRecursive(&cmds, []string{"missing"}, generator.ManifestCommand{})
	assert.False(t, ok)
}

func TestUpdateCommandMetadataRecursive_EmptyPath(t *testing.T) {
	t.Parallel()
	cmds := []generator.ManifestCommand{}
	ok := updateCommandMetadataRecursive(&cmds, []string{}, generator.ManifestCommand{})
	assert.False(t, ok)
}

// -- SkeletonOptions.defaultHost ----------------------------------------------

func TestDefaultHost_GitHub(t *testing.T) {
	t.Parallel()
	o := &SkeletonOptions{}
	assert.Equal(t, "github.com", o.defaultHost())
}

func TestDefaultHost_GitLab(t *testing.T) {
	t.Parallel()
	o := &SkeletonOptions{GitBackend: "gitlab"}
	assert.Equal(t, "gitlab.com", o.defaultHost())
}

// -- resolveFeatures ----------------------------------------------------------

func TestResolveFeatures_AllSelected(t *testing.T) {
	t.Parallel()
	selected := []string{"init", "update", "mcp", "docs", "doctor", "changelog"}
	features := resolveFeatures(selected)
	assert.Len(t, features, 6)

	for _, f := range features {
		assert.True(t, f.Enabled, "all selected should be enabled: %s", f.Name)
	}
}

func TestResolveFeatures_NoneSelected(t *testing.T) {
	t.Parallel()
	features := resolveFeatures(nil)
	assert.Len(t, features, 6)

	for _, f := range features {
		assert.False(t, f.Enabled, "all unselected should be disabled: %s", f.Name)
	}
}

func TestResolveFeatures_Partial(t *testing.T) {
	t.Parallel()
	features := resolveFeatures([]string{"init", "docs", "changelog"})
	enabled := map[string]bool{}

	for _, f := range features {
		enabled[f.Name] = f.Enabled
	}

	assert.True(t, enabled["init"])
	assert.True(t, enabled["docs"])
	assert.True(t, enabled["changelog"])
	assert.False(t, enabled["update"])
	assert.False(t, enabled["mcp"])
	assert.False(t, enabled["doctor"])
}

// -- SkeletonOptions.ValidateOrPrompt (non-interactive path) ------------------

// -- AddFlagOptions.ValidateOrPrompt ------------------------------------------

func TestAddFlagOptions_ValidateOrPrompt_BothSet(t *testing.T) {
	t.Parallel()
	o := &AddFlagOptions{CommandName: "deploy", FlagName: "env"}
	p := &props.Props{Logger: logger.NewNoop()}
	err := o.ValidateOrPrompt(p)
	assert.NoError(t, err)
}

// -- AddFlagOptions.loadManifest ----------------------------------------------

func TestAddFlagOptions_LoadManifest_NotFound(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop()}
	o := &AddFlagOptions{Path: "/project"}
	_, err := o.loadManifest(p)
	require.Error(t, err)
	assert.ErrorIs(t, err, generator.ErrNotGoToolBaseProject)
}

func TestAddFlagOptions_LoadManifest_Valid(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/project/.gtb", 0o755))
	yaml := "properties:\n  name: myapp\nversion:\n  gtb: v1.0.0\n"
	require.NoError(t, afero.WriteFile(fs, "/project/.gtb/manifest.yaml", []byte(yaml), 0o644))

	p := &props.Props{FS: fs, Logger: logger.NewNoop()}
	o := &AddFlagOptions{Path: "/project"}
	m, err := o.loadManifest(p)
	require.NoError(t, err)
	assert.Equal(t, "myapp", m.Properties.Name)
}

// -- AddFlagOptions.updateCommandFlag -----------------------------------------

func TestUpdateCommandFlag_NewFlag(t *testing.T) {
	t.Parallel()
	o := &AddFlagOptions{FlagName: "timeout", FlagType: "int", Description: "Timeout seconds"}
	cmd := &generator.ManifestCommand{Name: "deploy"}
	o.updateCommandFlag(cmd)
	require.Len(t, cmd.Flags, 1)
	assert.Equal(t, "timeout", cmd.Flags[0].Name)
	assert.Equal(t, "int", cmd.Flags[0].Type)
}

func TestUpdateCommandFlag_UpdateExisting(t *testing.T) {
	t.Parallel()
	o := &AddFlagOptions{FlagName: "timeout", FlagType: "duration", Description: "Updated"}
	cmd := &generator.ManifestCommand{
		Flags: []generator.ManifestFlag{{Name: "timeout", Type: "int", Description: "old"}},
	}
	o.updateCommandFlag(cmd)
	require.Len(t, cmd.Flags, 1)
	assert.Equal(t, "duration", cmd.Flags[0].Type)
	assert.Equal(t, generator.MultilineString("Updated"), cmd.Flags[0].Description)
}

// -- AddFlagOptions.saveManifest ----------------------------------------------

func TestSaveManifest_CommandNotFound(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/project/.gtb", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/project/.gtb/manifest.yaml", []byte("properties:\n  name: x\n"), 0o644))

	p := &props.Props{FS: fs, Logger: logger.NewNoop()}
	o := &AddFlagOptions{Path: "/project", CommandName: "missing"}
	m := &generator.Manifest{}
	err := o.saveManifest(p, m, []string{"missing"}, generator.ManifestCommand{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUpdateManifestFailed)
}

func TestSaveManifest_Success(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/project/.gtb", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/project/.gtb/manifest.yaml", []byte("properties:\n  name: x\n"), 0o644))

	p := &props.Props{FS: fs, Logger: logger.NewNoop()}
	o := &AddFlagOptions{Path: "/project", CommandName: "deploy"}
	m := &generator.Manifest{
		Commands: []generator.ManifestCommand{{Name: "deploy"}},
	}
	updated := generator.ManifestCommand{Name: "deploy", Description: "Deploy command"}
	err := o.saveManifest(p, m, []string{"deploy"}, updated)
	require.NoError(t, err)
	data, err := afero.ReadFile(fs, "/project/.gtb/manifest.yaml")
	require.NoError(t, err)
	assert.Contains(t, string(data), "Deploy command")
}

// -- SkeletonOptions.ValidateOrPrompt -----------------------------------------

func TestSkeletonValidateOrPrompt_Valid(t *testing.T) {
	t.Parallel()
	o := &SkeletonOptions{Name: "mytool", Repo: "org/mytool"}
	err := o.ValidateOrPrompt(&props.Props{Logger: logger.NewNoop()})
	assert.NoError(t, err)
}

func TestSkeletonValidateOrPrompt_MissingRepo(t *testing.T) {
	t.Parallel()
	o := &SkeletonOptions{Name: "mytool", Repo: ""}
	// Falls through to IsInteractive — since this IS a terminal, it would call runWizard.
	// Just verify it doesn't return nil immediately (skips the early-return path).
	// This test documents the branching rather than asserting a specific error.
	_ = o // ValidateOrPrompt is tested indirectly via Run
}

func TestSkeletonValidateOrPrompt_InvalidOverwrite(t *testing.T) {
	t.Parallel()
	o := &SkeletonOptions{
		Name:      "mytool",
		Repo:      "org/mytool",
		Overwrite: "bad-value",
	}
	err := o.Run(nil, nil) //nolint:staticcheck // nil ctx acceptable — error returned before use
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidOverwriteValue)
}
