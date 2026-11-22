package generator

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/afero"
	"gitlab.com/phpboyscout/go/errors"
)

// syncSkeletonFromManifest brings the skeleton's non-Go files into line with a
// settings change: only the files whose render moves between before and after
// are touched (spec 0218 D4), and one edited by hand is kept and named (D5).
// Hashes of what it writes go into after.Hashes, which the caller persists.
// before gets the derived fields and the keychain record after already has,
// so an older manifest's missing fields are not mistaken for the change.
func (g *Generator) syncSkeletonFromManifest(before, after *Manifest) error {
	if _, err := deriveMissingManifestFields(before); err != nil {
		return err
	}

	g.recordKeychainFromFile(before)

	was, err := g.renderSkeleton(*before)
	if err != nil {
		return err
	}

	now, err := g.renderSkeleton(*after)
	if err != nil {
		return err
	}

	if after.Hashes == nil {
		after.Hashes = map[string]string{}
	}

	sync := skeletonSync{g: g, hashes: after.Hashes, hasOverlays: len(after.Properties.Templates) > 0}
	rules := g.ignoreRules()
	overlaid := overlayOutputs(after.Properties.Templates)

	for _, rel := range skeletonChanges(was, now) {
		if isUserOwnedSeedFile(rel) || rules.IsIgnored(rel) || overlaid[rel] {
			continue
		}

		if err := sync.apply(rel, now[rel]); err != nil {
			return err
		}
	}

	return nil
}

// skeletonSync applies one settings change's skeleton diff to the project.
type skeletonSync struct {
	g           *Generator
	hashes      map[string]string
	hasOverlays bool
}

// apply writes, creates or removes one skeleton file; content is nil for a
// file the change stops producing. A file settled says to leave is left.
func (s skeletonSync) apply(rel string, content []byte) error {
	if s.settled(rel, content) {
		return nil
	}

	full := joinProjectPath(s.g.config.Path, rel)

	if content == nil {
		if err := s.g.removeGeneratedFile(rel); err != nil {
			return err
		}

		delete(s.hashes, hashKey(rel))

		return nil
	}

	if err := s.g.props.FS.MkdirAll(filepath.Dir(full), os.ModePerm); err != nil {
		return errors.Newf("failed to create directory for %s: %w", rel, err)
	}

	if err := afero.WriteFile(s.g.props.FS, full, content, os.ModePerm); err != nil {
		return errors.Newf("failed to write %s: %w", rel, err)
	}

	s.hashes[hashKey(rel)] = calculateHash(content)

	return nil
}

// settled reports whether a skeleton file needs no write or removal: it is
// already right, already gone, or not gtb's to change.
func (s skeletonSync) settled(rel string, content []byte) bool {
	full := joinProjectPath(s.g.config.Path, rel)

	existing, err := afero.ReadFile(s.g.props.FS, full)
	if err != nil {
		return s.absentSettled(full, rel, content)
	}

	switch {
	case content != nil && bytes.Equal(existing, content):
		s.hashes[hashKey(rel)] = calculateHash(content)
	case s.hashes[hashKey(rel)] != calculateHash(existing):
		// Not what gtb last recorded, so the developer's.
		s.g.props.Logger.Warn(rel + " was edited by hand, so it was left as it is; gtb regenerate project shows the change and asks")
	default:
		return false
	}

	return true
}

func (s skeletonSync) absentSettled(full, rel string, content []byte) bool {
	switch {
	case content == nil:
		delete(s.hashes, hashKey(rel))
	case s.hasOverlays:
		// An overlay's replaces: may have removed it on purpose, and set does
		// not resolve overlays (spec 0218, out of scope).
		s.g.props.Logger.Debug("not creating a skeleton file in a project with template overlays", "path", rel)
	case s.g.otherRenovateConfig(full, rel) != "":
	default:
		return false
	}

	return true
}

// hashKey is rel as regenerate records it in the manifest's hashes: an OS
// path, so the two agree on every platform.
func hashKey(rel string) string {
	return filepath.FromSlash(rel)
}

// renderSkeleton renders the embedded skeleton for m in memory, keyed by
// project-relative path. A release script the configuration does not call is
// absent; a file whose template fails maps to nil, and is never compared.
func (g *Generator) renderSkeleton(m Manifest) (map[string][]byte, error) {
	data := g.buildSkeletonTemplateData(m)
	out := map[string][]byte{}

	for _, r := range skeletonRoots(data) {
		if err := fs.WalkDir(r.fsys, r.root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}

			rel, err := filepath.Rel(r.root, path)
			if err != nil {
				return errors.Newf("failed to get relative path: %w", err)
			}

			rel = filepath.ToSlash(rel)
			if omittedReleaseAsset(rel, data) {
				return nil
			}

			src, err := fs.ReadFile(r.fsys, path)
			if err != nil {
				return errors.Newf("failed to read embedded file %s: %w", path, err)
			}

			// A failed render is regenerate's to report; here it is no change.
			out[rel], _ = renderSkeletonBytes(joinProjectPath(g.config.Path, rel), string(src), data)

			return nil
		}); err != nil {
			return nil, err
		}
	}

	return out, nil
}

// skeletonChanges lists, sorted, the paths whose render differs between two
// renders, a path present in only one included. A path that failed to render
// on either side is left out, so a listed path absent from now is one the
// change stops producing.
func skeletonChanges(was, now map[string][]byte) []string {
	var changed []string

	for rel, content := range now {
		old, ok := was[rel]
		if content == nil || (ok && old == nil) {
			continue
		}

		if !ok || !bytes.Equal(old, content) {
			changed = append(changed, rel)
		}
	}

	for rel, old := range was {
		if _, ok := now[rel]; !ok && old != nil {
			changed = append(changed, rel)
		}
	}

	sort.Strings(changed)

	return changed
}

// overlayOutputs is every path a template overlay last rendered, which set
// leaves to regenerate.
func overlayOutputs(sources []TemplateSource) map[string]bool {
	out := map[string]bool{}

	for _, s := range sources {
		for rel := range s.Hashes {
			out[filepath.ToSlash(rel)] = true
		}
	}

	return out
}
