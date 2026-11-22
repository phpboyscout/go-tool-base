package main

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/signing/verify"

	"gitlab.com/phpboyscout/go-tool-base/pkg/release/static"
)

var (
	errNoChecksum           = errors.NewSentinel("gtb.releasemanifest.no_checksum", "an archive carries no checksum; nothing unverifiable is published")
	errDuplicatePlatform    = errors.NewSentinel("gtb.releasemanifest.duplicate_platform", "two archives are built for the same platform")
	errRotationUnsigned     = errors.NewSentinel("gtb.releasemanifest.rotation_unsigned", "a rotation manifest and its signature are published together")
	errRotationUnverifiable = errors.NewSentinel("gtb.releasemanifest.rotation_unverifiable", "no rotation authority key to verify the rotation manifest against")
	errRotationExpiring     = errors.NewSentinel("gtb.releasemanifest.rotation_expiring", "the rotation manifest expires too soon to publish")
)

// checksumFields is the two columns of a checksums.txt line: digest, name.
const checksumFields = 2

// builder composes the channel's two documents from a goreleaser dist. Its
// two seams exist for the tests: platform reads an archive's binary and now
// stamps the pointer.
type builder struct {
	platform func(path string) (goos, goarch string, err error)
	now      func() time.Time
}

type metadata struct {
	ProjectName string `json:"project_name"`
	Tag         string `json:"tag"`
	Date        string `json:"date"`
}

// build composes the manifest and the pointer for the release in dist. It
// reads what goreleaser has written by the time its before_publish hooks
// run: metadata.json, checksums.txt and the archives themselves. It is pure
// over the directory so a test can pin it with a golden file.
func (b builder) build(dist, baseURL, previous string, notes notesSource) (static.Manifest, static.Pointer, error) {
	var meta metadata

	if err := readJSON(filepath.Join(dist, "metadata.json"), &meta); err != nil {
		return static.Manifest{}, static.Pointer{}, err
	}

	released, err := time.Parse(time.RFC3339Nano, meta.Date)
	if err != nil {
		return static.Manifest{}, static.Pointer{}, errors.Wrapf(err, "metadata.json date %q", meta.Date)
	}

	m := static.Manifest{
		Schema:     static.SchemaVersion,
		Tool:       meta.ProjectName,
		Tag:        meta.Tag,
		ReleasedAt: released.UTC().Format(time.RFC3339),
		Previous:   previous,
	}

	if err := b.addFiles(&m, dist, baseURL); err != nil {
		return static.Manifest{}, static.Pointer{}, err
	}

	if notes != nil {
		text, err := notes(m.Tag)
		if err != nil {
			return static.Manifest{}, static.Pointer{}, err
		}

		m.Notes = text
	}

	p := static.Pointer{
		Schema:      static.SchemaVersion,
		Tool:        m.Tool,
		Tag:         m.Tag,
		Manifest:    static.ManifestURL(baseURL, m.Tag),
		PublishedAt: b.now().UTC().Format(time.RFC3339),
	}

	return m, p, nil
}

// addRotation lists the rotation manifest and its signature when the release
// carries them (spec 0101 D4). One without the other is a broken release, and
// a reader would fail the update on it, so it is refused here first.
func addRotation(m *static.Manifest, dist, baseURL string) error {
	manifest, signature := exists(dist, static.RotationFile), exists(dist, static.RotationSignatureFile)

	switch {
	case manifest && signature:
		m.Rotation = static.FileURL(baseURL, m.Tag, static.RotationFile)
		m.RotationSignature = static.FileURL(baseURL, m.Tag, static.RotationSignatureFile)
	case manifest || signature:
		return errors.Wrapf(errRotationUnsigned, "%s and %s must both be in %s", static.RotationFile, static.RotationSignatureFile, dist)
	}

	return nil
}

// prepareRotation stages the committed rotation manifest in rotationDir into
// dist and checks it against the keys in authorityDir, so nothing staged is
// published unchecked.
func prepareRotation(rotationDir, authorityDir, dist string, now time.Time) error {
	if err := stageRotation(rotationDir, dist); err != nil {
		return err
	}

	return checkRotation(dist, authorityDir, now)
}

// stageRotation copies the rotation files found in dir into dist, where
// addRotation lists them and the upload picks them up.
func stageRotation(dir, dist string) error {
	if dir == "" {
		return nil
	}

	src, err := os.OpenRoot(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return errors.Wrapf(err, "opening %s", dir)
	}

	defer func() { _ = src.Close() }()

	dst, err := os.OpenRoot(dist)
	if err != nil {
		return errors.Wrapf(err, "opening %s", dist)
	}
	defer func() { _ = dst.Close() }()

	for _, name := range []string{static.RotationFile, static.RotationSignatureFile} {
		data, err := src.ReadFile(name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}

		if err != nil {
			return errors.Wrapf(err, "reading %s from %s", name, dir)
		}

		if err := dst.WriteFile(name, data, documentMode); err != nil {
			return errors.Wrapf(err, "staging %s into %s", name, dist)
		}
	}

	return nil
}

// rotationExpiryMargin is how long a carried manifest must stay valid after a
// release. A manifest past not_after fails every fresh install's update, so
// it has to be reissued before then, and the margin makes the release that
// would carry a nearly expired one stop instead.
const rotationExpiryMargin = 30 * 24 * time.Hour

