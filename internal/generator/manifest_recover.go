package generator

import (
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
)

// applyRecoveredProperties refreshes the manifest's properties from the root
// cmd.go. With a manifest present it stays authoritative for author-set fields
// and only the cmd.go source-of-truth fields are refreshed; on a from-scratch
// rebuild the full property set is reconstructed from source.
func (g *Generator) applyRecoveredProperties(m *Manifest, manifestExisted bool) {
	rootCmdPath := filepath.Join(g.config.Path, "pkg", "cmd", "root", "cmd.go")

	mProps, rs, err := g.extractProjectProperties(rootCmdPath)
	if err != nil {
		g.props.Logger.Warn("could not extract project properties from root cmd.go", "error", err)

		return
	}

	m.ReleaseSource = *rs

	if manifestExisted {
		m.Properties.Name = mProps.Name
		m.Properties.Description = mProps.Description

		// Fall back to the AST feature set only if the manifest recorded none.
		if len(m.Properties.Features) == 0 {
			m.Properties.Features = mProps.Features
		}

		return
	}

	// From scratch: reconstruct the full property set — the cmd.go Tool literal
	// (name, description, features, env prefix, update policy/interval, help,
	// telemetry, bootstrap) plus the non-literal recoveries below.
	m.Properties = *mProps
	g.recoverNonLiteralProperties(&m.Properties)
}

// recoverNonLiteralProperties fills the ManifestProperties fields that are not
// carried in the cmd.go Tool literal, from other generated artefacts in the
// project. Called only on a from-scratch rebuild (no existing manifest); with a
// manifest present those fields are preserved as authored.
//
//   - keychain: a delta feature entry, from the presence of the blank-import
//     artefact cmd/<name>/keychain.go (it has no FeatureCmd, so the literal
//     scanner never sees it).
//   - docs_layout: inferred from the docs tree shape.
//   - CI component source: read from the scaffolded .gitlab-ci.yml include base.
//
// Hashes and template-overlay provenance are recovered by their own paths.
func (g *Generator) recoverNonLiteralProperties(props *ManifestProperties) {
	if g.keychainArtefactExists() {
		// keychain defaults off; its presence is a delta entry, matching what
		// generate writes (normaliseManifestFeatures keeps it).
		props.Features = append(props.Features, ManifestFeature{Name: "keychain", Enabled: true})
	}

	// Canonical (name-sorted) order so the from-scratch feature list matches
	// generate's normalised form byte-for-byte.
	props.Features = sortManifestFeatures(props.Features)

	props.DocsLayout = g.recoverDocsLayout()

	if src := g.recoverCIComponentSource(); src != "" {
		props.CI.ComponentSource = src
	}
}

// keychainArtefactExists reports whether the scaffolded keychain blank-import
// file (cmd/<name>/keychain.go) is present, which is how the keychain feature is
// enabled.
func (g *Generator) keychainArtefactExists() bool {
	matches, err := afero.Glob(g.props.FS, filepath.Join(g.config.Path, "cmd", "*", "keychain.go"))

	return err == nil && len(matches) > 0
}

// recoverDocsLayout infers the docs layout from the generated tree: the Diátaxis
// layout writes command docs under docs/reference/cli, the legacy flat layout
// under docs/commands. Empty is treated as flat downstream, so a project with
// neither marker returns the flat sentinel unset only when the diataxis tree is
// absent.
func (g *Generator) recoverDocsLayout() string {
	if ok, _ := afero.DirExists(g.props.FS, filepath.Join(g.config.Path, "docs", "reference", "cli")); ok {
		return DocsLayoutDiataxis
	}

	if ok, _ := afero.DirExists(g.props.FS, filepath.Join(g.config.Path, "docs", "commands")); ok {
		return DocsLayoutFlat
	}

	// No recognisable docs tree: leave empty (treated as flat for backward
	// compatibility, matching ResolvedDocsLayout).
	return ""
}

// recoverCIComponentSource reads the phpboyscout/cicd include base from the
// scaffolded .gitlab-ci.yml. It returns "" when the base is the framework
// default (DefaultCICDComponentSource) so the manifest stays minimal, matching
// how generate records only a non-default override.
func (g *Generator) recoverCIComponentSource() string {
	data, err := afero.ReadFile(g.props.FS, filepath.Join(g.config.Path, ".gitlab-ci.yml"))
	if err != nil {
		return ""
	}

	base := ciComponentBase(string(data))
	if base == "" || base == DefaultCICDComponentSource {
		return ""
	}

	return base
}

// ciComponentBase extracts the include base from the first `component:` include
// line — everything before the trailing `/<component-name>@<version>`. For
// `component: gitlab.com/acme/cicd/go-lint@v1` it returns `gitlab.com/acme/cicd`.
func ciComponentBase(ci string) string {
	for _, line := range strings.Split(ci, "\n") {
		line = strings.TrimSpace(line)

		const marker = "component:"
		if !strings.HasPrefix(line, "- "+marker) && !strings.HasPrefix(line, marker) {
			continue
		}

		ref := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "- "), marker))
		ref, _, _ = strings.Cut(ref, "@") // drop @version

		if i := strings.LastIndex(ref, "/"); i > 0 {
			return ref[:i] // drop /<component-name>
		}
	}

	return ""
}
