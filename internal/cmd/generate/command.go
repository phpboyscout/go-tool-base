package generate

import (
	"context"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"github.com/phpboyscout/gtb/internal/generator"
	"github.com/phpboyscout/gtb/pkg/props"
	"github.com/phpboyscout/gtb/pkg/utils"
)

type CommandOptions struct {
	Name             string
	Short            string
	Long             string
	Path             string
	WithAssets       bool
	Parent           string
	Args             string
	Flags            []string
	FlagsInput       string // For form input
	Aliases          []string
	AliasesInput     string // For form input
	ScriptPath       string
	Prompt           string
	Agentless        bool
	PersistentPreRun bool
	PreRun           bool
	Force            bool
	WithInitializer  bool
	Protected        *bool
	Options          []string // For MultiSelect
}

func NewCmdCommand(p *props.Props) *cobra.Command {
	opts := CommandOptions{}

	var protectedFlag bool

	cmd := &cobra.Command{
		Use:   "command",
		Short: "Generate a new command or subcommand",
		Long: `Generate a new command or subcommand with boilerplate code.

Examples:
  # Generate a command named 'login' in the current project
  gtb generate command --name login --short "Login to the system"

  # Generate a subcommand 'list' under 'login'
  gtb generate command --name list --parent login --short "List sessions"

  # Generate a command with flags and assets
  gtb generate command -n serve -f "port:int:Port to listen on" --assets

  # Generate a command from a script (e.g., bash)
  # The AI will attempt to convert the script to Go code
  # The autonomous agent is used by default for verification
  gtb generate command -n backup --script ./backup.sh

  # Use the original feedback loop instead of the autonomous agent
  gtb generate command -n backup --script ./backup.sh --agentless

  # Create a protected command (cannot be overwritten by generator)
  gtb generate command -n sensible --protected

  # Temporarily unprotect a command to allow overwrite
  gtb generate command -n sensible --protected=false --force
`,
		Run: func(cmd *cobra.Command, args []string) {
			p.ErrorHandler.Fatal(opts.ValidateOrPrompt())

			// Handle tri-state protected flag
			if cmd.Flags().Changed("protected") {
				opts.Protected = &protectedFlag
			}

			p.ErrorHandler.Fatal(opts.Run(cmd.Context(), p))
		},
	}

	cmd.Flags().StringVarP(&opts.Name, "name", "n", "", "Command name (kebab-case)")
	cmd.Flags().StringVarP(&opts.Short, "short", "s", "", "Short description")
	cmd.Flags().StringVarP(&opts.Long, "long", "l", "", "Long description")
	cmd.Flags().StringVarP(&opts.Path, "path", "p", ".", "Path to project root")
	cmd.Flags().BoolVar(&opts.WithAssets, "assets", false, "Include assets directory support")
	cmd.Flags().StringVar(&opts.Parent, "parent", "root", "Parent command name (default: root)")
	cmd.Flags().StringVar(&opts.Args, "args", "", "Positional arguments (e.g. ExactArgs(1), ArbitraryArgs)")
	cmd.Flags().StringArrayVarP(&opts.Aliases, "alias", "a", []string{}, "Aliases for the command")
	cmd.Flags().StringArrayVarP(&opts.Flags, "flag", "f", []string{}, "Flags definition (name:type:description:persistent:shorthand:required:default:defaultIsCode)")
	cmd.Flags().StringVar(&opts.ScriptPath, "script", "", "Path to a script to convert to Go (bash/python/js)")
	cmd.Flags().StringVar(&opts.Prompt, "prompt", "", "Natural language description or path to a file containing the description")
	cmd.Flags().BoolVar(&opts.Agentless, "agentless", false, "Use original retry loop instead of autonomous agent")
	cmd.Flags().BoolVar(&opts.PersistentPreRun, "persistent-pre-run", false, "Generate a PersistentPreRun hook")
	cmd.Flags().BoolVar(&opts.PreRun, "pre-run", false, "Generate a PreRun hook")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "Overwrite existing files")
	cmd.Flags().BoolVar(&opts.WithInitializer, "with-initializer", false, "Generate an Initializer for this command")
	cmd.Flags().BoolVar(&protectedFlag, "protected", false, "Mark the command as protected (tri-state: --protected for true, --protected=false for false, omitted for nil)")

	cmd.MarkFlagsMutuallyExclusive("prompt", "script")

	cmd.AddCommand(NewCmdProtect(p))
	cmd.AddCommand(NewCmdUnprotect(p))

	return cmd
}

func NewCmdProtect(p *props.Props) *cobra.Command {
	var path string

	cmd := &cobra.Command{
		Use:   "protect [command-path]",
		Short: "Protect a command from being overwritten",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			gen := generator.New(p, &generator.Config{Path: path})

			err := gen.SetProtection(cmd.Context(), args[0], true)
			if err != nil {
				p.ErrorHandler.Fatal(err)
			}

			p.Logger.Infof("Command '%s' is now protected", args[0])
		},
	}
	cmd.Flags().StringVarP(&path, "path", "p", ".", "Path to project root")

	return cmd
}

func NewCmdUnprotect(p *props.Props) *cobra.Command {
	var path string

	cmd := &cobra.Command{
		Use:   "unprotect [command-path]",
		Short: "Unprotect a command to allow overwriting",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			gen := generator.New(p, &generator.Config{Path: path})

			err := gen.SetProtection(cmd.Context(), args[0], false)
			if err != nil {
				p.ErrorHandler.Fatal(err)
			}

			p.Logger.Warnf("Command '%s' is now unprotected", args[0])
		},
	}
	cmd.Flags().StringVarP(&path, "path", "p", ".", "Path to project root")

	return cmd
}

