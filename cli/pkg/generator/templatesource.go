package generator

// Custom template overlays. The generator walks every file in a user-supplied
// template source (a local folder or a cloned git repo) and renders each one
// through the existing text/template engine to the identical relative path in
// the generated project: a new path adds a file; a path that also exists in
// the embedded skeleton is overwritten (user wins). A source-side
// gtb-template.yaml descriptor can suppress whole embedded forge-CI scaffolds
// before the overlay renders.
//
// This is the riskiest surface in the generator: it parses and executes
// text/template content GTB did not author, for git sources fetched over the
// network. The escape-at-known-sites model in template_escape.go does NOT
// protect a third party's whole-file template — the author controls the
// output directly. The security posture here is trusted-source-with-bounded-
// blast-radius (NOT a sandbox): write-path containment under the project root,
// a protected-path denylist, a restricted FuncMap, a metadata-only data
// contract, and inert fetch. See
// https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0080-generator-custom-partial-templates and
// docs/development/template-security.md.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go/errors"
)

const (
	// templateDescriptorName is the reserved root meta file carrying the
	// source's replaces/contract declaration. It is never rendered.
	templateDescriptorName = "gtb-template.yaml"
	// templateReadmeName is the reserved root meta file (human explainer).
	// It is never rendered.
	templateReadmeName = "README.md"
	// supportedContractVersion is the highest data-contract version this
	// build understands. A descriptor targeting a higher version is rejected.
	supportedContractVersion = 1
	// maxTemplateFileBytes caps a single overlay source file so a
	// pathological template cannot exhaust memory at parse time (defence in
	// depth against the resource-exhaustion threat).
	maxTemplateFileBytes = 1 << 20 // 1 MiB
)

// TemplateDescriptor is the parsed gtb-template.yaml at a source root. It
// declares the data-contract version the set targets and the embedded
// scaffolds it suppresses before the overlay renders. Absent descriptor /
// empty Replaces ⇒ pure overlay (nothing suppressed).
type TemplateDescriptor struct {
	// Contract is the data-contract version the set targets. Zero is treated
	// as version 1 (the only version) for descriptors that omit it; any
	// value above supportedContractVersion is rejected.
	Contract int `yaml:"contract"`
	// Description is a human-facing summary of the template set.
	Description string `yaml:"description"`
	// Replaces names the embedded scaffolds this set supersedes. Each entry
	// must be a known alias in suppressibleAliases.
	Replaces []string `yaml:"replaces"`
}

// suppressibleAliases maps a gtb-template.yaml `replaces:` alias to the
// embedded asset path prefixes it suppresses before the overlay renders. The
// v1 named set is gitlab-ci and github-ci. GTB owns this map — a `replaces:`
// entry outside it is rejected at descriptor validation. The prefixes are the
// output-relative paths (mirroring walkSkeletonAssets' relPath), so the
// suppression set is matched against the same keys the overlay and embedded
// walk produce.
var suppressibleAliases = map[string][]string{
	"gitlab-ci": {
		".gitlab-ci.yml",
		".gitlab/",
		"renovate.json5",
	},
	"github-ci": {
		".github/",
	},
}

// TemplateContractData is the documented, stable, metadata-only and
// secret-free subset of skeletonTemplateData exposed to overlay templates. It
// deliberately excludes any resolved credential, env var, absolute host path,
// or forge token — the information-disclosure threat. Adding a field is a safe
// additive change; removing one is a contract-version bump.
type TemplateContractData struct {
	Name              string
	Description       string
	Repo              string
	Host              string
	Org               string
	RepoName          string
	ModulePath        string
	ReleaseProvider   string
	GoVersion         string
	GoToolBaseVersion string
	EnvPrefix         string
	EnabledFeatures   []string
	DisabledFeatures  []string
	Private           bool
	// SigningEnabled / HelpType expose only presence/shape, never secrets.
	SigningEnabled bool
	HelpType       string
}

// GetReleaseProvider satisfies releaseProviderAccessor.
func (d TemplateContractData) GetReleaseProvider() string { return d.ReleaseProvider }

// toOverlayContractData projects whatever template data the skeleton pipeline
// carries (always a skeletonTemplateData) onto the metadata-only overlay
// contract. A non-skeletonTemplateData value yields a zero contract — overlay
// templates then see only empty metadata, never anything secret.
func toOverlayContractData(data any) TemplateContractData {
	if d, ok := data.(skeletonTemplateData); ok {
		return toContractData(d)
	}

	return TemplateContractData{}
}

