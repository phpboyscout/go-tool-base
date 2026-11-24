package main

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"gitlab.com/phpboyscout/go/errors"
)

const (
	aptDir        = "apt/"
	packagesFile  = "Packages"
	packagesGzip  = "Packages.gz"
	releaseFile   = "Release"
	releaseSig    = "Release.gpg"
	aptDateFormat = "Mon, 02 Jan 2006 15:04:05 UTC"
	// releaseFields is a hash line in Release: digest, size, name.
	releaseFields = 3
)

var debArches = []string{"amd64", "arm64"}

// aptIndex is the published apt repository: what it lists, and when it was
// last written.
type aptIndex struct {
	entries []entry
	date    time.Time
}

func (r *reader) aptIndex(ctx context.Context) (aptIndex, error) {
	release, err := r.signed(ctx, aptDir+releaseFile, aptDir+releaseSig)
	if err != nil || release == nil {
		return aptIndex{}, err
	}

	hashes, date, err := parseRelease(string(release))
	if err != nil {
		return aptIndex{}, err
	}

	var packages []byte

	for _, name := range []string{packagesFile, packagesGzip} {
		data, err := r.index(ctx, aptDir+name)
		if err != nil {
			return aptIndex{}, err
		}

		if hashes[name] != sha256Hex(data) {
			return aptIndex{}, errors.Wrapf(errUnverified, "%s does not match its Release", aptDir+name)
		}

		if name == packagesFile {
			packages = data
		} else if _, err := r.unzip(data); err != nil {
			return aptIndex{}, errors.Wrap(err, aptDir+name)
		}
	}

	entries, err := parsePackages(string(packages))

	return aptIndex{entries: entries, date: date}, err
}

// parseRelease is a Release file's SHA256 list and its date.
func parseRelease(text string) (map[string]string, time.Time, error) {
	fields, err := controlFields(strings.TrimRight(text, "\n"))
	if err != nil {
		return nil, time.Time{}, errors.Wrap(err, releaseFile)
	}

	date, err := time.Parse(aptDateFormat, fields["Date"])
	if err != nil {
		return nil, time.Time{}, errors.Wrapf(errIndex, "Release's Date is %q", fields["Date"])
	}

	hashes := map[string]string{}

	for line := range strings.Lines(fields["SHA256"]) {
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}

		if len(parts) != releaseFields {
			return nil, time.Time{}, errors.Wrapf(errIndex, "%q in Release", strings.TrimSpace(line))
		}

		hashes[parts[2]] = parts[0]
	}

	return hashes, date, nil
}

func parsePackages(text string) ([]entry, error) {
	var out []entry

	for stanza := range strings.SplitSeq(strings.TrimRight(text, "\n"), "\n\n") {
		fields, err := controlFields(stanza)
		if err != nil {
			return nil, errors.Wrap(err, packagesFile)
		}

		e := entry{
			name: fields["Package"], version: fields["Version"], arch: fields["Architecture"], sha: fields["SHA256"], body: stanza,
		}
		if err := e.complete(); err != nil {
			return nil, errors.Wrap(err, packagesFile)
		}

		out = append(out, e)
	}

	return out, nil
}

// debEntry is the index entry for the deb at file, under its tag's directory.
func debEntry(tag, file string, data []byte) (entry, error) {
	control, e, err := readDeb(data)
	if err != nil {
		return entry{}, errors.Wrap(err, file)
	}

	if !slices.Contains(debArches, e.arch) {
		return entry{}, errors.Wrapf(errPackage, "%s is for %s", file, e.arch)
	}

	if err := agree(tag, file, e, fmt.Sprintf("%s_%s_%s.deb", e.name, e.version, e.arch)); err != nil {
		return entry{}, err
	}

	e.body = fmt.Sprintf("%s\nFilename: %s\nSize: %d\nSHA256: %s", control, location(tag, file), len(data), e.sha)

	return e, nil
}

// aptFiles are the apt repository's files, less the signature.
func (r *reader) aptFiles(origin string, entries []entry, date time.Time) (map[string][]byte, error) {
	var packages bytes.Buffer

	for _, e := range entries {
		packages.WriteString(e.body + "\n\n")
	}

	list := bytes.TrimSuffix(packages.Bytes(), []byte("\n"))

	zipped, err := r.zip(list)
	if err != nil {
		return nil, err
	}

	var release strings.Builder

	fmt.Fprintf(&release, "Origin: %s\nLabel: %s\nDate: %s\nArchitectures: %s\nSHA256:\n",
		origin, origin, date.UTC().Format(aptDateFormat), strings.Join(debArches, " "))

	for _, f := range []struct {
		name string
		data []byte
	}{{packagesFile, list}, {packagesGzip, zipped}} {
		release.WriteString(" " + sha256Hex(f.data) + " " + strconv.Itoa(len(f.data)) + " " + f.name + "\n")
	}

	return map[string][]byte{
		aptDir + packagesFile: list,
		aptDir + packagesGzip: zipped,
		aptDir + releaseFile:  []byte(release.String()),
	}, nil
}
