package generator

import (
	"context"
	"os"
	"path/filepath"

	"github.com/cockroachdb/errors"
	"github.com/dave/jennifer/jen"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator/templates"
)

// signingTrustKeysPath is the generated embed package, relative to the
// project root.
const signingTrustKeysPath = "internal/trustkeys/trustkeys.go"

// signingGoPath is the generated enforcement-defaults file, relative to
// the project root. It lives beside the templated root command.
const signingGoPath = "pkg/cmd/root/signing.go"

// signingKeysDir is the author-owned key directory, relative to the
// project root. The generator writes only its .gitkeep; the author drops
// minted *.asc files alongside.
const signingKeysDir = "internal/trustkeys/keys"

// writeGeneratedGoFile renders a jen file to path under the project root,
// creating parent directories as needed. Used for the signing-related Go
// files that are emitted outside the main skeleton/regenerate map.
func (g *Generator) writeGeneratedGoFile(relPath string, f *jen.File) error {
	fullPath := filepath.Join(g.config.Path, relPath)

	if err := g.props.FS.MkdirAll(filepath.Dir(fullPath), DefaultDirMode); err != nil {
		return errors.Newf("failed to create directory %s: %w", filepath.Dir(fullPath), err)
	}

	out, err := g.props.FS.Create(fullPath)
	if err != nil {
		return errors.Newf("failed to create file %s: %w", fullPath, err)
	}

	defer func() { _ = out.Close() }()

	if err := f.Render(out); err != nil {
		return errors.Newf("failed to render %s: %w", relPath, err)
	}

	return nil
}

// syncSigningFiles brings the signing-owned files into line with the
// manifest's signing posture. When signing is enabled it writes the
// internal/trustkeys package, the keys/.gitkeep and the generated
// signing.go; when disabled it removes the generated signing.go but
// never deletes the internal/trustkeys directory or any author-added
// *.asc keys (the generator never deletes author content).
//
// It does NOT re-render pkg/cmd/root/cmd.go — that file carries the
// Signing: field and is the caller's responsibility (full regenerate
// re-renders it via regenerateRootCommand; the enable/disable commands
// call regenerateRootCommand directly).
func (g *Generator) syncSigningFiles(m Manifest) error {
	if !m.Properties.Signing.Enabled {
		return g.removeGeneratedSigningGo()
	}

	if err := g.writeGeneratedGoFile(signingTrustKeysPath, templates.SkeletonTrustKeys()); err != nil {
		return err
	}

	if err := g.writeGitkeep(filepath.Join(g.config.Path, signingKeysDir)); err != nil {
		return err
	}

	signingFile := templates.SkeletonSigning(templates.SkeletonSigningData{
		ExternalKeyEmail:          m.Properties.Signing.ExternalKeyEmail,
		RequireSignature:          m.Properties.Signing.RequireSignature,
		KeySource:                 m.Properties.Signing.KeySource,
		RequireExternalCrosscheck: m.Properties.Signing.RequireExternalCrosscheck,
	})

	return g.writeGeneratedGoFile(signingGoPath, signingFile)
}

// removeGeneratedSigningGo deletes the generated pkg/cmd/root/signing.go
// if present. It deliberately leaves internal/trustkeys and any author
// *.asc keys in place.
func (g *Generator) removeGeneratedSigningGo() error {
	fullPath := filepath.Join(g.config.Path, signingGoPath)

	if exists, _ := afero.Exists(g.props.FS, fullPath); !exists {
		return nil
	}

	if err := g.props.FS.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return errors.Newf("failed to remove %s: %w", signingGoPath, err)
	}

	return nil
}

// EnableSigning sets the manifest signing posture, then selectively
// regenerates only the signing-affected files: it writes the trustkeys
// package, keys/.gitkeep and signing.go, and re-renders the root command
// (pkg/cmd/root/cmd.go) to add the Signing: field. It does not run a full
// regenerate — unrelated files are left untouched.
func (g *Generator) EnableSigning(ctx context.Context, signing ManifestSigning) error {
	signing.Enabled = true

	return g.applySigningPosture(ctx, signing)
}

// DisableSigning sets signing.enabled = false in the manifest and
// selectively regenerates: it drops the Signing: field from the root
// command and removes the generated signing.go. It never deletes
// internal/trustkeys or any author-added *.asc.
func (g *Generator) DisableSigning(ctx context.Context) error {
	m, err := g.loadManifest()
	if err != nil {
		return err
	}

	signing := m.Properties.Signing
	signing.Enabled = false

	return g.applySigningPosture(ctx, signing)
}

// applySigningPosture persists the given signing block to the manifest
// and selectively regenerates the signing-affected files plus the root
// command. Shared by EnableSigning and DisableSigning.
func (g *Generator) applySigningPosture(ctx context.Context, signing ManifestSigning) error {
	if err := g.verifyProject(); err != nil {
		return err
	}

	m, err := g.loadManifest()
	if err != nil {
		return err
	}

	m.Properties.Signing = signing

	if err := g.writeManifest(m); err != nil {
		return err
	}

	// Re-render the one root-command file so the Signing: field is added
	// (enable) or dropped (disable). regenerateRootCommand reads the
	// manifest values we just persisted via buildSkeletonRootData.
	if err := g.regenerateRootCommand(*m); err != nil {
		return err
	}

	if err := g.syncSigningFiles(*m); err != nil {
		return err
	}

	// On a real filesystem, tidy modules and format/fix the touched files
	// so the embed import that trustkeys.go introduces (enable) or the
	// dropped signing.go (disable) leaves a building, lint-clean tree.
	if _, ok := g.props.FS.(*afero.OsFs); ok {
		g.runSkeletonPostProcessing(ctx, g.config.Path)
	}

	return nil
}

// writeManifest serialises a manifest back to .gtb/manifest.yaml,
// preserving the project's two-space indentation.
func (g *Generator) writeManifest(m *Manifest) error {
	manifestPath := filepath.Join(g.config.Path, ".gtb", "manifest.yaml")

	f, err := g.props.FS.Create(manifestPath)
	if err != nil {
		return errors.Newf("failed to open manifest for writing: %w", err)
	}

	defer func() { _ = f.Close() }()

	enc := yaml.NewEncoder(f)

	const indent = 2

	enc.SetIndent(indent)

	if err := enc.Encode(m); err != nil {
		return errors.Newf("failed to write manifest: %w", err)
	}

	return nil
}