// toContractData projects the full skeletonTemplateData onto the metadata-only
// overlay contract, dropping every secret-bearing field (Slack/Teams handles
// are help shape, not secrets, but are still omitted to keep the contract
// minimal and stable).
func toContractData(d skeletonTemplateData) TemplateContractData {
	return TemplateContractData{
		Name:              d.Name,
		Description:       d.Description,
		Repo:              d.Repo,
		Host:              d.Host,
		Org:               d.Org,
		RepoName:          d.RepoName,
		ModulePath:        d.ModulePath,
		ReleaseProvider:   d.ReleaseProvider,
		GoVersion:         d.GoVersion,
		GoToolBaseVersion: d.GoToolBaseVersion,
		EnvPrefix:         d.EnvPrefix,
		EnabledFeatures:   d.EnabledFeatures,
		DisabledFeatures:  d.DisabledFeatures,
		Private:           d.Private,
		SigningEnabled:    d.Signing.Enabled,
		HelpType:          d.HelpType,
	}
}

// overlayFuncMap is the restricted FuncMap overlay templates render with. It
// exposes ONLY the existing pure escape helpers plus pure string/format
// helpers — nothing that reads files, runs commands, opens network
// connections, or reads the environment. text/template has no such builtins;
// the only risk is what GTB adds, so we add nothing dangerous.
var overlayFuncMap = template.FuncMap{
	"escapeYAML":              escapeYAML,
	"escapeMarkdown":          escapeMarkdown,
	"escapeMarkdownCodeBlock": escapeMarkdownCodeBlock,
	"escapeTOML":              escapeTOML,
	"escapeComment":           escapeComment,
	"escapeShellArg":          escapeShellArg,
	"lower":                   strings.ToLower,
	"upper":                   strings.ToUpper,
	"title":                   strings.Title, //nolint:staticcheck // pure ASCII title-casing for template prose; golang.org/x/text/cases is overkill for a scaffolding helper
	"trim":                    strings.TrimSpace,
	"replace":                 strings.ReplaceAll,
	"join":                    func(sep string, items []string) string { return strings.Join(items, sep) },
	"contains":                strings.Contains,
	"hasPrefix":               strings.HasPrefix,
	"hasSuffix":               strings.HasSuffix,
}

// protectedOverlayPaths are the security-critical output paths an overlay may
// NEVER write, even though it otherwise overwrites embedded files freely. A
// `replaces:` suppression cannot reach these either — they are denied
// unconditionally. Matching is by exact path or directory prefix.
var protectedOverlayPaths = []string{
	".gtb/",               // manifest + ignore
	"internal/trustkeys/", // signing trust anchors
	"go.mod",              // module graph
	"go.sum",              // module checksums
}

// isProtectedOverlayPath reports whether relPath (a clean, slash-separated,
// project-relative path) is on the protected denylist.
func isProtectedOverlayPath(relPath string) bool {
	rel := path.Clean(filepath.ToSlash(relPath))

	for _, p := range protectedOverlayPaths {
		if strings.HasSuffix(p, "/") {
			if rel == strings.TrimSuffix(p, "/") || strings.HasPrefix(rel, p) {
				return true
			}

			continue
		}

		if rel == p {
			return true
		}
	}

	return false
}

// containedOutputPath resolves relPath under projectRoot and verifies it sits
// strictly inside the project tree. It rejects absolute paths and any `..`
// escape (the path-traversal-on-write threat). It returns the cleaned
// project-relative path on success.
func containedOutputPath(projectRoot, relPath string) (string, error) {
	cleaned, err := cleanRelativeOutputPath(relPath)
	if err != nil {
		return "", err
	}

	// Belt-and-braces: resolve against the absolute project root and confirm
	// filepath.Rel does not climb out. This mirrors the getCommandPath
	// containment pattern.
	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", errors.Wrapf(err, "failed to resolve project root %q", projectRoot)
	}

	target := filepath.Join(absRoot, filepath.FromSlash(cleaned))

	rel, err := filepath.Rel(absRoot, target)
	if err != nil {
		return "", errors.Wrapf(err, "failed to relativise overlay path %q", relPath)
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.Newf("overlay path %q escapes the project root", relPath)
	}

	return cleaned, nil
}

