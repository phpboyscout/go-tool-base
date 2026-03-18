package generate

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/phpboyscout/gtb/internal/generator"
	"github.com/phpboyscout/gtb/internal/generator/templates"
	"github.com/phpboyscout/gtb/pkg/props"
	"github.com/phpboyscout/gtb/pkg/utils"
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
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.ValidateOrPrompt(p); err != nil {
				return err
			}

			return opts.Run(cmd.Context(), p)
		},
	}

	cmd.Flags().StringVarP(&opts.CommandName, "command", "c", "", "Command name to add the flag to")
	cmd.Flags().StringVarP(&opts.FlagName, "name", "n", "", "Flag name")
	cmd.Flags().StringVarP(&opts.FlagType, "type", "t", "string", "Flag type (string, bool, int, float64, stringSlice, intSlice)")
	cmd.Flags().StringVarP(&opts.Description, "description", "d", "", "Flag description")
	cmd.Flags().BoolVar(&opts.Persistent, "persistent", false, "Make the flag persistent")
	cmd.Flags().StringVarP(&opts.Path, "path", "p", ".", "Path to project root")

	return cmd
}

func (o *AddFlagOptions) ValidateOrPrompt(p *props.Props) error {
	if o.CommandName != "" && o.FlagName != "" {
		return nil
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

func (o *AddFlagOptions) Run(ctx context.Context, p *props.Props) error {
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
	manifestPath := filepath.Join(o.Path, ".gtb", "manifest.yaml")

	if _, err := p.FS.Stat(manifestPath); os.IsNotExist(err) {
		return nil, errors.Newf("%w (.gtb/manifest.yaml not found)", generator.ErrNotGoToolBaseProject)
	}

	data, err := afero.ReadFile(p.FS, manifestPath)
	if err != nil {
		return nil, errors.Newf("failed to read manifest: %w", err)
	}

	var m generator.Manifest

	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, errors.Newf("failed to unmarshal manifest: %w", err)
	}

	return &m, nil
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

	updated, err := yaml.Marshal(m)
	if err != nil {
		return errors.Newf("failed to marshal manifest: %w", err)
	}

	manifestPath := filepath.Join(o.Path, ".gtb", "manifest.yaml")
	permission := 0644

	if err := afero.WriteFile(p.FS, manifestPath, updated, os.FileMode(permission)); err != nil {
		return errors.Newf("failed to write manifest: %w", err)
	}

	return nil
}

func (o *AddFlagOptions) regenerateCommand(ctx context.Context, p *props.Props, cmd *generator.ManifestCommand, parentPath []string) error {
	cmdDir := filepath.Join(o.Path, "pkg", "cmd", filepath.Join(parentPath...), cmd.Name)

	templateFlags := make([]templates.CommandFlag, 0, len(cmd.Flags))

	for _, f := range cmd.Flags {
		templateFlags = append(templateFlags, templates.CommandFlag{
			Name:        f.Name,
			Type:        f.Type,
			Description: string(f.Description),
			Persistent:  f.Persistent,
		})
	}

	tData := templates.CommandData{
		Name:       cmd.Name,
		PascalName: generator.PascalCase(cmd.Name),
		Short:      string(cmd.Description),
		Long:       string(cmd.LongDescription),
		Package:    strings.ReplaceAll(cmd.Name, "-", "_"),
		WithAssets: cmd.WithAssets,
		Flags:      templateFlags,
	}

	gen := generator.New(p, &generator.Config{})

	if err := gen.GenerateCommandFile(ctx, cmdDir, &tData); err != nil {
		return errors.Newf("failed to regenerate command files: %w", err)
	}

	return nil
}

func findCommand(commands []generator.ManifestCommand, path []string, currentPath []string) (*generator.ManifestCommand, []string, error) {
	if len(path) == 0 {
		return nil, nil, ErrEmptyCommandPath
	}

	for _, cmd := range commands {
		if cmd.Name == path[0] {
			if len(path) == 1 {
				return &cmd, currentPath, nil
			}

			return findCommand(cmd.Commands, path[1:], append(currentPath, cmd.Name))
		}
	}

	return nil, nil, errors.Newf("%w: %s", ErrCommandNotFound, strings.Join(path, "/"))
}

func updateCommandMetadataRecursive(commands *[]generator.ManifestCommand, path []string, updatedCmd generator.ManifestCommand) bool {
	if len(path) == 0 {
		return false
	}

	for i := range *commands {
		if (*commands)[i].Name == path[0] {
			if len(path) == 1 {
				(*commands)[i] = updatedCmd

				return true
			}

			return updateCommandMetadataRecursive(&(*commands)[i].Commands, path[1:], updatedCmd)
		}
	}

	return false
}
