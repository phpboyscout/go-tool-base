package main

import (
	"archive/tar"
	"bytes"
	"io"
	"path"
	"slices"
	"strconv"
	"strings"

	"gitlab.com/phpboyscout/go/errors"
)

const (
	arMagic      = "!<arch>\n"
	arHeaderSize = 60
	// The fixed columns of an ar member header.
	arNameEnd, arSizeStart, arSizeEnd = 16, 48, 58
)

// indexFields are the ones apt's index adds to a package's control fields, so
// a control file that sets one would contradict it.
var indexFields = []string{"Filename", "Size", "SHA256", "SHA512", "SHA1", "MD5sum"}

// readDeb is a deb's index entry, less the fields that say where it is.
func readDeb(data []byte) (control string, e entry, err error) {
	raw, err := debControl(data)
	if err != nil {
		return "", entry{}, err
	}

	control = strings.TrimRight(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")

	fields, err := controlFields(control)
	if err != nil {
		return "", entry{}, err
	}

	for f := range fields {
		if slices.ContainsFunc(indexFields, func(i string) bool { return strings.EqualFold(i, f) }) {
			return "", entry{}, errors.Wrapf(errPackage, "the control file sets %s", f)
		}
	}

	e = entry{name: fields["Package"], version: fields["Version"], arch: fields["Architecture"], sha: sha256Hex(data)}

	return control, e, e.complete()
}

// debControl is the control file in a deb's control.tar.gz, the one
// compression nfpm gives it.
func debControl(data []byte) (string, error) {
	member, err := arMember(data, "control.tar.gz")
	if err != nil {
		return "", err
	}

	tarball, err := gunzip(member)
	if err != nil {
		return "", errors.Wrap(err, "control.tar.gz")
	}

	r := tar.NewReader(bytes.NewReader(tarball))

	for {
		h, err := r.Next()
		if errors.Is(err, io.EOF) {
			return "", errors.Wrap(errPackage, "the deb has no control file")
		}

		if err != nil {
			return "", errors.Wrap(err, "control.tar.gz")
		}

		if path.Clean(h.Name) == "control" {
			b, err := readBounded(r, "control")

			return string(b), err
		}
	}
}

func arMember(data []byte, want string) ([]byte, error) {
	rest, ok := bytes.CutPrefix(data, []byte(arMagic))
	if !ok {
		return nil, errors.Wrap(errPackage, "not an ar archive")
	}

	for len(rest) >= arHeaderSize {
		header := rest[:arHeaderSize]
		rest = rest[arHeaderSize:]

		size, err := strconv.Atoi(strings.TrimSpace(string(header[arSizeStart:arSizeEnd])))
		if err != nil || size < 0 || size > len(rest) {
			return nil, errors.Wrap(errPackage, "an ar member's size is not valid")
		}

		if strings.TrimRight(string(header[:arNameEnd]), " /") == want {
			return rest[:size], nil
		}

		// Members start on an even offset.
		rest = rest[min(size+size%2, len(rest)):]
	}

	return nil, errors.Wrapf(errPackage, "the deb has no %s", want)
}

// controlFields are the fields of one stanza. A continuation line adds to its
// field's value, and a field's name is the same one in either case.
func controlFields(stanza string) (map[string]string, error) {
	fields, seen := map[string]string{}, map[string]bool{}
	last := ""

	for line := range strings.Lines(stanza) {
		line = strings.TrimRight(line, "\n")

		if rest, ok := strings.CutPrefix(line, " "); ok && last != "" && strings.TrimSpace(rest) != "" {
			fields[last] += "\n" + rest

			continue
		}

		name, value, ok := strings.Cut(line, ":")
		if !ok || name == "" || strings.ContainsAny(name, " \t") {
			return nil, errors.Wrapf(errPackage, "%q is not a control field", line)
		}

		folded := strings.ToLower(name)
		if seen[folded] {
			return nil, errors.Wrapf(errPackage, "%s is set twice", name)
		}

		seen[folded] = true
		fields[name], last = strings.TrimSpace(value), name
	}

	return fields, nil
}
