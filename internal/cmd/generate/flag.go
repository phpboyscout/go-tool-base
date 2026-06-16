package generate

import (
	"context"
	"os"
	"strings"

	"charm.land/huh/v2"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	icmd "gitlab.com/phpboyscout/go-tool-base/internal/cmd"
	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/utils"
)

type AddFlagOptions struct {
	CommandName string
	FlagName    string
	FlagType    string
	Description string
	Persistent  bool
	Path        string
}

func NewCmdAddFlag(p *props.Props) *cobra.Command {
	opts := AddFlagOptions{}

	cmd := &cobra.Command{
		Use:   "add-flag",
		Short: "Add a new flag to an existing command",
		Long: `Add a new flag to an existing command in the current project.

The target command is addressed by -c/--command. To target a nested
subcommand, pass the full slash-delimited command path (parent/child/leaf) —
NOT just the leaf name, and NOT a space- or dot-separated form:

  parent/child/leaf    a deep subcommand
  leaf                 only when it is a top-level command
  "reel create now"    rejected (space-joined)
  reel.now             rejected (dotted)

-p/--path is the filesystem project root (default "."), NOT a command path; it
tells gtb which project's .gtb/manifest.yaml to edit.

Examples:
  # Add --verbose to the top-level 'deploy' command
  gtb generate add-flag -c deploy -n verbose -t bool -d "Verbose output"

  # Add --force to the 'now' command nested under 'reel/create'
  gtb generate add-flag -c reel/create/now -n force -t bool -d "Skip confirmation"
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.ValidateOrPrompt(p); err != nil {
				return err
			}

			return opts.Run(cmd.Context(), p)
		},
	}

	cmd.Flags().StringVarP(&opts.CommandName, "command", "c", "", "Command to add the flag to; use a slash path (parent/child/leaf) for nested subcommands")
	cmd.Flags().StringVarP(&opts.FlagName, "name", "n", "", "Flag name")
	cmd.Flags().StringVarP(&opts.FlagType, "type", "t", "string", "Flag type (string, bool, int, float64, stringSlice, intSlice)")
	cmd.Flags().StringVarP(&opts.Description, "description", "d", "", "Flag description")
	cmd.Flags().BoolVar(&opts.Persistent, "persistent", false, "Make the flag persistent")
	cmd.Flags().StringVarP(&opts.Path, "path", "p", ".", "Filesystem project root directory (not a command path)")

	return cmd
}

func (o *AddFlagOptions) ValidateOrPrompt(p *props.Props) error {
	if o.CommandName != "" && o.FlagName != "" {
		return o.validateNonInteractive()
	}

	if !utils.IsInteractive() {
		return ErrNonInteractive
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Command Name (e.g. kube/login)").
				Value(&o.CommandName).
				Validate(func(s string) error {
					if s == "" {
						return ErrCommandNameRequired
					}

					return nil
				}),
			huh.NewInput().
				Title("Flag Name").
				Value(&o.FlagName).
				Validate(func(s string) error {
					if s == "" {
						return ErrFlagNameRequired
					}

					return nil
				}),
			huh.NewSelect[string]().
				Title("Flag Type").
				Options(
					huh.NewOption("string", "string"),
					huh.NewOption("bool", "bool"),
					huh.NewOption("int", "int"),
					huh.NewOption("float64", "float64"),
					huh.NewOption("stringSlice", "stringSlice"),
					huh.NewOption("intSlice", "intSlice"),
				).
				Value(&o.FlagType),
			huh.NewInput().
				Title("Flag Description").
				Value(&o.Description),
			huh.NewConfirm().
				Title("Persistent?").
				Value(&o.Persistent),
			huh.NewInput().
				Title("Path to project root").
				Value(&o.Path),
		),
	)

	return form.Run()
}

// validateNonInteractive applies the same field rules the interactive
// wizard enforces (and more) when both --command and --name arrive from
// the command line. Without this gate a bad --type or --name flowed
// straight into the manifest and the generated Go identifiers; the
// command path is validated segment-by-segment the same way --parent is.
func (o *AddFlagOptions) validateNonInteractive() error {
	for _, seg := range strings.Split(strings.Trim(o.CommandName, "/"), "/") {
		if err := generator.ValidateCommandName(seg); err != nil {
			return err
		}
	}

	if err := generator.ValidateFlagName(o.FlagName); err != nil {
		return err
	}

	return generator.ValidateFlagType(o.FlagType)
}

func (o *AddFlagOptions) Run(ctx context.Context, p *props.Props) error {
	o.Path = icmd.ResolveProjectPath(p, o.Path)

	m, err := o.loadManifest(p)
	if err != nil {
		return err
	}

	pathParts := strings.Split(strings.Trim(o.CommandName, "/"), "/")

	cmd, parentPath, err := findCommand(m.Commands, pathParts, []string{})
	if err != nil {
		return err
	}

	o.updateCommandFlag(cmd)

	if err := o.saveManifest(p, m, pathParts, *cmd); err != nil {
		return err
	}

	if err := o.regenerateCommand(ctx, p, cmd, parentPath); err != nil {
		return err
	}

	p.Logger.Infof("Successfully added flag %s to command %s", o.FlagName, o.CommandName)

	return nil
}

func (o *AddFlagOptions) loadManifest(p *props.Props) (*generator.Manifest, error) {
	manifestPath := generator.ManifestPathFor(o.Path)

	if _, err := p.FS.Stat(manifestPath); os.IsNotExist(err) {
		return nil, errors.Wrapf(generator.ErrNotGoToolBaseProject, "at %q", o.Path)
	}

	return generator.DecodeManifestFile(p.FS, manifestPath)
}

func (o *AddFlagOptions) updateCommandFlag(cmd *generator.ManifestCommand) {
	found := false

	for i, f := range cmd.Flags {
		if f.Name == o.FlagName {
			cmd.Flags[i].Type = o.FlagType
			cmd.Flags[i].Description = generator.MultilineString(o.Description)
			cmd.Flags[i].Persistent = o.Persistent
			found = true

			break
		}
	}

	if !found {
		cmd.Flags = append(cmd.Flags, generator.ManifestFlag{
			Name:        o.FlagName,
			Type:        o.FlagType,
			Description: generator.MultilineString(o.Description),
			Persistent:  o.Persistent,
		})
	}
}

func (o *AddFlagOptions) saveManifest(p *props.Props, m *generator.Manifest, pathParts []string, cmd generator.ManifestCommand) error {
	if !updateCommandMetadataRecursive(&m.Commands, pathParts, cmd) {
		return errors.Newf("%w for command %s", ErrUpdateManifestFailed, o.CommandName)
	}

	const permission = 0o644

	return generator.MarshalManifestFile(p.FS, generator.ManifestPathFor(o.Path), m, os.FileMode(permission))
}

func (o *AddFlagOptions) regenerateCommand(ctx context.Context, p *props.Props, cmd *generator.ManifestCommand, parentPath []string) error {
	// Regenerate through the same full-record mapping the `regenerate project`
	// path uses. Building a partial CommandData here (as an earlier version did)
	// silently dropped the command's aliases, persistent/required/shorthand
	// flags, and pre-run hooks, and never wrote the refreshed cmd.go hash back
	// to the manifest. RegenerateCommand populates CommandData from the full
	// manifest command record and persists the new hash via the shared
	// post-generation pipeline.
	gen := generator.New(p, &generator.Config{Path: o.Path})

	if err := gen.RegenerateCommand(ctx, *cmd, parentPath); err != nil {
		return errors.Newf("failed to regenerate command files: %w", err)
	}

	return nil
}

func findCommand(commands []generator.ManifestCommand, path []string, currentPath []string) (*generator.ManifestCommand, []string, error) {
	if len(path) == 0 {
		return nil, nil, ErrEmptyCommandPath
	}

	head, tail := path[0], path[1:]

	for _, cmd := range commands {
		if cmd.Name == head {
			if len(tail) == 0 {
				return &cmd, currentPath, nil
			}

			return findCommand(cmd.Commands, tail, append(currentPath, cmd.Name))
		}
	}

	return nil, nil, errors.WithHintf(
		errors.Newf("%w: %s", ErrCommandNotFound, strings.Join(path, "/")),
		"Target nested subcommands with the full slash-delimited path, e.g. -c parent/%s. "+
			"A leaf name alone only resolves a top-level command (not a space- or dot-separated form).",
		head,
	)
}

func updateCommandMetadataRecursive(commands *[]generator.ManifestCommand, path []string, updatedCmd generator.ManifestCommand) bool {
	if len(path) == 0 {
		return false
	}

	head, tail := path[0], path[1:]

	for i := range *commands {
		if (*commands)[i].Name == head {
			if len(tail) == 0 {
				(*commands)[i] = updatedCmd

				return true
			}

			return updateCommandMetadataRecursive(&(*commands)[i].Commands, tail, updatedCmd)
		}
	}

	return false
}
