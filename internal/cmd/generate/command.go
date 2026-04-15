package generate

import (
	"context"
	"fmt"
	"strings"

	"charm.land/huh/v2"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	icmd "github.com/phpboyscout/go-tool-base/internal/cmd"
	"github.com/phpboyscout/go-tool-base/internal/generator"
	"github.com/phpboyscout/go-tool-base/pkg/forms"
	"github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/utils"
)

// flagFieldCount is the number of colon-separated fields in a serialised flag string.
const (
	flagFieldCount     = 8
	flagTypeIdx        = 1
	flagDescriptionIdx = 2
)

type CommandOptions struct {
	Name                 string
	Short                string
	Long                 string
	Path                 string
	WithAssets           bool
	Parent               string
	Args                 string
	Flags                []string
	AliasesInput         string // For form input
	Aliases              []string
	ScriptPath           string
	Prompt               string
	Agentless            bool
	PersistentPreRun     bool
	PreRun               bool
	Force                bool
	WithInitializer      bool
	WithConfigValidation bool
	WrapSubcommands      *bool
	Protected            *bool
	Options              []string // For MultiSelect
	AddFlags             bool     // Whether to show the flag entry stage
	AddPrompt            bool     // Whether to show the AI prompt stage
}

// FlagFormInput holds data for a single flag collected via the interactive form.
type FlagFormInput struct {
	Name          string
	Type          string
	Description   string
	Persistent    bool
	Shorthand     string
	Required      bool
	Default       string
	DefaultIsCode bool
	AddAnother    bool
}

// toFlagString serializes a FlagFormInput to the colon-delimited format
// expected by the generator: name:type:desc:persistent:shorthand:required:default:defaultIsCode.
func (fi *FlagFormInput) toFlagString() string {
	return strings.Join([]string{
		fi.Name,
		fi.Type,
		fi.Description,
		boolToStr(fi.Persistent),
		fi.Shorthand,
		boolToStr(fi.Required),
		fi.Default,
		boolToStr(fi.DefaultIsCode),
	}, ":")
}

func boolToStr(b bool) string {
	if b {
		return "true"
	}

	return "false"
}

func NewCmdCommand(p *props.Props) *cobra.Command {
	opts := CommandOptions{}

	var (
		protectedFlag       bool
		wrapSubcommandsFlag bool
	)

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

			if cmd.Flags().Changed("wrap-subcommands") {
				opts.WrapSubcommands = &wrapSubcommandsFlag
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
	cmd.Flags().BoolVar(&opts.WithConfigValidation, "with-config-validation", false, "Generate a config validation stub for this command")
	cmd.Flags().BoolVar(&wrapSubcommandsFlag, "wrap-subcommands", true, "Automatically wrap subcommands with middleware")
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

	return forms.NewWizard(o.buildMainGroup()).
		Step(o.runAdditionalSteps).
		Run()
}

// buildMainGroup returns the primary form group containing core command fields
// plus toggles to opt in to flag and AI prompt stages.
func (o *CommandOptions) buildMainGroup() *huh.Group {
	return huh.NewGroup(
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
			Description("Optional — leave blank to use short description").
			Value(&o.Long),
		huh.NewInput().
			Title("Aliases").
			Description("Comma-separated aliases (e.g. ls, list)").
			Value(&o.AliasesInput),
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
				huh.NewOption("Config Validation", "config-validation"),
			).
			Value(&o.Options),
		huh.NewConfirm().
			Title("Add Flags").
			Description("Define flags for this command in the next step?").
			Affirmative("Yes").
			Negative("No").
			Value(&o.AddFlags),
		huh.NewConfirm().
			Title("Set AI Prompt").
			Description("Provide an AI prompt to generate the command logic in the next step?").
			Affirmative("Yes").
			Negative("No").
			Value(&o.AddPrompt),
	).Title("New Command").
		Description("Configure your new CLI command.\n")
}

// buildFlagGroup returns a form group for capturing a single flag's definition.
// existing is the list of flag strings already collected in this session; when
// non-empty they are displayed in the group description so the user can see
// what has been defined so far.
func (o *CommandOptions) buildFlagGroup(fi *FlagFormInput, existing []string) *huh.Group {
	fi.Type = "string" // sensible default

	desc := "Define a flag for this command.\n"
	if len(existing) > 0 {
		desc = flagsSummary(existing)
	}

	return huh.NewGroup(
		huh.NewInput().
			Title("Flag Name").
			Description("Kebab-case name (e.g. output-format)").
			Value(&fi.Name).
			Validate(func(s string) error {
				if s == "" {
					return errors.Newf("flag name is required")
				}

				return nil
			}),
		huh.NewSelect[string]().
			Title("Type").
			Options(
				huh.NewOption("string", "string"),
				huh.NewOption("bool", "bool"),
				huh.NewOption("int", "int"),
				huh.NewOption("float64", "float64"),
				huh.NewOption("[]string (stringSlice)", "stringSlice"),
				huh.NewOption("[]int (intSlice)", "intSlice"),
				huh.NewOption("duration", "duration"),
			).
			Value(&fi.Type),
		huh.NewInput().
			Title("Description").
			Value(&fi.Description),
		huh.NewInput().
			Title("Shorthand").
			Description("Single character (leave blank for none)").
			Value(&fi.Shorthand),
		huh.NewInput().
			Title("Default Value").
			Description("Leave blank for zero value").
			Value(&fi.Default),
		huh.NewConfirm().
			Title("Persistent").
			Description("Inherit this flag in all subcommands?").
			Value(&fi.Persistent),
		huh.NewConfirm().
			Title("Required").
			Description("Is this flag required?").
			Value(&fi.Required),
		huh.NewConfirm().
			Title("Add Another Flag").
			Description("Define another flag after this one?").
			Affirmative("Yes").
			Negative("No, done").
			Value(&fi.AddAnother),
	).Title("Flag Definition").
		Description(desc)
}

