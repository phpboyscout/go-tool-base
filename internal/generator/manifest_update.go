package generator

import (
	"os"
	"path/filepath"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"

	"github.com/phpboyscout/go-tool-base/internal/generator/templates"
)

// ManifestCommandUpdate carries all fields that updateCommandRecursive writes to
// a ManifestCommand entry.  Adding a new manifest field means adding it here
// rather than extending the function signature.
type ManifestCommandUpdate struct {
	Name             string
	Description      string
	LongDescription  string
	Aliases          []string
	Args             string
	Hashes           map[string]string
	Flags            []ManifestFlag
	WithAssets       bool
	WithInitializer  bool
	PersistentPreRun bool
	PreRun           bool
	Protected        *bool
	Hidden           bool
}

func (g *Generator) updateManifest(parsedFlags []templates.CommandFlag, hashes map[string]string) error {
	manifestPath := filepath.Join(g.config.Path, ".gtb", "manifest.yaml")

	g.props.Logger.Debugf("Updating manifest at %s for command %q", manifestPath, g.config.Name)

	data, err := afero.ReadFile(g.props.FS, manifestPath)
	if err != nil {
		return errors.Newf("failed to read manifest: %w", err)
	}

	var m Manifest

	if err := yaml.Unmarshal(data, &m); err != nil {
		return errors.Newf("failed to unmarshal manifest: %w", err)
	}

	mFlags := g.convertFlagsToManifest(parsedFlags)

	pathParts := g.getParentPathParts()

	// Update version
	if g.props.Version != nil {
		m.Version.GoToolBase = g.props.Version.GetVersion()
	}

	g.props.Logger.Debugf("Manifest update: parent=%v, flags=%d, hashes=%d", pathParts, len(mFlags), len(hashes))

	if len(pathParts) == 0 {
		g.updateRootCommand(&m, mFlags, hashes)
	} else if !updateCommandRecursive(&m.Commands, pathParts, ManifestCommandUpdate{
		Name:             g.config.Name,
		Description:      g.config.Short,
		LongDescription:  g.config.Long,
		Aliases:          g.config.Aliases,
		Args:             g.config.Args,
		Hashes:           hashes,
		WithAssets:       g.config.WithAssets,
		WithInitializer:  g.config.WithInitializer,
		PersistentPreRun: g.config.PersistentPreRun,
		PreRun:           g.config.PreRun,
		Protected:        g.config.Protected,
		Hidden:           g.config.Hidden,
		Flags:            mFlags,
	}) {
		return errors.Newf("%w: %s", ErrParentPathNotFound, g.config.Parent)
	}

	updated, err := yaml.Marshal(m)
	if err != nil {
		return errors.Newf("failed to marshal manifest: %w", err)
	}

	permission := DefaultFileMode

	if err := afero.WriteFile(g.props.FS, manifestPath, updated, os.FileMode(permission)); err != nil {
		return errors.Newf("failed to write manifest: %w", err)
	}

	g.props.Logger.Debugf("Manifest updated successfully at %s", manifestPath)

	return nil
}

func (g *Generator) updateRootCommand(m *Manifest, mFlags []ManifestFlag, hashes map[string]string) {
	found := false

	for i, cmd := range m.Commands {
		if cmd.Name == g.config.Name {
			m.Commands[i].Description = MultilineString(g.config.Short)
			m.Commands[i].LongDescription = MultilineString(g.config.Long)
			m.Commands[i].Aliases = g.config.Aliases
			m.Commands[i].Args = g.config.Args
			m.Commands[i].Hashes = hashes
			m.Commands[i].WithAssets = g.config.WithAssets
			m.Commands[i].WithInitializer = g.config.WithInitializer
			m.Commands[i].PersistentPreRun = g.config.PersistentPreRun
			m.Commands[i].PreRun = g.config.PreRun
			m.Commands[i].Hidden = g.config.Hidden

			m.Commands[i].Flags = mFlags
			if g.config.Protected != nil {
				m.Commands[i].Protected = g.config.Protected
			}

			found = true

			break
		}
	}

	if !found {
		m.Commands = append(m.Commands, ManifestCommand{
			Name:             g.config.Name,
			Description:      MultilineString(g.config.Short),
			LongDescription:  MultilineString(g.config.Long),
			Aliases:          g.config.Aliases,
			Args:             g.config.Args,
			Hidden:           g.config.Hidden,
			Flags:            mFlags,
			Hashes:           hashes,
			Protected:        g.config.Protected,
			WithAssets:       g.config.WithAssets,
			WithInitializer:  g.config.WithInitializer,
			PersistentPreRun: g.config.PersistentPreRun,
			PreRun:           g.config.PreRun,
		})
	}
}

