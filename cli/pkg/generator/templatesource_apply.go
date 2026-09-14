package generator

// Applying resolved template overlays on top of the embedded skeleton, in
// manifest (layer) order with last-writer-wins, recording per-source hashes
// and the resolved pin (SHA for git, fingerprint for local) back into each
// source entry. Overlay output participates in the existing hash/conflict and
// .gtb/ignore machinery exactly as embedded files do (D8); a `replaces:`
// suppression has already removed the relevant embedded files from the walk
// before this runs.

import (
	"gitlab.com/phpboyscout/go/errors"
)

// resolveAllSources resolves every source to a readable tree + descriptor +
// pin, preserving manifest (layer) order. A single source failing to resolve
// is fatal for the run — a missing/offline source must not silently produce a
// different output.
func (g *Generator) resolveAllSources(sources []TemplateSource) ([]*resolvedSource, error) {
	resolved := make([]*resolvedSource, 0, len(sources))

	for i := range sources {
		rs, err := g.resolveTemplateSource(sources[i])
		if err != nil {
			return nil, errors.Wrapf(err, "template source %q", sourceLabel(sources[i]))
		}

		resolved = append(resolved, rs)
	}

	return resolved, nil
}

// collectSuppressedPrefixes unions the suppressed embedded path prefixes across
// all resolved sources' descriptors (D3). Order does not matter — suppression
// is a set applied before the overlay layers.
func collectSuppressedPrefixes(resolved []*resolvedSource) []string {
	prefixes := make([]string, 0, len(resolved))
	for _, rs := range resolved {
		prefixes = append(prefixes, suppressedPrefixes(rs.descriptor)...)
	}

	return prefixes
}

// applyOverlays renders each resolved source's overlay in manifest order and
// writes the rendered files through the existing hash/conflict machinery,
// recording each output's hash under the source's own Hashes map. The returned
// slice is the input sources updated with the resolved SHA/fingerprint and the
// per-source hashes. For a shared output path across sources, the last source
// in order wins (D6); every override emits an info log naming the path and the
// winning source.
func (g *Generator) applyOverlays(destPath string, data any, storedHashes, collectedHashes map[string]string, rules *IgnoreRules, sources []TemplateSource, resolved []*resolvedSource) ([]TemplateSource, error) {
	if len(sources) == 0 {
		return sources, nil
	}

	contractData := toOverlayContractData(data)

	// Track which source last wrote each output path, for the override log.
	lastWriter := make(map[string]string, len(collectedHashes))

	updated := make([]TemplateSource, len(sources))

	for i := range sources {
		incoming := sources[i]
		src := incoming
		rs := resolved[i]

		files, err := renderOverlay(rs.fs, rs.root, destPath, contractData)
		if err != nil {
			return nil, errors.Wrapf(err, "template source %q", sourceLabel(src))
		}

		srcHashes := make(map[string]string, len(files))

		for _, f := range files {
			if rules.IsIgnored(f.relPath) {
				g.props.Logger.Debug("ignored by .gtb/ignore", "path", f.relPath)

				continue
			}

			g.logOverlayOverride(f.relPath, src, lastWriter, collectedHashes)

			hash, writeErr := g.writeOverlayFile(destPath, f, storedHashes)
			if writeErr != nil {
				g.props.Logger.Warn("skipped overlay", "path", f.relPath, "error", writeErr)

				continue
			}

			srcHashes[f.relPath] = hash
			collectedHashes[f.relPath] = hash
			lastWriter[f.relPath] = sourceLabel(src)
		}

		// Record the pin and per-source hashes back onto the entry.
		src.Resolved = rs.resolvedSHA
		src.Fingerprint = rs.fingerprint
		src.Hashes = srcHashes

		// Warn on local-source drift: the recorded fingerprint differs from
		// the tree we just read (O5). Only meaningful on regenerate, where the
		// incoming entry already carries a fingerprint.
		g.warnLocalDrift(incoming, rs)

		updated[i] = src
	}

	return updated, nil
}

// logOverlayOverride emits an info log when an overlay file overwrites an
// embedded file or an earlier source's output, naming the path and winner.
func (g *Generator) logOverlayOverride(relPath string, src TemplateSource, lastWriter, collectedHashes map[string]string) {
	if prev, ok := lastWriter[relPath]; ok {
		g.props.Logger.Info("overlay source overrides earlier source", "path", relPath, "source", sourceLabel(src), "overrides", prev)

		return
	}

	if _, embedded := collectedHashes[relPath]; embedded {
		g.props.Logger.Info("overlay source overrides the embedded skeleton (user wins)", "path", relPath, "source", sourceLabel(src))
	}
}

// warnLocalDrift logs a warning when a local source's recorded fingerprint
// (from a prior generation) no longer matches the current on-disk tree.
func (g *Generator) warnLocalDrift(incoming TemplateSource, rs *resolvedSource) {
	if incoming.Type != TemplateSourceLocal || incoming.Fingerprint == "" {
		return
	}

	if incoming.Fingerprint != rs.fingerprint {
		g.props.Logger.Warn(
			"local template source has drifted since last generation (fingerprint changed); regenerated output reflects the current on-disk tree",
			"source", sourceLabel(incoming),
		)
	}
}

// writeOverlayFile writes one rendered overlay file through the same
// conflict-checked write path embedded files use, so a user-edited overlay
// output is protected identically (D8). It returns the written content hash.
func (g *Generator) writeOverlayFile(destPath string, f renderedOverlayFile, storedHashes map[string]string) (string, error) {
	fullPath := joinProjectPath(destPath, f.relPath)

	return g.renderPreRenderedSkeletonFile(fullPath, f.relPath, f.content, storedHashes)
}

// removeStrandedSuppressed deletes on-disk files for tracked paths that a
// `replaces:` descriptor now suppresses, so an incremental regenerate does not
// leave an orphaned embedded file from a prior run. Only previously-tracked
// paths (in storedHashes) are removed — never a user-authored file the
// generator never wrote. Paths the overlay will re-supply are re-created by the
// subsequent overlay render, so removing them here is safe.
func (g *Generator) removeStrandedSuppressed(destPath string, suppressed []string, storedHashes map[string]string) {
	if len(suppressed) == 0 {
		return
	}

	for relPath := range storedHashes {
		if !isSuppressed(relPath, suppressed) {
			continue
		}

		full := joinProjectPath(destPath, relPath)
		if err := g.props.FS.Remove(full); err != nil {
			g.props.Logger.Debug("could not remove suppressed file", "path", relPath, "error", err)

			continue
		}

		delete(storedHashes, relPath)
		g.props.Logger.Debug("removed suppressed embedded file", "path", relPath)
	}
}

// sourceLabel returns a stable human label for a source for logs and errors:
// its Name when set, else its Location.
func sourceLabel(ts TemplateSource) string {
	if ts.Name != "" {
		return ts.Name
	}

	return ts.Location
}