// cleanRelativeOutputPath performs the syntactic containment checks: non-empty,
// relative (not absolute), and free of a leading `..` escape. It returns the
// cleaned slash path.
func cleanRelativeOutputPath(relPath string) (string, error) {
	if relPath == "" {
		return "", errors.New("empty overlay path")
	}

	slash := filepath.ToSlash(relPath)
	if path.IsAbs(slash) || filepath.IsAbs(relPath) {
		return "", errors.Newf("overlay path %q must be relative to the project root", relPath)
	}

	cleaned := path.Clean(slash)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || cleaned == "." {
		return "", errors.Newf("overlay path %q escapes the project root", relPath)
	}

	return cleaned, nil
}

// parseTemplateDescriptor reads and validates the gtb-template.yaml at the
// source root from srcFS. A missing descriptor is non-fatal (pure overlay):
// it returns a zero descriptor. A present-but-invalid descriptor is a hard
// error (the malformed-descriptor threat).
func parseTemplateDescriptor(srcFS afero.Fs, root string) (TemplateDescriptor, error) {
	descPath := filepath.Join(root, templateDescriptorName)

	exists, _ := afero.Exists(srcFS, descPath)
	if !exists {
		return TemplateDescriptor{}, nil
	}

	raw, err := afero.ReadFile(srcFS, descPath)
	if err != nil {
		return TemplateDescriptor{}, errors.Wrapf(err, "failed to read %s", templateDescriptorName)
	}

	var desc TemplateDescriptor
	if err := yaml.Unmarshal(raw, &desc); err != nil {
		return TemplateDescriptor{}, errors.Wrapf(err, "failed to parse %s", templateDescriptorName)
	}

	if err := validateTemplateDescriptor(&desc); err != nil {
		return TemplateDescriptor{}, err
	}

	return desc, nil
}

// validateTemplateDescriptor rejects an unknown contract version (O9) and any
// `replaces:` alias outside the maintained set (Security model). A zero
// contract is treated as version 1.
func validateTemplateDescriptor(desc *TemplateDescriptor) error {
	contract := desc.Contract
	if contract == 0 {
		contract = supportedContractVersion
	}

	if contract < 1 || contract > supportedContractVersion {
		return errors.WithHintf(
			errors.Newf("unsupported template contract version %d", desc.Contract),
			"this gtb build supports contract versions 1..%d; upgrade gtb or pin the template set to a supported version",
			supportedContractVersion,
		)
	}

	for _, alias := range desc.Replaces {
		if _, ok := suppressibleAliases[alias]; !ok {
			return errors.WithHintf(
				errors.Newf("unknown replaces alias %q in %s", alias, templateDescriptorName),
				"known aliases: %s", strings.Join(knownAliases(), ", "),
			)
		}
	}

	return nil
}

// knownAliases returns the sorted maintained replaces-alias set for error
// hints and docs.
func knownAliases() []string {
	out := make([]string, 0, len(suppressibleAliases))
	for k := range suppressibleAliases {
		out = append(out, k)
	}

	sort.Strings(out)

	return out
}

// suppressedPrefixes returns the embedded path prefixes a descriptor's
// `replaces:` list suppresses. A relPath is suppressed when it equals or is
// nested under any returned prefix.
func suppressedPrefixes(desc TemplateDescriptor) []string {
	var prefixes []string
	for _, alias := range desc.Replaces {
		prefixes = append(prefixes, suppressibleAliases[alias]...)
	}

	return prefixes
}

// isSuppressed reports whether an embedded output path relPath is suppressed
// by the given prefixes (file-exact or directory-prefix match).
func isSuppressed(relPath string, prefixes []string) bool {
	rel := filepath.ToSlash(relPath)

	for _, p := range prefixes {
		if strings.HasSuffix(p, "/") {
			if strings.HasPrefix(rel, p) {
				return true
			}

			continue
		}

		if rel == p {
			return true
		}
	}

	return false
}

// renderedOverlayFile is one rendered overlay output: its project-relative
// path and the rendered bytes.
type renderedOverlayFile struct {
	relPath string
	content []byte
}

