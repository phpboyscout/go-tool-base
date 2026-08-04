package generator

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator/templates"
)

// externalAdapterRelPath is the project-relative path of the author-owned
// external-command adapter scaffolded by AttachExternalAdapter.
const externalAdapterRelPath = "pkg/cmd/external/attach.go"

// ExternalCommandSpec is the input to AttachExternalCommand: one declarative
// external-module attachment to record. It mirrors the stored manifest form.
type ExternalCommandSpec = ManifestExternalCommand

// AttachExternalCommand records a declarative external-command attachment in the
// manifest, re-renders the root so the attach calls are wired in, and (on a real
// filesystem) pins the module require via `go get module@version` and tidies.
// The attachment is manifest-driven, so it survives every future regenerate /
// enable / disable — replacing the cmd/<tool>/main.go + .gtb/ignore workaround.
func (g *Generator) AttachExternalCommand(ctx context.Context, spec ExternalCommandSpec) error {
	if err := g.verifyProject(); err != nil {
		return err
	}

	if err := ValidateExternalCommand(&spec); err != nil {
		return err
	}

	m, err := g.loadManifest()
	if err != nil {
		return err
	}

	// Merge into an existing entry for the same module (so a second `attach
	// command` for the same module appends another constructor) or append a new
	// entry. A version mismatch is an error — detach first to change the pin.
	if idx := indexOfExternalModule(m.Properties.ExternalCommands, spec.Module); idx >= 0 {
		existing := &m.Properties.ExternalCommands[idx]
		if existing.Version != spec.Version {
			return errors.WithHintf(ErrInvalidInput,
				"external module %q is already attached at %s; detach it first to change the version to %s",
				spec.Module, existing.Version, spec.Version)
		}

		existing.Attach = append(existing.Attach, spec.Attach...)
	} else {
		m.Properties.ExternalCommands = append(m.Properties.ExternalCommands, spec)
	}

	// Re-validate the whole set so a cross-entry duplicate (module, constructor)
	// is caught before anything is written.
	if err := validateManifestExternalCommands(m.Properties.ExternalCommands); err != nil {
		return err
	}

	if err := g.regenerateRootCommand(*m); err != nil {
		return err
	}

	if err := g.writeManifest(m); err != nil {
		return err
	}

	g.pinExternalRequire(ctx, spec.Module, spec.Version)

	return nil
}

// indexOfExternalModule returns the index of the attachment entry for module,
// or -1 when the module is not yet attached.
func indexOfExternalModule(ecs []ManifestExternalCommand, module string) int {
	for i := range ecs {
		if ecs[i].Module == module {
			return i
		}
	}

	return -1
}

// pinExternalRequire pins the module require at version and tidies, but only on
// a real filesystem — on the in-memory FS used by tests there is no go tooling
// to run. A go-get failure is a warning: the attachment is already recorded and
// rendered; the operator can resolve the module manually.
func (g *Generator) pinExternalRequire(ctx context.Context, module, version string) {
	if _, ok := g.props.FS.(*afero.OsFs); !ok {
		return
	}

	if err := g.runSkeletonCommand(ctx, g.config.Path, "go", "get", module+"@"+version); err != nil {
		g.props.Logger.Warn("Failed to pin external module require", "module", module, "error", err)
	}

	g.runSkeletonPostProcessing(ctx, g.config.Path)
}

// AttachExternalAdapter scaffolds the author-owned adapter (pkg/cmd/external/
// attach.go) if absent — never overwriting an existing one — sets the adapter
// manifest flag, and re-renders the root to spread external.Commands(p) into
// NewCmdRoot. The author then fills in Commands to attach any shape the
// declarative vocabulary cannot express.
func (g *Generator) AttachExternalAdapter(ctx context.Context) error {
	if err := g.verifyProject(); err != nil {
		return err
	}

	m, err := g.loadManifest()
	if err != nil {
		return err
	}

	if err := g.scaffoldExternalAdapter(); err != nil {
		return err
	}

	m.Properties.ExternalCommandsAdapter = true

	if err := g.regenerateRootCommand(*m); err != nil {
		return err
	}

	if err := g.writeManifest(m); err != nil {
		return err
	}

	if _, ok := g.props.FS.(*afero.OsFs); ok {
		g.runSkeletonPostProcessing(ctx, g.config.Path)
	}

	return nil
}

