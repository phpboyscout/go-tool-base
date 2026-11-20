package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"gitlab.com/phpboyscout/go/errors"
)

var errNotesTwice = errors.NewSentinel("gtb.releasemanifest.notes_twice", "--notes and --changelog both name the notes; give one")

// notesSource returns the notes for a release, given its tag. Nil means the
// release carries none.
type notesSource func(tag string) (string, error)

// notesFlags turns the --notes and --changelog flags into a source.
func notesFlags(notesPath, changelogPath string, warn io.Writer) (notesSource, error) {
	switch {
	case notesPath != "" && changelogPath != "":
		return nil, errNotesTwice
	case notesPath != "":
		return notesFromFile(notesPath), nil
	case changelogPath != "":
		return notesFromChangelog(changelogPath, warn), nil
	default:
		return nil, nil
	}
}

// notesFromFile gives the file's text as every release's notes.
func notesFromFile(path string) notesSource {
	return func(string) (string, error) {
		text, err := os.ReadFile(path)
		if err != nil {
			return "", errors.Wrap(err, "reading notes")
		}

		return string(text), nil
	}
}

// notesFromChangelog gives the tag's section of the changelog colophon has
// already committed for it. A missing file or section warns and gives no
// notes rather than failing the release, because notes are optional (spec
// 0203 D8) and the binaries are already built.
func notesFromChangelog(path string, warn io.Writer) notesSource {
	return func(tag string) (string, error) {
		raw, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			_, _ = fmt.Fprintf(warn, "releasemanifest: %s does not exist, so %s carries no notes\n", path, tag)

			return "", nil
		}

		if err != nil {
			return "", errors.Wrap(err, "reading changelog")
		}

		section, found := changelogSection(string(raw), tag)
		if !found {
			_, _ = fmt.Fprintf(warn, "releasemanifest: %s has no section for %s, so it carries no notes\n", path, tag)
		}

		return section, nil
	}
}

// changelogSection is the body under tag's "## " heading, up to the next one,
// with the heading itself left out because the updater prints its own.
func changelogSection(text, tag string) (string, bool) {
	var (
		body []string
		in   bool
	)

	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(line, "## ") {
			if in {
				break
			}

			in = isReleaseHeading(line, tag)

			continue
		}

		if in {
			body = append(body, line)
		}
	}

	return strings.TrimSpace(strings.Join(body, "\n")), in
}

// isReleaseHeading matches "## [v1.2.0](...)" and "## v1.2.0", and not a
// heading for a tag v1.2.0 merely prefixes, such as v1.2.0-rc.1.
func isReleaseHeading(line, tag string) bool {
	rest := strings.TrimPrefix(strings.TrimPrefix(line, "## "), "[")

	after, ok := strings.CutPrefix(rest, tag)

	return ok && (after == "" || after[0] == ']' || after[0] == ' ')
}
