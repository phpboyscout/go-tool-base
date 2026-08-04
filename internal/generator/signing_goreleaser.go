package generator

import (
	"bytes"
	"io/fs"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go/errors"
)

// goreleaserSignsMarker is the head comment gtb writes above an injected
// signs: block (it is the first line of the skeleton's signs section). Its
// presence identifies a block gtb owns: disable removes only a marked block,
// and enable recognises a prior gtb injection. An author-written signs: block
// without this marker is never touched.
const goreleaserSignsMarker = "# Release signing: gtb enable signing wired this."

// applyGoreleaserSigns brings the .goreleaser.yaml signs: block into line with
// the manifest signing posture WITHOUT clobbering a customised release config.
// The precedence is deliberate and does not depend on the generator's Overwrite
// mode (an ignored path is protected even under Overwrite: "allow"):
//
//  1. Absent file  → render the whole skeleton asset (fresh-scaffold semantics;
//     there is no customisation to lose).
//  2. Ignored path → never write. Advise, and keep the on-disk hash tracked.
//  3. Unparseable  → never write. Advise, and keep the on-disk hash tracked.
//  4. Otherwise    → safe, structure-preserving injection (enable) or removal
//     (disable) of only the top-level signs: block, leaving all other content
//     byte-for-byte untouched. A signs: block already present (enable) or an
//     author-written signs: block (disable) degrades to an advisory.
//
// It records the resulting on-disk hash on m.Hashes for the caller to persist.
// A failure to perform the edit is never fatal — the rest of enable/disable
// signing (trustkeys scaffold, root-command wiring, manifest update) proceeds.
func (g *Generator) applyGoreleaserSigns(m *Manifest) error {
	relPath := goreleaserAssetRelPath
	fullPath := filepath.Join(g.config.Path, relPath)
	enable := m.Properties.Signing.Enabled && m.Properties.Signing.KeyID != ""

	exists, _ := afero.Exists(g.props.FS, fullPath)
	if !exists {
		// No file to preserve: render the full skeleton asset. This is the only
		// path that writes the whole skeleton, and it cannot lose customisation.
		return g.regenerateGoreleaserAsset(m)
	}

	// .gtb/ignore takes precedence over everything, including Overwrite:"allow".
	if LoadIgnoreRules(g.props.FS, g.config.Path).IsIgnored(relPath) {
		g.adviseGoreleaserSigns(relPath, m, enable, "it is listed in .gtb/ignore")
		g.rehashGoreleaserOnDisk(fullPath, relPath, m)

		return nil
	}

	content, err := afero.ReadFile(g.props.FS, fullPath)
	if err != nil {
		return errors.Newf("failed to read %s: %w", relPath, err)
	}

	root, ok := parseYAMLMapping(content)
	if !ok {
		// A file we cannot parse as a YAML mapping is not safe to splice.
		g.adviseGoreleaserSigns(relPath, m, enable, "it is not a parseable YAML mapping")
		g.rehashGoreleaserOnDisk(fullPath, relPath, m)

		return nil
	}

	hasSigns := topLevelKeyPresent(root, "signs")

	if enable {
		return g.enableGoreleaserSigns(fullPath, relPath, content, m, hasSigns)
	}

	return g.disableGoreleaserSigns(fullPath, relPath, content, m, hasSigns)
}

// enableGoreleaserSigns injects the top-level signs: block into a parseable,
// non-ignored .goreleaser.yaml, appending it as a new top-level key and leaving
// every other block untouched. If a signs: block is already present (author-
// written or a prior run) it does not touch the file — it advises instead, so
// an author-tuned block is never clobbered.
func (g *Generator) enableGoreleaserSigns(fullPath, relPath string, content []byte, m *Manifest, hasSigns bool) error {
	if hasSigns {
		g.adviseGoreleaserSigns(relPath, m, true, "a signs: block is already present")
		g.rehashGoreleaserOnDisk(fullPath, relPath, m)

		return nil
	}

	block, err := g.renderGoreleaserSignsBlock(m)
	if err != nil {
		g.adviseGoreleaserSigns(relPath, m, true, "the signs: block could not be rendered")

		return nil //nolint:nilerr // advisory degrade: the edit is non-fatal, the rest of enable proceeds
	}

	updated := appendTopLevelBlock(content, block)
	if err := afero.WriteFile(g.props.FS, fullPath, updated, DefaultFileMode); err != nil {
		return errors.Newf("failed to write %s: %w", relPath, err)
	}

	g.recordGoreleaserHash(relPath, updated, m)
	g.props.Logger.Info("Injected the release-signing signs: block into the release config.", "path", relPath)

	return nil
}