// DetachExternalCommand removes the declarative attachment for the given module,
// re-renders the root (dropping its wiring), and — on a real filesystem — tidies
// so the now-unused require is pruned from go.mod.
func (g *Generator) DetachExternalCommand(ctx context.Context, module string) error {
	if err := g.verifyProject(); err != nil {
		return err
	}

	m, err := g.loadManifest()
	if err != nil {
		return err
	}

	kept := make([]ManifestExternalCommand, 0, len(m.Properties.ExternalCommands))
	found := false

	for _, ec := range m.Properties.ExternalCommands {
		if ec.Module == module {
			found = true

			continue
		}

		kept = append(kept, ec)
	}

	if !found {
		return errors.WithHintf(ErrInvalidInput,
			"no external command attachment for module %q; run 'attach list' to see declared attachments",
			module)
	}

	m.Properties.ExternalCommands = kept

	if err := g.regenerateRootCommand(*m); err != nil {
		return err
	}

	if err := g.writeManifest(m); err != nil {
		return err
	}

	if _, ok := g.props.FS.(*afero.OsFs); ok {
		// go mod tidy prunes the require now that the import is gone.
		g.runSkeletonPostProcessing(ctx, g.config.Path)
	}

	return nil
}

// ListExternalCommands returns the declared external-command attachments and
// whether the adapter channel is wired.
func (g *Generator) ListExternalCommands() ([]ManifestExternalCommand, bool, error) {
	m, err := g.loadManifest()
	if err != nil {
		return nil, false, err
	}

	return m.Properties.ExternalCommands, m.Properties.ExternalCommandsAdapter, nil
}

// scaffoldExternalAdapter writes the adapter seed file when absent. It is
// preserve-if-exists: an existing adapter (author-owned) is never overwritten,
// so re-running attach adapter — or a regenerate — keeps the author's Commands
// body intact.
func (g *Generator) scaffoldExternalAdapter() error {
	path := filepath.Join(g.config.Path, filepath.FromSlash(externalAdapterRelPath))

	if exists, _ := afero.Exists(g.props.FS, path); exists {
		return nil
	}

	if err := g.props.FS.MkdirAll(filepath.Dir(path), os.FileMode(DefaultDirMode)); err != nil {
		return errors.Newf("failed to create external adapter directory: %w", err)
	}

	if err := afero.WriteFile(g.props.FS, path, []byte(templates.SkeletonExternalAdapter()), os.FileMode(DefaultFileMode)); err != nil {
		return errors.Newf("failed to write external adapter: %w", err)
	}

	return nil
}

// buildSkeletonExternalCommands flattens the manifest's declarative
// external_commands block into the per-constructor render descriptors
// SkeletonRoot consumes. ImportPath defaults to the module path, and the import
// alias defaults to a sanitised base name when the entry does not pin one.
func buildSkeletonExternalCommands(ecs []ManifestExternalCommand) []templates.SkeletonExternalCommand {
	if len(ecs) == 0 {
		return nil
	}

	out := make([]templates.SkeletonExternalCommand, 0, len(ecs))

	for _, ec := range ecs {
		importPath := ec.ImportPath
		if importPath == "" {
			importPath = ec.Module
		}

		alias := externalPkgAlias(importPath, ec.Alias)

		for _, a := range ec.Attach {
			out = append(out, templates.SkeletonExternalCommand{
				ImportPath:  importPath,
				PkgAlias:    alias,
				Constructor: a.Constructor,
				Args:        a.Args,
				Wrap:        a.Wrap,
			})
		}
	}

	return out
}

