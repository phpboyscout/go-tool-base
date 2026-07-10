package generator

import (
	"context"
	"path/filepath"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
)

func (g *Generator) Remove(ctx context.Context) error {
	if err := g.verifyProject(); err != nil {
		return err
	}

	cmdDir, err := g.getCommandPath()
	if err != nil {
		return err
	}

	g.props.Logger.Info("removing command", "name", g.config.Name, "path", cmdDir)

	if err := g.performRemoval(cmdDir); err != nil {
		return err
	}

	g.cleanupDocumentation()

	// Also regenerate indices
	if err := g.generateCommandsIndex(); err != nil {
		g.props.Logger.Warn("failed to regenerate commands index", "error", err)
	}

	if err := g.regenerateMkdocsNav(); err != nil {
		g.props.Logger.Warn("failed to regenerate mkdocs navigation", "error", err)
	}

	g.props.Logger.Info("successfully removed command", "name", g.config.Name)

	return nil
}

func (g *Generator) performRemoval(cmdDir string) error {
	// 1. Deregister from parent
	if err := g.deregisterSubcommand(); err != nil {
		g.props.Logger.Warn("failed to deregister subcommand", "error", err)
	} else if err := g.updateParentCmdHash(); err != nil {
		g.props.Logger.Warn("failed to update parent command hash after deregistration", "error", err)
	}

	// 2. Remove from manifest
	if err := g.removeFromManifest(); err != nil {
		return err
	}

	// 3. Delete command directory
	if err := g.props.FS.RemoveAll(cmdDir); err != nil {
		return errors.Newf("failed to remove command directory: %w", err)
	}

	return nil
}

func (g *Generator) cleanupDocumentation() {
	// 4. Delete documentation
	promptParentParts, _ := g.FindCommandParentPath(g.config.Name)

	outRelPath := g.config.Name
	if len(promptParentParts) > 0 {
		outRelPath = filepath.Join(filepath.Join(promptParentParts...), g.config.Name)
	}

	docDir := filepath.Join(g.config.Path, "docs", "commands", outRelPath)
	if exists, _ := afero.Exists(g.props.FS, docDir); exists {
		if err := g.props.FS.RemoveAll(docDir); err != nil {
			g.props.Logger.Warn("failed to remove documentation directory", "error", err)
		}
	}
}
