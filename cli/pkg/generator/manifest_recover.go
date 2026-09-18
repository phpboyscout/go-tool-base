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

	// The Tool literal carries no backend (spec 0195 D2: the backend is the
	// manifest's), so the scan cannot rebuild it. With a manifest present its
	// own value is kept; from scratch it is derived below once the features
	// and type are known (#85).
	backend := m.ReleaseSource.Backend
	m.ReleaseSource = *rs
	m.ReleaseSource.Backend = backend

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

	// The backend is derived the way the derived-fields sync derives it for
	// an older manifest: from the one forge feature, else the release type.
	if backend, err := deriveForgeBackend(m); err == nil {
		m.ReleaseSource.Backend = backend
	} else {
		g.props.Logger.Warn("could not derive the forge backend from source", "error", err)
	}
}

// recoverNonLiteralProperties fills the ManifestProperties fields that are not
// carried in the cmd.go Tool literal, from other generated artefacts in the
// project. Called only on a from-scratch rebuild (no existing manifest); with a
// manifest present those fields are preserved as authored.
//
//   - the framework links (keychain, mcp): a delta feature entry when the
//     blank-import artefact cmd/<name>/<id>.go disagrees with the link's
//     default (the literal scanner never sees a link).
//   - docs_layout: inferred from the docs tree shape.
//   - CI component source: read from the scaffolded .gitlab-ci.yml include base.
//
// Hashes and template-overlay provenance are recovered by their own paths.
func (g *Generator) recoverNonLiteralProperties(props *ManifestProperties) {
	props.Features = g.recoverLinks(props.Features)

	// Canonical (name-sorted) order so the from-scratch feature list matches
	// generate's normalised form byte-for-byte.
	props.Features = sortManifestFeatures(props.Features)

	props.DocsLayout = g.recoverDocsLayout()

	if providers := g.recoverChatProviders(); providers != nil {
		props.Chat.Providers = providers
	}

	if src := g.recoverCIComponentSource(); src != "" {
		props.CI.ComponentSource = src
	}

	// Signing, template-overlay provenance, and module_published are not in the
	// generated source; recover them from the annotated provenance file.
	g.applyProvenanceFile(props)
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
