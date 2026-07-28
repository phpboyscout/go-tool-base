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

	// Refuse to delete a protected command unless --force is given: the operator
	// marked it protected precisely because it carries hand-written logic that
	// RemoveAll would destroy (main.go included). Mirrors SetMCPEnabled.
	if err := g.checkRemovalProtection(); err != nil {
		return err
	}

	// Resolve the doc path while the command is still in the manifest — the
	// Diátaxis parent/leaf and has-children resolution depends on it, and
	// performRemoval deletes the entry.
	docPath := g.resolveCommandDocPath(cmdDir)

	g.props.Logger.Info("removing command", "name", g.config.Name, "path", cmdDir)

	if err := g.performRemoval(cmdDir); err != nil {
		return err
	}

	g.cleanupDocumentation(docPath)

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

// checkRemovalProtection refuses removal of a command the manifest marks
// Protected, unless the operator passed --force (g.config.Force). A command
// absent from the manifest, or one with no protection flag, removes freely.
func (g *Generator) checkRemovalProtection() error {
	if g.config.Force {
		return nil
	}

	cmd, err := g.findManifestCommand()
	if err != nil {
		// Not in the manifest (or unreadable): nothing to protect — let the
		// downstream removal steps surface any real error.
		return nil //nolint:nilerr // absence is not a protection failure
	}

	if cmd.Protected != nil && *cmd.Protected {
		return errors.Wrapf(ErrCommandProtected, "refusing to remove %q; pass --force to override", g.config.Name)
	}

	return nil
}

// resolveCommandDocPath returns the absolute documentation output path for the
// command being removed, resolved through the same prepareDocsContext machinery
// generation uses so it honours the project's docs layout (Diátaxis or legacy
// flat) and the real parent path. It MUST be called before the command is
// deleted from the manifest. Returns "" when the path cannot be resolved.
func (g *Generator) resolveCommandDocPath(cmdDir string) string {
	relPath, err := filepath.Rel(g.config.Path, cmdDir)
	if err != nil {
		relPath = ""
	}

	_, docPath := g.prepareDocsContext(g.config.Name, relPath, false)

	return docPath
}

func (g *Generator) cleanupDocumentation(docPath string) {
	if docPath == "" {
		return
	}

	// A leaf's Diátaxis doc is a single <path>.md file; a command whose doc is
	// <path>/index.md (flat layout, or a Diátaxis command with children) owns
	// the enclosing directory, so remove that whole directory with the subtree.
	target := docPath
	if filepath.Base(docPath) == "index.md" {
		target = filepath.Dir(docPath)
	}

	if exists, _ := afero.Exists(g.props.FS, target); exists {
		if err := g.props.FS.RemoveAll(target); err != nil {
			g.props.Logger.Warn("failed to remove documentation", "path", target, "error", err)
		}
	}
}