func updateCommandRecursive(commands *[]ManifestCommand, parentPath []string, u ManifestCommandUpdate) bool {
	if len(parentPath) == 0 {
		return false
	}

	for i := range *commands {
		if (*commands)[i].Name == parentPath[0] {
			return handleCommandRecursiveUpdate(commands, i, parentPath, u)
		}
	}

	return false
}

func handleCommandRecursiveUpdate(commands *[]ManifestCommand, idx int, parentPath []string, u ManifestCommandUpdate) bool {
	if len(parentPath) == 1 {
		// Found the parent
		found := false

		for j, sub := range (*commands)[idx].Commands {
			if sub.Name == u.Name {
				(*commands)[idx].Commands[j].Description = MultilineString(u.Description)
				(*commands)[idx].Commands[j].LongDescription = MultilineString(u.LongDescription)
				(*commands)[idx].Commands[j].Aliases = u.Aliases
				(*commands)[idx].Commands[j].Args = u.Args
				(*commands)[idx].Commands[j].Hashes = u.Hashes
				(*commands)[idx].Commands[j].WithAssets = u.WithAssets
				(*commands)[idx].Commands[j].WithInitializer = u.WithInitializer
				(*commands)[idx].Commands[j].PersistentPreRun = u.PersistentPreRun
				(*commands)[idx].Commands[j].PreRun = u.PreRun
				(*commands)[idx].Commands[j].Hidden = u.Hidden
				(*commands)[idx].Commands[j].Flags = u.Flags

				if u.Protected != nil {
					(*commands)[idx].Commands[j].Protected = u.Protected
				}

				found = true

				break
			}
		}

		if !found {
			(*commands)[idx].Commands = append((*commands)[idx].Commands, ManifestCommand{
				Name:             u.Name,
				Description:      MultilineString(u.Description),
				LongDescription:  MultilineString(u.LongDescription),
				Aliases:          u.Aliases,
				Args:             u.Args,
				Hashes:           u.Hashes,
				WithAssets:       u.WithAssets,
				WithInitializer:  u.WithInitializer,
				PersistentPreRun: u.PersistentPreRun,
				PreRun:           u.PreRun,
				Hidden:           u.Hidden,
				Flags:            u.Flags,
				Protected:        u.Protected,
			})
		}

		return true
	}

	// Descend further
	return updateCommandRecursive(&(*commands)[idx].Commands, parentPath[1:], u)
}

// updateParentCmdHash reads the parent command's cmd.go after it has been
// modified by (de)registration and stores the new hash in the manifest.
// When the parent is root the file is not tracked in manifest commands, so
// the call is a no-op.
func (g *Generator) updateParentCmdHash() error {
	parentParts := g.getParentPathParts()
	if len(parentParts) == 0 {
		return nil
	}

	parentCmdFile := filepath.Join(g.config.Path, "pkg", "cmd", filepath.Join(parentParts...), "cmd.go")

	g.props.Logger.Debugf("Updating parent cmd hash for %s", parentCmdFile)

	content, err := afero.ReadFile(g.props.FS, parentCmdFile)
	if err != nil {
		return errors.Newf("failed to read parent cmd.go: %w", err)
	}

	hash := calculateHash(content)

	manifestPath := filepath.Join(g.config.Path, ".gtb", "manifest.yaml")

	data, err := afero.ReadFile(g.props.FS, manifestPath)
	if err != nil {
		return errors.Newf("failed to read manifest: %w", err)
	}

	var m Manifest

	if err := yaml.Unmarshal(data, &m); err != nil {
		return errors.Newf("failed to unmarshal manifest: %w", err)
	}

	if !updateCommandHashRecursive(&m.Commands, parentParts, hash) {
		return nil
	}

	updated, err := yaml.Marshal(m)
	if err != nil {
		return errors.Newf("failed to marshal manifest: %w", err)
	}

	return afero.WriteFile(g.props.FS, manifestPath, updated, DefaultFileMode)
}

func updateCommandHashRecursive(commands *[]ManifestCommand, path []string, hash string) bool {
	if len(path) == 0 {
		return false
	}

	for i := range *commands {
		if (*commands)[i].Name != path[0] {
			continue
		}

		if len(path) == 1 {
			if (*commands)[i].Hashes == nil {
				(*commands)[i].Hashes = make(map[string]string)
			}

			(*commands)[i].Hashes["cmd.go"] = hash

			return true
		}

		return updateCommandHashRecursive(&(*commands)[i].Commands, path[1:], hash)
	}

	return false
}