// checkRotation verifies a rotation manifest staged in dist against the
// minisign authority keys (*.pub) in authorityDir, and refuses one that
// expires within rotationExpiryMargin of now. Nothing staged passes.
func checkRotation(dist, authorityDir string, now time.Time) error {
	if !exists(dist, static.RotationFile) || !exists(dist, static.RotationSignatureFile) {
		return nil
	}

	keys, err := authorityKeys(authorityDir)
	if err != nil {
		return err
	}

	authority, err := verify.NewRotationAuthority(keys...)
	if err != nil {
		return errors.Wrapf(err, "reading the rotation authority keys in %s", authorityDir)
	}

	manifest, err := os.ReadFile(filepath.Join(dist, static.RotationFile))
	if err != nil {
		return errors.Wrap(err, "reading the staged rotation manifest")
	}

	sig, err := os.ReadFile(filepath.Join(dist, static.RotationSignatureFile))
	if err != nil {
		return errors.Wrap(err, "reading the staged rotation signature")
	}

	m, err := authority.VerifyRotationManifest(manifest, sig)
	if err != nil {
		return errors.Wrap(err, "the committed rotation manifest cannot be carried")
	}

	if m.NotAfter.Before(now.Add(rotationExpiryMargin)) {
		return errors.WithHint(errors.Wrapf(errRotationExpiring, "not_after is %s", m.NotAfter.Format(time.RFC3339)),
			"Issue a new rotation manifest with a later not_after and commit it in place of this one, then release again.")
	}

	return nil
}

func authorityKeys(dir string) ([]string, error) {
	if dir == "" {
		return nil, errors.Wrap(errRotationUnverifiable, "--rotation-authority is not set")
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.pub"))
	if err != nil {
		return nil, errors.Wrapf(err, "listing %s", dir)
	}

	if len(files) == 0 {
		return nil, errors.Wrapf(errRotationUnverifiable, "no *.pub in %s", dir)
	}

	keys := make([]string, 0, len(files))

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, errors.Wrapf(err, "reading %s", f)
		}

		keys = append(keys, string(data))
	}

	return keys, nil
}

func exists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))

	return err == nil
}

// addFiles fills the manifest's files: one download per archive named in
// checksums.txt, the checksums file itself, and the signature when the
// release has one beside it. Downloads are ordered by os then arch so the
// document is stable whatever order goreleaser built in.
func (b builder) addFiles(m *static.Manifest, dist, baseURL string) error {
	sums, err := readChecksums(filepath.Join(dist, static.ChecksumsFile))
	if err != nil {
		return err
	}

	m.Checksums = static.FileURL(baseURL, m.Tag, static.ChecksumsFile)

	if exists(dist, static.SignatureFile) {
		m.Signature = static.FileURL(baseURL, m.Tag, static.SignatureFile)
	}

	if err := addRotation(m, dist, baseURL); err != nil {
		return err
	}

	seen := map[string]string{}

	for _, name := range archivesIn(dist) {
		d, err := b.download(dist, baseURL, m.Tag, name, sums[name])
		if err != nil {
			return err
		}

		platform := d.OS + "/" + d.Arch
		if other, dup := seen[platform]; dup {
			return errors.Wrapf(errDuplicatePlatform, "%s: %s and %s", platform, other, name)
		}

		seen[platform] = name

		m.Downloads = append(m.Downloads, d)
	}

	sort.Slice(m.Downloads, func(i, j int) bool {
		if m.Downloads[i].OS != m.Downloads[j].OS {
			return m.Downloads[i].OS < m.Downloads[j].OS
		}

		return m.Downloads[i].Arch < m.Downloads[j].Arch
	})

	return nil
}

// download is one archive's row: its digest from checksums.txt, its size
// from the file, its platform from the binary inside.
func (b builder) download(dist, baseURL, tag, name, sum string) (static.Download, error) {
	if sum == "" {
		return static.Download{}, errors.Wrapf(errNoChecksum, "%s is not in %s", name, static.ChecksumsFile)
	}

	path := filepath.Join(dist, name)

	info, err := os.Stat(path)
	if err != nil {
		return static.Download{}, errors.Wrapf(err, "sizing %s", name)
	}

	goos, goarch, err := b.platform(path)
	if err != nil {
		return static.Download{}, errors.Wrapf(err, "platform of %s", name)
	}

	return static.Download{
		OS:     goos,
		Arch:   goarch,
		Name:   name,
		URL:    static.FileURL(baseURL, tag, name),
		Size:   info.Size(),
		SHA256: sum,
	}, nil
}

// archivesIn is the archives goreleaser wrote into dist, by name. The sbom
// and signature files beside them are not downloads.
func archivesIn(dist string) []string {
	var names []string

	for _, pattern := range []string{"*.tar.gz", "*.tgz", "*.zip"} {
		matches, _ := filepath.Glob(filepath.Join(dist, pattern))
		for _, m := range matches {
			names = append(names, filepath.Base(m))
		}
	}

	sort.Strings(names)

	return names
}

// readChecksums is checksums.txt as name to hex digest, in the two-column
// form sha256sum writes and goreleaser follows.
func readChecksums(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.Wrapf(errNoChecksum, "reading %s: %v", filepath.Base(path), err)
	}
	defer func() { _ = f.Close() }()

	sums := map[string]string{}
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != checksumFields {
			continue
		}

		sums[fields[1]] = fields[0]
	}

	if err := scanner.Err(); err != nil {
		return nil, errors.Wrapf(err, "reading %s", filepath.Base(path))
	}

	return sums, nil
}

func readJSON(path string, into any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return errors.Wrapf(err, "reading %s", filepath.Base(path))
	}

	if err := json.Unmarshal(raw, into); err != nil {
		return errors.Wrapf(err, "parsing %s", filepath.Base(path))
	}

	return nil
}