// externalPkgAlias resolves the import alias for an external package: the
// operator-pinned alias when set, otherwise a Go-identifier-safe form of the
// import path's base segment (e.g. "signing-cli" → "signingcli"). Because the
// generated root always forces this alias via jen's ImportAlias, it need not
// match the package's declared name — only be a valid identifier used
// consistently for the import and its symbol references.
func externalPkgAlias(importPath, explicitAlias string) string {
	if explicitAlias != "" {
		return explicitAlias
	}

	base := importPath
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}

	s := sanitiseGoIdent(base)
	if s == "" || (s[0] >= '0' && s[0] <= '9') {
		s = "ext" + s
	}

	return s
}

// sanitiseGoIdent lowercases ASCII letters and drops any rune that cannot appear
// in a Go identifier (hyphens, dots), producing a candidate package alias.
func sanitiseGoIdent(s string) string {
	var b strings.Builder

	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		}
	}

	return b.String()
}

// externalFields serialises one external_commands entry for the provenance file.
// The nested attach list is packed into a single value (encodeKV percent-encodes
// it, so the internal separators survive the space-framed KV line); the whole
// block is not present in the generated source, so it must be recorded here for
// a from-scratch `regenerate manifest` to reconstruct it. The module path,
// version, import path, and identifiers are all gated by ValidateExternalCommand
// before they reach here.
func externalFields(ec ManifestExternalCommand) []kv {
	return []kv{
		{"module", ec.Module},
		{"version", ec.Version},
		{"import", ec.ImportPath},
		{"alias", ec.Alias},
		{"attach", encodeAttachList(ec.Attach)},
	}
}

// externalFromKV rebuilds an external_commands entry from a decoded provenance
// line.
func externalFromKV(m map[string]string) ManifestExternalCommand {
	return ManifestExternalCommand{
		Module:     m["module"],
		Version:    m["version"],
		ImportPath: m["import"],
		Alias:      m["alias"],
		Attach:     decodeAttachList(m["attach"]),
	}
}

// Attach-list packing separators. Tokens (a fixed lowercase vocabulary),
// constructors (Go identifiers), and command names (lowercase + hyphen) never
// contain these, so the packing is unambiguous; encodeKV percent-encodes the
// packed string so the separators also survive the KV framing.
const (
	attachEntrySep = ";"
	attachFieldSep = "|"
	attachArgSep   = ","
)

// Positions of the pipe-separated fields within one packed attach entry
// (see encodeAttachList). Named so the decode reads without magic indices.
const (
	attachFieldConstructor = 0
	attachFieldArgs        = 1
	attachFieldWrap        = 2
	attachFieldName        = 3
)

func encodeAttachList(attach []ManifestExternalAttach) string {
	parts := make([]string, 0, len(attach))

	for _, a := range attach {
		wrap := "0"
		if a.Wrap {
			wrap = "1"
		}

		parts = append(parts, strings.Join([]string{
			a.Constructor,
			strings.Join(a.Args, attachArgSep),
			wrap,
			a.Name,
		}, attachFieldSep))
	}

	return strings.Join(parts, attachEntrySep)
}

func decodeAttachList(s string) []ManifestExternalAttach {
	if s == "" {
		return nil
	}

	entries := strings.Split(s, attachEntrySep)
	out := make([]ManifestExternalAttach, 0, len(entries))

	for _, entry := range entries {
		fields := strings.Split(entry, attachFieldSep)

		field := func(i int) string {
			if i < len(fields) {
				return fields[i]
			}

			return ""
		}

		var args []string
		if a := field(attachFieldArgs); a != "" {
			args = strings.Split(a, attachArgSep)
		}

		out = append(out, ManifestExternalAttach{
			Constructor: field(attachFieldConstructor),
			Args:        args,
			Wrap:        field(attachFieldWrap) == "1",
			Name:        field(attachFieldName),
		})
	}

	return out
}
