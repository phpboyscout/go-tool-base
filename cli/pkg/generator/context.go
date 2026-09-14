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
	Aliases              []string
	Args                 string
	WithAssets           bool
	WithInitializer      bool
	WithConfigValidation bool
	PersistentPreRun     bool
	PreRun               bool
	Protected            *bool
	MCPEnabled           *bool // tri-state MCP exposure; mirrors Protected
	Hidden               bool

	// Project-level settings (carried from the originating generator).
	//
	// Every one of these must be threaded through ToConfig as well as named
	// here. Overwrite was declared on Config but missing from this type, so
	// swapping g.config for a command context silently reset --overwrite to
	// its "ask" default for every command file in the run (issue #13). Taking
	// the whole *Config in buildCommandContext keeps the omission visible in
	// one place rather than spread across four call sites.
	ProjectPath string
	DryRun      bool
	Force       bool
	UpdateDocs  bool
	Overwrite   string
}

// buildCommandContext constructs a CommandContext from a ManifestCommand and
// the parent path accumulated during recursive regeneration, carrying the
// originating generator's project-level settings.
func buildCommandContext(cfg *Config, cmd ManifestCommand, parentPath []string) CommandContext {
	return CommandContext{
		Name:                 cmd.Name,
		ParentPath:           parentPath,
		Short:                string(cmd.Description),
		Long:                 string(cmd.LongDescription),
		Aliases:              cmd.Aliases,
		Args:                 cmd.Args,
		WithAssets:           cmd.WithAssets,
		WithInitializer:      cmd.WithInitializer,
		WithConfigValidation: cmd.WithConfigValidation,
		PersistentPreRun:     cmd.PersistentPreRun,
		PreRun:               cmd.PreRun,
		Protected:            cmd.Protected,
		MCPEnabled:           cmd.MCPEnabled,
		Hidden:               cmd.Hidden,
		ProjectPath:          cfg.Path,
		DryRun:               cfg.DryRun,
		Force:                cfg.Force,
		UpdateDocs:           cfg.UpdateDocs,
		Overwrite:            cfg.Overwrite,
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
		Path:                 c.ProjectPath,
		Name:                 c.Name,
		Parent:               parent,
		Short:                c.Short,
		Long:                 c.Long,
		Aliases:              c.Aliases,
		Args:                 c.Args,
		WithAssets:           c.WithAssets,
		WithInitializer:      c.WithInitializer,
		WithConfigValidation: c.WithConfigValidation,
		PersistentPreRun:     c.PersistentPreRun,
		PreRun:               c.PreRun,
		Protected:            c.Protected,
		MCPEnabled:           c.MCPEnabled,
		Hidden:               c.Hidden,
		DryRun:               c.DryRun,
		Force:                c.Force,
		UpdateDocs:           c.UpdateDocs,
		Overwrite:            c.Overwrite,
	}
}