// flagsSummary builds a human-readable description listing the flags already
// collected, shown on subsequent flag forms as context for the user.
// Each entry is rendered as "  • name (type) — description".
func flagsSummary(flags []string) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "Flags added so far (%d):\n", len(flags))

	for _, f := range flags {
		parts := strings.SplitN(f, ":", flagFieldCount)
		name := parts[0]
		flagType := "string"
		desc := ""

		if len(parts) > flagTypeIdx {
			flagType = parts[flagTypeIdx]
		}

		if len(parts) > flagDescriptionIdx {
			desc = parts[flagDescriptionIdx]
		}

		if desc != "" {
			fmt.Fprintf(&sb, "  • %s (%s) — %s\n", name, flagType, desc)
		} else {
			fmt.Fprintf(&sb, "  • %s (%s)\n", name, flagType)
		}
	}

	return sb.String()
}

// buildPromptGroup returns a form group for capturing the AI generation prompt.
func (o *CommandOptions) buildPromptGroup() *huh.Group {
	return huh.NewGroup(
		huh.NewText().
			Title("AI Prompt").
			Description("Describe what this command should do, or paste a script to convert to Go.").
			Value(&o.Prompt),
	).Title("AI Generation").
		Description("Provide a prompt or script for AI-assisted command logic generation.\n")
}

// runAdditionalSteps processes aliases and conditionally runs the flag and
// prompt stages based on selections made in the main form.
//
// Escape on the first flag form (before any flags are saved) propagates back
// to the main form. Escape on subsequent flag forms stops flag entry and
// continues. Escape on the prompt form returns to the main form.
func (o *CommandOptions) runAdditionalSteps() error {
	o.processAliasesInput()

	if o.AddFlags {
		if err := o.runFlagLoop(); err != nil {
			return err
		}
	}

	if o.AddPrompt {
		if err := forms.NewNavigable(o.buildPromptGroup()).Run(); err != nil {
			return err
		}
	}

	o.syncOptionsToFlags()

	return nil
}

// runFlagLoop presents the flag form repeatedly until the user unchecks
// "Add Another Flag". Escape on the very first flag form propagates
// huh.ErrUserAborted so the Wizard navigates back to the main form.
// Escape on any subsequent flag form stops flag entry cleanly.
func (o *CommandOptions) runFlagLoop() error {
	first := true

	for {
		fi := FlagFormInput{}

		err := forms.NewNavigable(o.buildFlagGroup(&fi, o.Flags)).Run()
		if errors.Is(err, huh.ErrUserAborted) {
			if first {
				return err // propagate → Wizard goes back to main form
			}

			return nil // user is done adding flags
		}

		if err != nil {
			return err
		}

		o.Flags = append(o.Flags, fi.toFlagString())
		first = false

		if !fi.AddAnother {
			return nil
		}
	}
}

func (o *CommandOptions) processAliasesInput() {
	if o.AliasesInput == "" {
		return
	}

	o.Aliases = []string{}

	for a := range strings.SplitSeq(o.AliasesInput, ",") {
		if trimmed := strings.TrimSpace(a); trimmed != "" {
			o.Aliases = append(o.Aliases, trimmed)
		}
	}
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

	if o.WithConfigValidation {
		o.Options = append(o.Options, "config-validation")
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
		case "config-validation":
			o.WithConfigValidation = true
		}
	}
}

func (o *CommandOptions) Run(ctx context.Context, p *props.Props) error {
	o.Path = icmd.ResolveProjectPath(p, o.Path)

	cfg := &generator.Config{
		Name:                          o.Name,
		Short:                         o.Short,
		Long:                          o.Long,
		Aliases:                       o.Aliases,
		Path:                          o.Path,
		WithAssets:                    o.WithAssets,
		Parent:                        o.Parent,
		Args:                          o.Args,
		DryRun:                        dryRun,
		Flags:                         o.Flags,
		ScriptPath:                    o.ScriptPath,
		Prompt:                        o.Prompt,
		AIProvider:                    aiProvider,
		AIModel:                       aiModel,
		Agentless:                     o.Agentless,
		PersistentPreRun:              o.PersistentPreRun,
		PreRun:                        o.PreRun,
		Force:                         o.Force,
		WithInitializer:               o.WithInitializer,
		WithConfigValidation:          o.WithConfigValidation,
		WrapSubcommandsWithMiddleware: o.WrapSubcommands,
		Protected:                     o.Protected,
	}

	if cfg.Long == "" {
		cfg.Long = cfg.Short
	}

	return generator.New(p, cfg).Generate(ctx)
}