func (o *CommandOptions) ValidateOrPrompt() error {
	if o.Name != "" {
		return o.validateNonInteractive()
	}

	if !utils.IsInteractive() {
		return ErrNonInteractive
	}

	return o.runInteractivePrompt()
}

func (o *CommandOptions) validateNonInteractive() error {
	if o.Name == "options" {
		return errors.Newf("command name 'options' is reserved")
	}

	return o.syncFlagsToOptions()
}

func (o *CommandOptions) runInteractivePrompt() error {
	if len(o.Aliases) > 0 {
		o.AliasesInput = strings.Join(o.Aliases, ", ")
	}

	if len(o.Flags) > 0 {
		o.FlagsInput = strings.Join(o.Flags, "\n")
	}

	form := o.buildForm()

	if err := form.Run(); err != nil {
		return err
	}

	if o.AliasesInput != "" {
		o.Aliases = []string{}
		for a := range strings.SplitSeq(o.AliasesInput, ",") {
			trimmed := strings.TrimSpace(a)
			if trimmed != "" {
				o.Aliases = append(o.Aliases, trimmed)
			}
		}
	}

	if o.FlagsInput != "" {
		o.Flags = []string{}
		// Split by newline for multiple flags
		for f := range strings.SplitSeq(o.FlagsInput, "\n") {
			trimmed := strings.TrimSpace(f)
			if trimmed != "" {
				o.Flags = append(o.Flags, trimmed)
			}
		}
	}

	o.syncOptionsToFlags()

	return nil
}

func (o *CommandOptions) syncFlagsToOptions() error {
	if o.WithAssets {
		o.Options = append(o.Options, "assets")
	}

	if o.PersistentPreRun {
		o.Options = append(o.Options, "persistent-pre-run")
	}

	if o.PreRun {
		o.Options = append(o.Options, "pre-run")
	}

	if o.WithInitializer {
		o.Options = append(o.Options, "initializer")
	}

	return nil
}

func (o *CommandOptions) syncOptionsToFlags() {
	for _, opt := range o.Options {
		switch opt {
		case "assets":
			o.WithAssets = true
		case "persistent-pre-run":
			o.PersistentPreRun = true
		case "pre-run":
			o.PreRun = true
		case "initializer":
			o.WithInitializer = true
		}
	}
}

func (o *CommandOptions) buildForm() *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Command Name").
				Description("Kebab-case name (e.g. create-user)").
				Value(&o.Name).
				Validate(func(s string) error {
					if s == "" {
						return errors.Newf("name is required")
					}

					if strings.Contains(s, " ") {
						return errors.Newf("name must not contain spaces")
					}

					if s == "options" {
						return errors.Newf("command name 'options' is reserved")
					}

					return nil
				}),
			huh.NewInput().
				Title("Short Description").
				Value(&o.Short).
				Validate(func(s string) error {
					if s == "" {
						return errors.Newf("description is required")
					}

					return nil
				}),
			huh.NewText().
				Title("Long Description").
				Description("Optional (leave blank to use short description)").
				Value(&o.Long),
			huh.NewInput().
				Title("Aliases").
				Description("Comma separated aliases (e.g. ls, list)").
				Value(&o.AliasesInput),
			huh.NewText().
				Title("Flags Definitions").
				Description("One definition per line: name:type:desc:persistent:shorthand:required:default:defaultIsCode").
				Value(&o.FlagsInput),
			huh.NewInput().
				Title("Parent Command").
				Description("Parent command name or path (e.g. root, or kube/ctx)").
				Value(&o.Parent),
			huh.NewInput().
				Title("Positional Arguments").
				Description("Cobra argument validation (e.g. ExactArgs(1), ArbitraryArgs)").
				Value(&o.Args),
			huh.NewMultiSelect[string]().
				Title("Options").
				Options(
					huh.NewOption("Include Assets", "assets"),
					huh.NewOption("PersistentPreRun Hook", "persistent-pre-run"),
					huh.NewOption("PreRun Hook", "pre-run"),
					huh.NewOption("Config Initialiser", "initializer"),
				).
				Value(&o.Options),
			huh.NewText().
				Title("AI Prompt").
				Description("Optional (leave blank to skip)").
				Value(&o.Prompt),
		),
	)
}

func (o *CommandOptions) Run(ctx context.Context, p *props.Props) error {
	cfg := &generator.Config{
		Name:             o.Name,
		Short:            o.Short,
		Long:             o.Long,
		Aliases:          o.Aliases,
		Path:             o.Path,
		WithAssets:       o.WithAssets,
		Parent:           o.Parent,
		Args:             o.Args,
		Flags:            o.Flags,
		ScriptPath:       o.ScriptPath,
		Prompt:           o.Prompt,
		AIProvider:       aiProvider,
		AIModel:          aiModel,
		Agentless:        o.Agentless,
		PersistentPreRun: o.PersistentPreRun,
		PreRun:           o.PreRun,
		Force:            o.Force,
		WithInitializer:  o.WithInitializer,
		Protected:        o.Protected,
	}

	if cfg.Long == "" {
		cfg.Long = cfg.Short
	}

	return generator.New(p, cfg).Generate(ctx)
}
