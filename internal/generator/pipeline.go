package generator

import (
	"context"
	"path/filepath"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"

	"github.com/phpboyscout/go-tool-base/internal/generator/templates"
)

// StepWarning records a non-fatal failure within a pipeline step.
type StepWarning struct {
	Step string
	Err  error
}

// PipelineResult is returned by CommandPipeline.Run and carries any advisory
// warnings accumulated during execution.  A non-empty Warnings slice does not
// indicate overall failure — the pipeline continued past those steps.
type PipelineResult struct {
	Warnings []StepWarning
}

func (r *PipelineResult) warn(step string, err error) {
	if err != nil {
		r.Warnings = append(r.Warnings, StepWarning{Step: step, Err: err})
	}
}

// PipelineOptions controls which steps CommandPipeline executes.
// The zero value enables all steps.
type PipelineOptions struct {
	SkipAssets        bool // do not generate asset files
	SkipDocumentation bool // do not run documentation generation
	SkipRegistration  bool // do not modify the parent cmd.go
}

// CommandPipeline owns the ordered post-generation steps that are shared by
// both generate command and regenerate project.  Constructing a pipeline and
// calling Run centralises all registration, hash, manifest, and documentation
// logic so a fix in one place applies to both entrypoints.
type CommandPipeline struct {
	g    *Generator
	opts PipelineOptions
}

func newCommandPipeline(g *Generator, opts PipelineOptions) *CommandPipeline {
	return &CommandPipeline{g: g, opts: opts}
}

// Run executes the pipeline steps in order for the given command data and
// directory.  Fatal steps (asset generation) return an error immediately.
// Advisory steps (registration, manifest) log a warning and accumulate into
// PipelineResult so callers can inspect partial failures.
func (p *CommandPipeline) Run(ctx context.Context, data templates.CommandData, cmdDir string) (PipelineResult, error) {
	var result PipelineResult

	// ── Step 1: asset files (fatal — missing assets break the build) ─────────
	switch {
	case !p.opts.SkipAssets && p.g.config.WithAssets:
		p.g.props.Logger.Info("Generating asset files...")

		if err := p.g.generateAssetFiles(cmdDir); err != nil {
			return result, err
		}
	case p.opts.SkipAssets:
		p.g.props.Logger.Debug("Skipping asset file generation (SkipAssets=true)")
	case !p.g.config.WithAssets:
		p.g.props.Logger.Debug("Skipping asset file generation (WithAssets=false)")
	}

	// ── Steps 2+3: parent registration + child re-registration (advisory) ────
	p.runRegistrationSteps(data, cmdDir, &result)

	// ── Step 4: persist manifest (advisory) ──────────────────────────────────
	p.g.props.Logger.Info("Updating manifest.yaml...")

	allFlags := append([]templates.CommandFlag{}, data.Flags...)
	allFlags = append(allFlags, data.PersistentFlags...)

	if err := p.g.updateManifest(allFlags, data.Hashes); err != nil {
		p.g.props.Logger.Warnf("Failed to update manifest: %v", err)
		result.warn("persistManifest", err)
	}

	// ── Step 5: documentation (always advisory — never returns an error) ──────
	if !p.opts.SkipDocumentation {
		p.g.props.Logger.Info("Generating documentation...")
		p.g.handleDocumentationGeneration(ctx, data, cmdDir)
	} else {
		p.g.props.Logger.Debug("Skipping documentation generation (SkipDocumentation=true)")
	}

	p.g.props.Logger.Debugf("Pipeline complete: %d warnings", len(result.Warnings))

	return result, nil
}

// runRegistrationSteps handles parent registration and child re-registration
// as a single extracted method to keep Run within the cyclomatic complexity
// limit.
func (p *CommandPipeline) runRegistrationSteps(data templates.CommandData, cmdDir string, result *PipelineResult) {
	if p.opts.SkipRegistration {
		p.g.props.Logger.Debug("Skipping parent registration (SkipRegistration=true)")
	}

	if !p.opts.SkipRegistration {
		p.g.props.Logger.Infof("Registering subcommand %q...", data.Name)

		if err := p.g.registerSubcommand(); err != nil {
			p.g.props.Logger.Warnf("Failed to register subcommand %q: %v", data.Name, err)
			result.warn("registerInParent", err)
		} else if err := p.g.updateParentCmdHash(); err != nil {
			p.g.props.Logger.Warnf("Failed to update parent command hash: %v", err)
			result.warn("updateParentCmdHash", err)
		}
	}

	p.g.props.Logger.Infof("Re-registering child commands for %q...", data.Name)

	if err := p.g.reRegisterChildCommands(cmdDir, data.Hashes); err != nil {
		p.g.props.Logger.Warnf("Failed to re-register child commands for %q: %v", data.Name, err)
		result.warn("reRegisterChildren", err)
	}
}

// reRegisterChildCommands re-injects AddCommand calls for any children of the
// current command that are already recorded in the manifest.  This preserves
// child registrations when cmd.go is overwritten by a regeneration.  After all
// children are re-registered the cmd.go hash in hashes is refreshed to reflect
// the modified file content.
func (g *Generator) reRegisterChildCommands(cmdDir string, hashes map[string]string) error {
	m, err := g.loadManifest()
	if err != nil {
		return nil //nolint:nilerr // no manifest yet — nothing to preserve
	}

	parentParts := g.getParentPathParts()
	cmd := findCommandAt(m.Commands, parentParts, g.config.Name)

	if cmd == nil || len(cmd.Commands) == 0 {
		return nil
	}

	// The child's parent path is: parentParts + [current command name]
	childParentPath := append(append([]string{}, parentParts...), g.config.Name)

	for _, child := range cmd.Commands {
		// Use buildCommandContext so the child generator carries full project
		// settings (Path, Force, UpdateDocs) rather than a bare minimal Config.
		childCtx := buildCommandContext(g.config.Path, g.config.DryRun, g.config.Force, g.config.UpdateDocs, child, childParentPath)
		childGen := New(g.props, childCtx.ToConfig())

		if err := childGen.registerSubcommand(); err != nil {
			g.props.Logger.Warnf("Failed to re-register child %q under %q: %v", child.Name, g.config.Name, err)
		}
	}

	// Recompute cmd.go hash after modifications by child registrations.
	cmdFile := filepath.Join(cmdDir, "cmd.go")

	content, err := afero.ReadFile(g.props.FS, cmdFile)
	if err != nil {
		return errors.Newf("failed to read cmd.go after child re-registration: %w", err)
	}

	hashes["cmd.go"] = calculateHash(content)

	return nil
}