// renderOverlay walks the template source tree under root in srcFS, renders
// every non-meta file through the restricted overlay engine against the
// metadata-only contract, and returns the rendered files keyed by mirrored
// project-relative path. The two reserved root meta files (README.md,
// gtb-template.yaml) are excluded. Each output path is contained under the
// project root and checked against the protected denylist before it is
// emitted — a path that escapes or hits the denylist is a hard error (no
// partial write). renderOverlay does NOT write to disk; the caller applies the
// returned files through the existing hash/conflict machinery.
func renderOverlay(srcFS afero.Fs, root, projectRoot string, data TemplateContractData) ([]renderedOverlayFile, error) {
	var out []renderedOverlayFile

	walkErr := afero.Walk(srcFS, root, func(p string, info fs.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		rendered, skip, fileErr := renderOverlayFile(srcFS, root, projectRoot, p, info, data)
		if fileErr != nil {
			return fileErr
		}

		if !skip {
			out = append(out, rendered)
		}

		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	// Deterministic order so layering/logging is stable.
	sort.Slice(out, func(i, j int) bool { return out[i].relPath < out[j].relPath })

	return out, nil
}

// renderOverlayFile validates, contains, and renders a single overlay source
// file. skip is true for a reserved root meta file (which is not emitted). All
// security checks (size bound, containment, denylist) run before any render.
func renderOverlayFile(srcFS afero.Fs, root, projectRoot, p string, info fs.FileInfo, data TemplateContractData) (rendered renderedOverlayFile, skip bool, err error) {
	relPath, relErr := filepath.Rel(root, p)
	if relErr != nil {
		return renderedOverlayFile{}, false, errors.Wrapf(relErr, "failed to relativise source file %q", p)
	}

	relSlash := filepath.ToSlash(relPath)

	// Exclude the two reserved root meta files from rendering.
	if relSlash == templateDescriptorName || relSlash == templateReadmeName {
		return renderedOverlayFile{}, true, nil
	}

	if info.Size() > maxTemplateFileBytes {
		return renderedOverlayFile{}, false, errors.Newf("overlay source file %q exceeds the %d-byte limit", relSlash, maxTemplateFileBytes)
	}

	// Containment + denylist BEFORE any render work.
	cleaned, contErr := containedOutputPath(projectRoot, relSlash)
	if contErr != nil {
		return renderedOverlayFile{}, false, contErr
	}

	if isProtectedOverlayPath(cleaned) {
		return renderedOverlayFile{}, false, errors.WithHintf(
			errors.Newf("overlay source may not write protected path %q", cleaned),
			"paths under %s are security-critical and can never be overwritten by a template overlay",
			strings.Join(protectedOverlayPaths, ", "),
		)
	}

	content, readErr := afero.ReadFile(srcFS, p)
	if readErr != nil {
		return renderedOverlayFile{}, false, errors.Wrapf(readErr, "failed to read overlay source file %q", relSlash)
	}

	out, rErr := renderOverlayTemplate(cleaned, string(content), data)
	if rErr != nil {
		return renderedOverlayFile{}, false, rErr
	}

	return renderedOverlayFile{relPath: cleaned, content: out}, false, nil
}

// renderOverlayTemplate parses and executes a single overlay template with the
// restricted FuncMap and the metadata-only contract.
func renderOverlayTemplate(relPath, tmplStr string, data TemplateContractData) ([]byte, error) {
	tmpl, err := template.New(relPath).Funcs(overlayFuncMap).Parse(tmplStr)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse overlay template %q", relPath)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, errors.Wrapf(err, "failed to execute overlay template %q", relPath)
	}

	return buf.Bytes(), nil
}

// fingerprintTree returns a stable content fingerprint of the template source
// tree under root in srcFS: SHA256 over each non-meta file's path and content,
// in sorted path order. Used for local sources where no git SHA pin exists, so
// regenerate can warn on drift (O5).
func fingerprintTree(srcFS afero.Fs, root string) (string, error) {
	type entry struct {
		rel     string
		content []byte
	}

	var entries []entry

	err := afero.Walk(srcFS, root, func(p string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}

		content, readErr := afero.ReadFile(srcFS, p)
		if readErr != nil {
			return readErr
		}

		entries = append(entries, entry{rel: filepath.ToSlash(rel), content: content})

		return nil
	})
	if err != nil {
		return "", errors.Wrap(err, "failed to fingerprint template source")
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })

	h := sha256.New()
	for _, e := range entries {
		_, _ = h.Write([]byte(e.rel))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(e.content)
		_, _ = h.Write([]byte{0})
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
