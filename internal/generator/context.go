package generator

import "strings"

// CommandContext holds the fully resolved configuration for a single command
// generation or regeneration pass. It is a value type so recursive invocations
// cannot accidentally share or mutate each other's state.
type CommandContext struct {
	// Identity
	Name       string
	ParentPath []string // empty = direct child of root

	// Display
	Short string
	Long  string

	// Routing / feature options
	Aliases                       []string
	Args                          string
	WithAssets                    bool
	WithInitializer               bool
	WithConfigValidation          bool
	WrapSubcommandsWithMiddleware bool
	PersistentPreRun              bool
	PreRun                        bool
	Protected                     *bool
	Hidden                        bool

	// Project-level settings (carried from the originating generator)
	ProjectPath string
	DryRun      bool
	Force       bool
	UpdateDocs  bool
}

// buildCommandContext constructs a CommandContext from a ManifestCommand and
// the parent path accumulated during recursive regeneration.
func buildCommandContext(projectPath string, dryRun, force, updateDocs bool, cmd ManifestCommand, parentPath []string) CommandContext {
	return CommandContext{
		Name:                          cmd.Name,
		ParentPath:                    parentPath,
		Short:                         string(cmd.Description),
		Long:                          string(cmd.LongDescription),
		Aliases:                       cmd.Aliases,
		Args:                          cmd.Args,
		WithAssets:                    cmd.WithAssets,
		WithInitializer:               cmd.WithInitializer,
		WithConfigValidation:          cmd.WithConfigValidation,
		WrapSubcommandsWithMiddleware: cmd.WrapSubcommandsWithMiddleware,
		PersistentPreRun:              cmd.PersistentPreRun,
		PreRun:                        cmd.PreRun,
		Protected:                     cmd.Protected,
		Hidden:                        cmd.Hidden,
		ProjectPath:                   projectPath,
		DryRun:                        dryRun,
		Force:                         force,
		UpdateDocs:                    updateDocs,
	}
}

// ToConfig converts the CommandContext into a *Config suitable for constructing
// a Generator scoped to this specific command.
func (c CommandContext) ToConfig() *Config {
	parent := "root"
	if len(c.ParentPath) > 0 {
		parent = strings.Join(c.ParentPath, "/")
	}

	return &Config{
		Path:                          c.ProjectPath,
		Name:                          c.Name,
		Parent:                        parent,
		Short:                         c.Short,
		Long:                          c.Long,
		Aliases:                       c.Aliases,
		Args:                          c.Args,
		WithAssets:                    c.WithAssets,
		WithInitializer:               c.WithInitializer,
		WithConfigValidation:          c.WithConfigValidation,
		WrapSubcommandsWithMiddleware: &c.WrapSubcommandsWithMiddleware,
		PersistentPreRun:              c.PersistentPreRun,
		PreRun:                        c.PreRun,
		Protected:                     c.Protected,
		Hidden:                        c.Hidden,
		DryRun:                        c.DryRun,
		Force:                         c.Force,
		UpdateDocs:                    c.UpdateDocs,
	}
}