// disableGoreleaserSigns removes a gtb-injected signs: block from a parseable,
// non-ignored .goreleaser.yaml, leaving every other block untouched. It removes
// only a block gtb itself wrote (identified by goreleaserSignsMarker); an
// author-written signs: block is left in place with an advisory.
func (g *Generator) disableGoreleaserSigns(fullPath, relPath string, content []byte, m *Manifest, hasSigns bool) error {
	if !hasSigns {
		g.rehashGoreleaserOnDisk(fullPath, relPath, m)

		return nil
	}

	if !bytes.Contains(content, []byte(goreleaserSignsMarker)) {
		g.props.Logger.Warn(
			"Release config not modified: a signs: block is present but was not written by gtb — remove it manually to disable release signing.",
			"path", relPath)
		g.rehashGoreleaserOnDisk(fullPath, relPath, m)

		return nil
	}

	updated := removeGtbSignsBlock(content)
	if err := afero.WriteFile(g.props.FS, fullPath, updated, DefaultFileMode); err != nil {
		return errors.Newf("failed to write %s: %w", relPath, err)
	}

	g.recordGoreleaserHash(relPath, updated, m)
	g.props.Logger.Info("Removed the release-signing signs: block from the release config.", "path", relPath)

	return nil
}

// adviseGoreleaserSigns emits a fail-loud advisory when the release-config edit
// cannot be performed safely: it states the file was NOT modified and (on
// enable) prints the exact top-level signs: block for the user to paste, with
// the path. The rest of enable/disable signing still proceeds.
func (g *Generator) adviseGoreleaserSigns(relPath string, m *Manifest, enable bool, reason string) {
	if !enable {
		g.props.Logger.Warn(
			"Release config not modified — remove the release-signing signs: block manually to complete disable.",
			"path", relPath, "reason", reason)

		return
	}

	block, err := g.renderGoreleaserSignsBlock(m)
	if err != nil {
		g.props.Logger.Warn(
			"Release config not modified and the signs: block could not be rendered — enable signing manually.",
			"path", relPath, "reason", reason, "error", err)

		return
	}

	g.props.Logger.Warn(
		"Release config not modified — add the following top-level block to enable release signing manually.",
		"path", relPath, "reason", reason)
	// Emit the block itself as its own record so it is cleanly pasteable.
	g.props.Logger.Warn(block)
}

// renderGoreleaserSignsBlock renders the embedded skeleton with the current
// signing posture and extracts just the top-level signs: block (including its
// gtb marker comment). Deriving it from the same skeleton the fresh-generate
// path renders keeps the injected/advised block identical to a scaffolded one,
// with no separate copy to drift.
func (g *Generator) renderGoreleaserSignsBlock(m *Manifest) (string, error) {
	content, err := fs.ReadFile(skeletonAssets, goreleaserAssetEmbedPath)
	if err != nil {
		return "", errors.Newf("failed to read embedded %s: %w", goreleaserAssetEmbedPath, err)
	}

	tmpl, err := template.New(goreleaserAssetRelPath).Funcs(templateFuncMap).Parse(string(content))
	if err != nil {
		return "", errors.Newf("failed to parse %s: %w", goreleaserAssetEmbedPath, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, g.buildSkeletonTemplateData(*m)); err != nil {
		return "", errors.Newf("failed to render %s: %w", goreleaserAssetEmbedPath, err)
	}

	block, ok := extractSignsBlock(buf.String())
	if !ok {
		return "", errors.Newf("rendered skeleton contains no signs: block (key id: %q)", m.Properties.Signing.KeyID)
	}

	return block, nil
}

// recordGoreleaserHash records the hash of freshly-written content on the
// manifest so subsequent runs track the file against what gtb wrote.
func (g *Generator) recordGoreleaserHash(relPath string, content []byte, m *Manifest) {
	if m.Hashes == nil {
		m.Hashes = make(map[string]string)
	}

	m.Hashes[relPath] = calculateHash(content)
}

// rehashGoreleaserOnDisk re-records the on-disk hash of an unmodified file so
// drift stays tracked after an advisory/ignored no-write (mirroring
// hashIgnoredFile in the full-skeleton walk).
func (g *Generator) rehashGoreleaserOnDisk(fullPath, relPath string, m *Manifest) {
	content, err := afero.ReadFile(g.props.FS, fullPath)
	if err != nil {
		return
	}

	g.recordGoreleaserHash(relPath, content, m)
}

// parseYAMLMapping decodes content and returns the root mapping node when the
// document is a single YAML mapping, reporting false otherwise (empty,
// unparseable, or a non-mapping top level) — the shapes injection must refuse.
func parseYAMLMapping(content []byte) (*yaml.Node, bool) {
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, false
	}

	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, false
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, false
	}

	return root, true
}

