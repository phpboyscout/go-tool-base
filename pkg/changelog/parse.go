package changelog

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/cockroachdb/errors"
)

var (
	releaseHeaderRe = regexp.MustCompile(`^#\s+(v?\d+\.\d+\.\d+.*)`)
	sectionHeaderRe = regexp.MustCompile(`^###?\s+(.+)`)
	entryScopedRe   = regexp.MustCompile(`^\*\s+\*\*([^*:]+):\*\*\s*(.+)`)
	entryUnscopedRe = regexp.MustCompile(`^\*\s+(.+)`)
)

var sectionCategories = map[string]Category{
	"breaking changes":         CategoryBreaking,
	"features":                 CategoryFeature,
	"bug fixes":                CategoryFix,
	"performance improvements": CategoryPerformance,
	"feat":                     CategoryFeature,
	"fix":                      CategoryFix,
	"perf":                     CategoryPerformance,
}

const (
	// changelogFileName is the name of the changelog file expected in release archives.
	changelogFileName = "CHANGELOG.md"
	// maxChangelogSize is the maximum size of a changelog file to read from an archive (10 MB).
	maxChangelogSize = 10 << 20
)

// ParseFromArchive extracts and parses a CHANGELOG.md file from a gzipped tar
// release archive reader. Returns nil (not an error) if no changelog is found
// in the archive, allowing callers to fall back to API-based retrieval.
func ParseFromArchive(r io.Reader) (*Changelog, error) {
	gzipReader, err := gzip.NewReader(r)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open gzip reader")
	}

	defer func() { _ = gzipReader.Close() }()

	tarReader := tar.NewReader(gzipReader)

	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, errors.Wrap(err, "failed to read tar entry")
		}

		if header.Typeflag != tar.TypeReg {
			continue
		}

		// Match CHANGELOG.md at any nesting depth within the archive.
		name := header.Name
		if idx := strings.LastIndex(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}

		if !strings.EqualFold(name, changelogFileName) {
			continue
		}

		var buf bytes.Buffer

		if _, err := io.Copy(&buf, io.LimitReader(tarReader, maxChangelogSize)); err != nil {
			return nil, errors.Wrap(err, "failed to read changelog from archive")
		}

		return Parse(buf.String()), nil
	}

	return nil, nil
}

// Parse parses raw release notes markdown (as returned by SelfUpdater.GetReleaseNotes)
// into a structured Changelog.
func Parse(rawNotes string) *Changelog {
	cl := &Changelog{}

	if strings.TrimSpace(rawNotes) == "" {
		return cl
	}

	lines := strings.Split(rawNotes, "\n")
	currentCategory := CategoryOther

	var currentRelease *Release

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		var handled bool

		currentRelease, currentCategory, handled = parseHeader(cl, trimmed, currentRelease, currentCategory)
		if handled {
			continue
		}

		currentRelease = parseEntry(trimmed, currentRelease, currentCategory)
	}

	// Append the last release
	if currentRelease != nil {
		cl.Releases = append(cl.Releases, *currentRelease)
	}

	// Reverse so oldest is first
	slices.Reverse(cl.Releases)

	// Set version range from releases
	if len(cl.Releases) > 0 {
		cl.FromVersion = cl.Releases[0].Version
		cl.ToVersion = cl.Releases[len(cl.Releases)-1].Version
	}

	return cl
}

// parseHeader checks if the line is a release header or section header and
// updates the parser state accordingly. Returns true if the line was handled.
func parseHeader(cl *Changelog, line string, current *Release, cat Category) (*Release, Category, bool) {
	if m := releaseHeaderRe.FindStringSubmatch(line); m != nil {
		if current != nil {
			cl.Releases = append(cl.Releases, *current)
		}

		return &Release{Version: strings.TrimSpace(m[1])}, CategoryOther, true
	}

	if m := sectionHeaderRe.FindStringSubmatch(line); m != nil {
		sectionName := strings.ToLower(strings.TrimSpace(m[1]))
		if mapped, ok := sectionCategories[sectionName]; ok {
			return current, mapped, true
		}

		return current, CategoryOther, true
	}

	return current, cat, false
}

// parseEntry attempts to parse a bullet-point entry line and append it to the
// current release. Returns the (possibly initialised) current release.
func parseEntry(line string, current *Release, cat Category) *Release {
	if m := entryScopedRe.FindStringSubmatch(line); m != nil {
		if current == nil {
			current = &Release{}
		}

		entry := Entry{
			Category:    cat,
			Scope:       m[1],
			Description: strings.TrimSpace(m[2]),
			Raw:         line,
		}

		reclassifyBreaking(&entry)
		current.Entries = append(current.Entries, entry)

		return current
	}

	if m := entryUnscopedRe.FindStringSubmatch(line); m != nil {
		if current == nil {
			current = &Release{}
		}

		entry := Entry{
			Category:    cat,
			Description: strings.TrimSpace(m[1]),
			Raw:         line,
		}

		reclassifyBreaking(&entry)
		current.Entries = append(current.Entries, entry)

		return current
	}

	return current
}

// reclassifyBreaking detects a BREAKING CHANGE: footer in the entry description
// and reclassifies the entry as CategoryBreaking.
func reclassifyBreaking(e *Entry) {
	if strings.HasPrefix(e.Description, "BREAKING CHANGE:") {
		e.Category = CategoryBreaking
		e.Description = strings.TrimSpace(strings.TrimPrefix(e.Description, "BREAKING CHANGE:"))
	}
}
