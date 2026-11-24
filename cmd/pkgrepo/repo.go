package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/signing/verify"
)

const (
	checksumsFile    = "checksums.txt"
	checksumsSigFile = checksumsFile + ".sig"
	// baseDir holds what the indexes were built from, for the publish to
	// check the store against (spec 0097 D5).
	baseDir = "base/"
)

var (
	errTag        = errors.NewSentinel("gtb.pkgrepo.tag", "only a stable release's packages are listed")
	errSource     = errors.NewSentinel("gtb.pkgrepo.source", "the store could not be read")
	errTooLarge   = errors.NewSentinel("gtb.pkgrepo.too_large", "an object is larger than it should be")
	errUnverified = errors.NewSentinel("gtb.pkgrepo.unverified", "what was read does not verify, so nothing is built from it")
	errPackage    = errors.NewSentinel("gtb.pkgrepo.package", "a package cannot be listed")
	errIndex      = errors.NewSentinel("gtb.pkgrepo.index", "the published index cannot be read, and is not replaced")
	errRedefined  = errors.NewSentinel("gtb.pkgrepo.redefined", "a published version cannot change")
	errOutput     = errors.NewSentinel("gtb.pkgrepo.output", "the output directory is not empty")
)

var (
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// packageName keeps a file name to its tag's directory.
	packageName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*\.(deb|rpm)$`)
)

// release is the tag whose packages are added.
type release struct {
	tag, origin string
	date        time.Time
}

// reader reads the store, verifies what it reads, and keeps every index file
// it read in base. zipped holds the published gzip files by their content's
// digest.
type reader struct {
	source source
	trust  *verify.TrustSet
	base   map[string][]byte
	zipped map[string][]byte
}

// build returns the files to write, by path: the indexes with the release's
// packages added, and the published copies they were built from.
func build(ctx context.Context, src source, trust *verify.TrustSet, rel release) (map[string][]byte, error) {
	if !semver.IsValid(rel.tag) || semver.Canonical(rel.tag) != rel.tag || semver.Prerelease(rel.tag) != "" {
		return nil, errors.Wrapf(errTag, "%q", rel.tag)
	}

	r := &reader{source: src, trust: trust, base: map[string][]byte{}, zipped: map[string][]byte{}}

	debs, rpms, err := r.packages(ctx, rel.tag)
	if err != nil {
		return nil, err
	}

	out, err := r.apt(ctx, rel, debs)
	if err != nil {
		return nil, err
	}

	yum, err := r.yum(ctx, rel, rpms)
	if err != nil {
		return nil, err
	}

	maps.Copy(out, yum)

	for name, data := range r.base {
		out[baseDir+name] = data
	}

	return out, nil
}

func (r *reader) apt(ctx context.Context, rel release, debs []entry) (map[string][]byte, error) {
	published, err := r.aptIndex(ctx)
	if err != nil {
		return nil, err
	}

	entries, err := merge(published.entries, debs)
	if err != nil {
		return nil, err
	}

	return r.aptFiles(rel.origin, entries, later(rel.date, published.date))
}

func (r *reader) yum(ctx context.Context, rel release, rpms []entry) (map[string][]byte, error) {
	published, err := r.yumIndex(ctx)
	if err != nil {
		return nil, err
	}

	entries, err := merge(published.entries, rpms)
	if err != nil {
		return nil, err
	}

	return r.yumFiles(entries, later(rel.date, published.date))
}

// packages are the release's, read from the store and checked against its
// signed checksums.
func (r *reader) packages(ctx context.Context, tag string) (debs, rpms []entry, err error) {
	sums, err := r.checksums(ctx, tag)
	if err != nil {
		return nil, nil, err
	}

	for _, file := range slices.Sorted(maps.Keys(sums)) {
		read, into := debEntry, &debs

		switch path.Ext(file) {
		case ".deb":
		case ".rpm":
			read, into = rpmEntry, &rpms
		default:
			continue
		}

		data, err := r.packageFile(ctx, tag, file, sums[file])
		if err != nil {
			return nil, nil, err
		}

		e, err := read(tag, file, data)
		if err != nil {
			return nil, nil, err
		}

		*into = append(*into, e)
	}

	if len(debs) == 0 || len(rpms) == 0 {
		return nil, nil, errors.Wrapf(errPackage, "%s lists %d debs and %d rpms", checksumsFile, len(debs), len(rpms))
	}

	return debs, rpms, nil
}

func (r *reader) packageFile(ctx context.Context, tag, file, sum string) ([]byte, error) {
	if !packageName.MatchString(file) {
		return nil, errors.Wrapf(errPackage, "%q", file)
	}

	data, err := r.source.get(ctx, tag+"/"+file)
	if err != nil {
		return nil, err
	}

	if sha256Hex(data) != sum {
		return nil, errors.Wrapf(errUnverified, "%s does not match %s", file, checksumsFile)
	}

	return data, nil
}

func (r *reader) checksums(ctx context.Context, tag string) (map[string]string, error) {
	data, err := r.source.get(ctx, tag+"/"+checksumsFile)
	if err != nil {
		return nil, err
	}

	sig, err := r.source.get(ctx, tag+"/"+checksumsSigFile)
	if err != nil {
		return nil, err
	}

	if err := r.verify(checksumsFile, data, sig); err != nil {
		return nil, err
	}

	sums := map[string]string{}

	for line := range strings.Lines(string(data)) {
		sum, name, ok := strings.Cut(strings.TrimRight(line, "\n"), "  ")
		if !ok || !sha256Pattern.MatchString(sum) {
			return nil, errors.Wrapf(errPackage, "%q in %s", strings.TrimSpace(line), checksumsFile)
		}

		if _, dup := sums[name]; dup {
			return nil, errors.Wrapf(errPackage, "%s lists %s twice", checksumsFile, name)
		}

		sums[name] = sum
	}

	return sums, nil
}

// signed is an index's signed file, or nil when neither it nor its signature
// is published. One without the other is refused.
func (r *reader) signed(ctx context.Context, name, sigName string) ([]byte, error) {
	data, err := r.index(ctx, name)
	missing := errors.Is(err, fs.ErrNotExist)

	if err != nil && !missing {
		return nil, err
	}

	sig, err := r.index(ctx, sigName)
	if missing && errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if missing {
		return nil, errors.Wrapf(errUnverified, "%s is published and %s is not", sigName, name)
	}

	if err != nil {
		return nil, err
	}

	return data, r.verify(name, data, sig)
}

// index reads one published index file and keeps it.
func (r *reader) index(ctx context.Context, name string) ([]byte, error) {
	data, err := r.source.get(ctx, name)
	if err != nil {
		return nil, err
	}

	r.base[name] = data

	return data, nil
}

// unzip is the content of a published gzip file that has been verified.
func (r *reader) unzip(zipped []byte) ([]byte, error) {
	plain, err := gunzip(zipped)
	if err != nil {
		return nil, err
	}

	r.zipped[sha256Hex(plain)] = zipped

	return plain, nil
}

// zip compresses plain, or is the published file that holds it. A compressor's
// bytes change between Go releases, and a re-run must write the same ones.
func (r *reader) zip(plain []byte) ([]byte, error) {
	if zipped, ok := r.zipped[sha256Hex(plain)]; ok {
		return zipped, nil
	}

	return gzipBytes(plain)
}

func (r *reader) verify(name string, data, sig []byte) error {
	if err := r.trust.VerifyManifestSignature(data, sig); err != nil {
		return errors.Newf("%s: %w: %w", name, errUnverified, err)
	}

	return nil
}

// agree refuses a package whose version or file name is another release's: the
// signed checksums say whose bytes it is, not which release.
func agree(tag, file string, e entry, want string) error {
	if "v"+e.version != tag || file != want {
		return errors.Wrapf(errPackage, "%s is %s %s for %s, in %s", file, e.name, e.version, e.arch, tag)
	}

	return nil
}

// location is a package's path from either repository (spec 0097 D2).
func location(tag, file string) string { return "../" + tag + "/" + file }

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}

	return a
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}

// gzipBytes compresses with no time in the header, so the same data gives the
// same bytes on one Go release.
func gzipBytes(data []byte) ([]byte, error) {
	var b bytes.Buffer

	w, err := gzip.NewWriterLevel(&b, gzip.BestCompression)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	if _, err := w.Write(data); err != nil {
		return nil, errors.WithStack(err)
	}

	if err := w.Close(); err != nil {
		return nil, errors.WithStack(err)
	}

	return b.Bytes(), nil
}

func gunzip(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, errors.Wrap(errPackage, "not gzip")
	}
	defer func() { _ = r.Close() }()

	return readBounded(r, "gzip data")
}