// topLevelKeyPresent reports whether the mapping node has the given top-level
// key. Keys are the even-indexed children of a mapping node.
func topLevelKeyPresent(root *yaml.Node, key string) bool {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			return true
		}
	}

	return false
}

// appendTopLevelBlock appends block as a new top-level key at the end of the
// document, separated by exactly one blank line. YAML mapping keys may appear
// in any order, so appending is a valid, structure-preserving edit that leaves
// every existing byte in place. block is expected to end with a newline.
func appendTopLevelBlock(content []byte, block string) []byte {
	s := string(content)

	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}

	if !strings.HasSuffix(s, "\n\n") {
		s += "\n"
	}

	return []byte(s + block)
}

// extractSignsBlock returns the top-level signs: block from a rendered
// .goreleaser.yaml: the contiguous comment lines immediately above signs:
// through the last indented line of its body. The returned block ends with a
// single newline.
func extractSignsBlock(rendered string) (string, bool) {
	lines := strings.Split(rendered, "\n")

	signsIdx := indexOfLine(lines, "signs:")
	if signsIdx == -1 {
		return "", false
	}

	start := signsIdx
	for start > 0 && strings.HasPrefix(lines[start-1], "#") {
		start--
	}

	end := blockBodyEnd(lines, signsIdx)

	return strings.Join(lines[start:end+1], "\n") + "\n", true
}

// removeGtbSignsBlock removes the gtb-injected signs: block (its marker comment
// lines, the signs: key, and its indented body) plus one trailing blank-line
// separator, leaving every other block untouched and a single trailing newline.
func removeGtbSignsBlock(content []byte) []byte {
	lines := strings.Split(string(content), "\n")

	signsIdx := indexOfLine(lines, "signs:")
	if signsIdx == -1 {
		return content
	}

	start := signsIdx
	for start > 0 && strings.HasPrefix(lines[start-1], "#") {
		start--
	}

	end := blockBodyEnd(lines, signsIdx)

	// Consume one trailing blank-line separator so removal does not leave a
	// double blank (append injects exactly one).
	if end+1 < len(lines) && lines[end+1] == "" {
		end++
	}

	kept := append(append([]string{}, lines[:start]...), lines[end+1:]...)
	result := strings.TrimRight(strings.Join(kept, "\n"), "\n") + "\n"

	return []byte(result)
}

// indexOfLine returns the index of the first line exactly equal to want, or -1.
// A top-level key such as "signs:" is unindented, so an exact match cannot
// collide with a nested key of the same name.
func indexOfLine(lines []string, want string) int {
	for i, l := range lines {
		if l == want {
			return i
		}
	}

	return -1
}

// blockBodyEnd returns the index of the last line of the block whose key is at
// keyIdx: the run of indented (or blank-within) body lines following it. The
// scan stops at the first blank or unindented line, so the block ends at its
// last indented line.
func blockBodyEnd(lines []string, keyIdx int) int {
	end := keyIdx

	for end+1 < len(lines) {
		next := lines[end+1]
		if next == "" || !strings.HasPrefix(next, " ") {
			break
		}

		end++
	}

	return end
}
