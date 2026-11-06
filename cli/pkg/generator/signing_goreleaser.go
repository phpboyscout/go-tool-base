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
//  1. Ignored path → never write, not even to create it (#148). Advise.
//  2. Absent file  → render the whole skeleton asset (fresh-scaffold semantics;
//     there is no customisation to lose).
//  3. Unparseable  → never write. Advise.
//  4. Otherwise    → safe, structure-preserving injection (enable) or removal
//     (disable) of only the top-level signs: block, leaving all other content
//     byte-for-byte untouched. A signs: block already present (enable) or an
//     author-written signs: block (disable) degrades to an advisory.
//
// A path that writes nothing leaves the stored hash as it was, and an edit moves
// it only if the file was unmodified (spec 0188 D9, #150). The caller persists
// m.Hashes.
// A failure to perform the edit is never fatal — the rest of enable/disable
// signing (trustkeys scaffold, root-command wiring, manifest update) proceeds.
func (g *Generator) applyGoreleaserSigns(m *Manifest) error {
	relPath := goreleaserAssetRelPath
	fullPath := filepath.Join(g.config.Path, relPath)
	enable := m.Properties.Signing.SignsReleases()

	exists, _ := afero.Exists(g.props.FS, fullPath)

	// .gtb/ignore takes precedence over everything, including Overwrite:"allow".
	if g.ignoreRules().IsIgnored(relPath) {
		// Disabling has nothing to remove from a file that is not there.
		if enable || exists {
			g.adviseGoreleaserSigns(relPath, m, enable, "it is listed in .gtb/ignore")
		}

		return nil
	}

	if !exists {
		// No file to preserve: render the full skeleton asset. This is the only
		// path that writes the whole skeleton, and it cannot lose customisation.
		g.regenerateGoreleaserAsset(m)

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
// every other block untouched. A block already present goes to
// refreshGoreleaserSigns.
func (g *Generator) enableGoreleaserSigns(fullPath, relPath string, content []byte, m *Manifest, hasSigns bool) error {
	block, err := g.renderGoreleaserSignsBlock(m)
	if err != nil {
		g.adviseGoreleaserSigns(relPath, m, true, "the signs: block could not be rendered")

		return nil //nolint:nilerr // advisory degrade: the edit is non-fatal, the rest of enable proceeds
	}

	if hasSigns {
		return g.refreshGoreleaserSigns(fullPath, relPath, content, block, m)
	}

	updated := appendTopLevelBlock(content, block)
	if err := afero.WriteFile(g.props.FS, fullPath, updated, DefaultFileMode); err != nil {
		return errors.Newf("failed to write %s: %w", relPath, err)
	}

	g.recordGoreleaserEdit(relPath, content, updated, m)
	g.props.Logger.Info("Injected the release-signing signs: block into the release config.", "path", relPath)

	return nil
}

// refreshGoreleaserSigns brings an existing signs: block up to date when it is
// still gtb's: it carries the marker and the file is unedited since gtb last
// wrote it. Any other block gets an advisory, so an author-tuned one is never
// clobbered.
func (g *Generator) refreshGoreleaserSigns(fullPath, relPath string, content []byte, block string, m *Manifest) error {
	if bytes.Contains(content, []byte(block)) {
		return nil
	}

	if !bytes.Contains(content, []byte(goreleaserSignsMarker)) || m.Hashes[relPath] != calculateHash(content) {
		g.adviseGoreleaserSigns(relPath, m, true, "a signs: block is already present")

		return nil
	}

	updated := appendTopLevelBlock(removeGtbSignsBlock(content), block)
	if err := afero.WriteFile(g.props.FS, fullPath, updated, DefaultFileMode); err != nil {
		return errors.Newf("failed to write %s: %w", relPath, err)
	}

	g.recordGoreleaserEdit(relPath, content, updated, m)
	g.props.Logger.Info("Updated the release-signing signs: block in the release config.", "path", relPath)

	return nil
}

// disableGoreleaserSigns removes a gtb-injected signs: block from a parseable,
// non-ignored .goreleaser.yaml, leaving every other block untouched. It removes
// only a block gtb itself wrote (identified by goreleaserSignsMarker); an
// author-written signs: block is left in place with an advisory.
func (g *Generator) disableGoreleaserSigns(fullPath, relPath string, content []byte, m *Manifest, hasSigns bool) error {
	if !hasSigns {
		return nil
	}

	if !bytes.Contains(content, []byte(goreleaserSignsMarker)) {
		g.props.Logger.Warn(
			"Release config not modified: a signs: block is present but was not written by gtb — remove it manually to disable release signing.",
			"path", relPath)

		return nil
	}

	updated := removeGtbSignsBlock(content)
	if err := afero.WriteFile(g.props.FS, fullPath, updated, DefaultFileMode); err != nil {
		return errors.Newf("failed to write %s: %w", relPath, err)
	}

	g.recordGoreleaserEdit(relPath, content, updated, m)
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

// recordGoreleaserEdit moves the stored hash to gtb's edit only when the file
// was unmodified before it (stored hash == before). An edited file keeps its
// old baseline, so the next regenerate still sees the developer's edits and
// asks rather than overwriting (spec 0188 D9, #150).
func (g *Generator) recordGoreleaserEdit(relPath string, before, after []byte, m *Manifest) {
	if m.Hashes == nil || m.Hashes[relPath] != calculateHash(before) {
		return
	}

	m.Hashes[relPath] = calculateHash(after)
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
