package main

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"flag"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite the golden files")

var (
	january  = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	february = time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
)

func golden(t *testing.T, name string, got []byte) {
	t.Helper()

	path := filepath.Join("testdata", "golden", name)

	if *update {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, got, 0o600))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got), name)
}

// dataFile is the one built yum data file of a kind.
func dataFile(t *testing.T, files map[string][]byte, kind string) (name string, plain []byte) {
	t.Helper()

	for n, data := range files {
		if strings.HasSuffix(n, "-"+kind+".xml.gz") && !strings.HasPrefix(n, baseDir) {
			require.Empty(t, name, "one %s", kind)

			p, err := gunzip(data)
			require.NoError(t, err)

			name, plain = n, p
		}
	}

	require.NotEmpty(t, name, kind)

	return name, plain
}

var digest = regexp.MustCompile(`[0-9a-f]{64}`)

var (
	repomdSize  = regexp.MustCompile(`<size>\d+</size>`)
	releaseSize = regexp.MustCompile(` \d+ Packages\.gz`)
)

// The gzip bytes are the Go toolchain's, so the golden files hold what is
// inside them, with their digests and sizes masked. TestTheIndexesNameTheirFiles
// checks those.
func checkGolden(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()

	golden(t, dir+"/Packages", files["apt/Packages"])

	unzipped, err := gunzip(files["apt/Packages.gz"])
	require.NoError(t, err)
	assert.Equal(t, string(files["apt/Packages"]), string(unzipped))

	repomd := string(files["rpm/repodata/repomd.xml"])

	for _, d := range yumData {
		name, plain := dataFile(t, files, d.name)
		golden(t, dir+"/"+d.name+".xml", plain)

		repomd = strings.ReplaceAll(repomd, sha256Hex(files[name]), "ZIPPED")
	}

	golden(t, dir+"/repomd.xml", []byte(repomdSize.ReplaceAllString(repomd, "<size>ZIPPED</size>")))

	release := strings.ReplaceAll(string(files["apt/Release"]), sha256Hex(files["apt/Packages.gz"]), "ZIPPED")
	golden(t, dir+"/Release", []byte(releaseSize.ReplaceAllString(release, " ZIPPED Packages.gz")))
}

func TestTheFirstReleaseIsListedInNewIndexes(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	s.releaseFixtures(t, "1.0.0")

	files, err := s.build(t, "v1.0.0", january)
	require.NoError(t, err)

	checkGolden(t, "first", files)
	assert.Empty(t, slices.DeleteFunc(slices.Collect(maps.Keys(files)), func(n string) bool {
		return !strings.HasPrefix(n, baseDir)
	}), "nothing was published to start from")
}

func TestALaterReleaseIsAddedToThePublishedIndexes(t *testing.T) {
	t.Parallel()

	s := newStore(t)

	for _, v := range []string{"1.10.0", "1.1.0", "1.0.0"} {
		s.releaseFixtures(t, v)
	}

	first := s.publish(t, "v1.0.0", january)
	s.publish(t, "v1.10.0", january)

	files, err := s.build(t, "v1.1.0", february)
	require.NoError(t, err)

	checkGolden(t, "third", files)

	// What was read is kept for the publish to check the store against.
	for _, name := range []string{
		"apt/Packages", "apt/Packages.gz", "apt/Release", "apt/Release.gpg",
		"rpm/repodata/repomd.xml", "rpm/repodata/repomd.xml.asc",
	} {
		assert.Equal(t, string(s.get(t, name)), string(files[baseDir+name]), name)
	}

	assert.Contains(t, string(files["apt/Release"]), "\nDate: Sun, 01 Feb 2026 12:00:00 UTC\n")
	assert.Contains(t, string(files["rpm/repodata/repomd.xml"]), "<revision>"+strconv.FormatInt(february.Unix(), 10)+"</revision>")

	// A package keeps the text it was first published with.
	assert.True(t, strings.HasPrefix(string(files["apt/Packages"]), strings.TrimSuffix(string(first["apt/Packages"]), "\n")))
}

func TestTheIndexesNameTheirFiles(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	s.releaseFixtures(t, "1.0.0")
	files := s.publish(t, "v1.0.0", january)

	release := string(files["apt/Release"])
	for _, name := range []string{"Packages", "Packages.gz"} {
		data := files["apt/"+name]
		assert.Contains(t, release, "\n "+sha256Hex(data)+" "+strconv.Itoa(len(data))+" "+name+"\n")
	}

	assert.Equal(t, "Origin: gtb\nLabel: gtb\nDate: Thu, 01 Jan 2026 12:00:00 UTC\nArchitectures: amd64 arm64\nSHA256:\n",
		release[:strings.Index(release, "SHA256:\n")+len("SHA256:\n")])
	assert.NotContains(t, release, "Valid-Until")

	var repomd repomdXML
	require.NoError(t, xml.Unmarshal(files["rpm/repodata/repomd.xml"], &repomd))
	require.Len(t, repomd.Data, 3)

	for _, d := range repomd.Data {
		name, plain := dataFile(t, files, d.Type)
		zipped := files[name]

		assert.Equal(t, name, "rpm/"+d.Location.Href)
		assert.Equal(t, sha256Hex(zipped), d.Checksum.Value)
		assert.Equal(t, sha256Hex(plain), d.OpenChecksum.Value)
		assert.Equal(t, len(zipped), d.Size)
		assert.Equal(t, len(plain), d.OpenSize)
		assert.Equal(t, sha256Hex(zipped), digest.FindString(d.Location.Href))
	}
}

func TestARerunChangesNothing(t *testing.T) {
	t.Parallel()

	for name, rerun := range map[string]struct {
		tag  string
		date time.Time
	}{
		"of the newest tag":              {"v1.10.0", february},
		"of an earlier tag":              {"v1.0.0", january},
		"with a date before the indexes": {"v1.10.0", january.AddDate(-1, 0, 0)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := newStore(t)
			s.releaseFixtures(t, "1.0.0")
			s.releaseFixtures(t, "1.10.0")
			s.publish(t, "v1.0.0", january)
			want := s.publish(t, "v1.10.0", february)

			got, err := s.build(t, rerun.tag, rerun.date)
			require.NoError(t, err)
			assert.Equal(t, indexes(want), indexes(got))
		})
	}
}

// A Go release's compressor writes its own bytes for the same content, so the
// published ones are kept where the content is (spec 0097 D3).
func TestARerunKeepsWhatAnotherCompressorWrote(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	s.releaseFixtures(t, "1.0.0")
	s.releaseFixtures(t, "1.1.0")
	s.publish(t, "v1.0.0", january)
	s.recompress(t)

	again, err := s.build(t, "v1.0.0", january)
	require.NoError(t, err)

	for name, data := range indexes(again) {
		assert.Equal(t, sha256Hex(s.get(t, name)), sha256Hex(data), name)
	}

	// New content has no published bytes to keep.
	next, err := s.build(t, "v1.1.0", february)
	require.NoError(t, err)

	for _, d := range yumData {
		name, _ := dataFile(t, next, d.name)
		assert.NotEqual(t, s.dataFile(t, d.name), name)
	}
}

// recompress rewrites the published gzip files as another compressor would.
func (s store) recompress(t *testing.T) {
	t.Helper()

	stored := func(name string) (old, zipped []byte) {
		old = s.get(t, name)
		plain, err := gunzip(old)
		require.NoError(t, err)

		var b bytes.Buffer

		w, err := gzip.NewWriterLevel(&b, gzip.NoCompression)
		require.NoError(t, err)

		_, err = w.Write(plain)
		require.NoError(t, err)
		require.NoError(t, w.Close())

		return old, b.Bytes()
	}

	for _, d := range yumData {
		name := s.dataFile(t, d.name)
		old, zipped := stored(name)
		require.NoError(t, os.Remove(filepath.Join(s.dir, filepath.FromSlash(name))))
		s.put(t, strings.Replace(name, sha256Hex(old), sha256Hex(zipped), 1), zipped)
		s.rewriteIndex(t, "rpm/repodata/repomd.xml", old, zipped, "<size>%d</size>")
	}

	old, zipped := stored("apt/Packages.gz")
	s.put(t, "apt/Packages.gz", zipped)
	s.rewriteIndex(t, "apt/Release", old, zipped, " %d Packages.gz")
	s.resign(t)
}

func (s store) rewriteIndex(t *testing.T, index string, old, zipped []byte, size string) {
	t.Helper()

	text := strings.ReplaceAll(string(s.get(t, index)), sha256Hex(old), sha256Hex(zipped))
	text = strings.Replace(text, fmt.Sprintf(size, len(old)), fmt.Sprintf(size, len(zipped)), 1)
	s.put(t, index, []byte(text))
}

func TestTwoTagsInEitherOrderGiveTheSameIndexes(t *testing.T) {
	t.Parallel()

	key := newReleaseKey(t)
	in := func(first, second string) map[string][]byte {
		s := store{dir: t.TempDir(), key: key}
		s.releaseFixtures(t, "1.0.0")
		s.releaseFixtures(t, "1.1.0")

		dates := map[string]time.Time{"v1.0.0": january, "v1.1.0": february}
		s.publish(t, first, dates[first])

		return s.publish(t, second, dates[second])
	}

	assert.Equal(t, indexes(in("v1.0.0", "v1.1.0")), indexes(in("v1.1.0", "v1.0.0")))
}

func TestAReleaseThatDoesNotVerifyIsRefused(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		break_ func(t *testing.T, s store)
		want   error
	}{
		"checksums.txt changed": {func(t *testing.T, s store) {
			s.put(t, "v1.0.0/checksums.txt", append(s.get(t, "v1.0.0/checksums.txt"), '\n'))
		}, errUnverified},
		"checksums.txt signed by another key": {func(t *testing.T, s store) {
			s.put(t, "v1.0.0/checksums.txt.sig", newReleaseKey(t).sign(t, s.get(t, "v1.0.0/checksums.txt")))
		}, errUnverified},
		"no signature": {func(t *testing.T, s store) {
			require.NoError(t, os.Remove(filepath.Join(s.dir, "v1.0.0", "checksums.txt.sig")))
		}, fs.ErrNotExist},
		"no checksums.txt": {func(t *testing.T, s store) {
			require.NoError(t, os.Remove(filepath.Join(s.dir, "v1.0.0", "checksums.txt")))
		}, fs.ErrNotExist},
		"a deb changed": {func(t *testing.T, s store) {
			s.put(t, "v1.0.0/gtb_1.0.0_amd64.deb", fixture(t, "gtb_1.0.0_arm64.deb"))
		}, errUnverified},
		"an rpm changed": {func(t *testing.T, s store) {
			s.put(t, "v1.0.0/gtb-1.0.0-1.x86_64.rpm", fixture(t, "gtb-1.0.0-1.aarch64.rpm"))
		}, errUnverified},
		"a package missing": {func(t *testing.T, s store) {
			require.NoError(t, os.Remove(filepath.Join(s.dir, "v1.0.0", "gtb_1.0.0_amd64.deb")))
		}, fs.ErrNotExist},
		"checksums.txt is not a checksum list": {func(t *testing.T, s store) {
			s.putSigned(t, "v1.0.0/checksums.txt", "v1.0.0/checksums.txt.sig", []byte("Date: today\n"))
		}, errPackage},
		"checksums.txt lists a name twice": {func(t *testing.T, s store) {
			sums := s.get(t, "v1.0.0/checksums.txt")
			s.putSigned(t, "v1.0.0/checksums.txt", "v1.0.0/checksums.txt.sig", append(sums, sums...))
		}, errPackage},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := newStore(t)
			s.releaseFixtures(t, "1.0.0")
			c.break_(t, s)

			_, err := s.build(t, "v1.0.0", january)
			require.ErrorIs(t, err, c.want)
		})
	}
}

// The signed checksums say whose bytes a package is, not which release.
func TestAPackageThatIsNotTheReleasesOwnIsRefused(t *testing.T) {
	t.Parallel()

	for name, change := range map[string]func(t *testing.T, files map[string][]byte){
		"another release's deb": func(t *testing.T, files map[string][]byte) {
			files["gtb_1.1.0_amd64.deb"] = fixture(t, "gtb_1.1.0_amd64.deb")
		},
		"another release's rpm": func(t *testing.T, files map[string][]byte) {
			files["gtb-1.1.0-1.x86_64.rpm"] = fixture(t, "gtb-1.1.0-1.x86_64.rpm")
		},
		"another release's deb under this one's name": func(t *testing.T, files map[string][]byte) {
			files["gtb_1.0.0_amd64.deb"] = fixture(t, "gtb_1.1.0_amd64.deb")
		},
		"another release's rpm under this one's name": func(t *testing.T, files map[string][]byte) {
			files["gtb-1.0.0-1.x86_64.rpm"] = fixture(t, "gtb-1.1.0-1.x86_64.rpm")
		},
		"another architecture's deb under this one's name": func(t *testing.T, files map[string][]byte) {
			files["gtb_1.0.0_amd64.deb"] = fixture(t, "gtb_1.0.0_arm64.deb")
		},
		"another architecture's rpm under this one's name": func(t *testing.T, files map[string][]byte) {
			files["gtb-1.0.0-1.x86_64.rpm"] = fixture(t, "gtb-1.0.0-1.aarch64.rpm")
		},
		"a deb under another name": func(t *testing.T, files map[string][]byte) {
			files["renamed.deb"] = fixture(t, "gtb_1.0.0_amd64.deb")
		},
		"an rpm under another name": func(t *testing.T, files map[string][]byte) {
			files["renamed.rpm"] = fixture(t, "gtb-1.0.0-1.x86_64.rpm")
		},
		"an rpm as a deb": func(t *testing.T, files map[string][]byte) {
			files["gtb_1.0.0_amd64.deb"] = fixture(t, "gtb-1.0.0-1.x86_64.rpm")
		},
		"a deb as an rpm": func(t *testing.T, files map[string][]byte) {
			files["gtb-1.0.0-1.x86_64.rpm"] = fixture(t, "gtb_1.0.0_amd64.deb")
		},
		"no debs": func(_ *testing.T, files map[string][]byte) {
			maps.DeleteFunc(files, func(n string, _ []byte) bool { return strings.HasSuffix(n, ".deb") })
		},
		"no rpms": func(_ *testing.T, files map[string][]byte) {
			maps.DeleteFunc(files, func(n string, _ []byte) bool { return strings.HasSuffix(n, ".rpm") })
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			files := map[string][]byte{}
			for _, f := range fixturesOf("1.0.0") {
				files[f] = fixture(t, f)
			}

			change(t, files)

			s := newStore(t)
			s.release(t, "v1.0.0", files)

			_, err := s.build(t, "v1.0.0", january)
			require.ErrorIs(t, err, errPackage)
		})
	}
}

func TestAPackageNamedOutsideItsTagIsRefused(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	deb := fixture(t, "gtb_1.0.0_amd64.deb")
	s.put(t, "gtb_1.0.0_amd64.deb", deb)
	s.putSigned(t, "v1.0.0/checksums.txt", "v1.0.0/checksums.txt.sig", []byte(sha256Hex(deb)+"  ../gtb_1.0.0_amd64.deb\n"))

	_, err := s.build(t, "v1.0.0", january)
	require.ErrorIs(t, err, errPackage)
	assert.ErrorContains(t, err, `"../gtb_1.0.0_amd64.deb"`)
}

func TestOnlyAStableReleaseIsListed(t *testing.T) {
	t.Parallel()

	for _, tag := range []string{"v1.0.0-rc.1", "v1.0.0+build", "v1.0", "1.0.0", "latest", "", "../v1.0.0"} {
		t.Run(tag, func(t *testing.T) {
			t.Parallel()

			s := newStore(t)
			s.releaseFixtures(t, "1.0.0")

			_, err := s.build(t, tag, january)
			require.ErrorIs(t, err, errTag)
		})
	}
}

func TestEntriesAreInVersionOrder(t *testing.T) {
	t.Parallel()

	of := func(version, arch string) entry {
		return entry{name: "gtb", version: version, arch: arch, sha: version + arch}
	}

	got, err := merge(
		[]entry{of("1.10.0", "arm64"), of("1.9.0", "arm64")},
		[]entry{of("1.10.0", "amd64"), of("1.9.0", "amd64"), of("1.9.0", "arm64")},
	)
	require.NoError(t, err)
	assert.Equal(t, []entry{of("1.9.0", "amd64"), of("1.9.0", "arm64"), of("1.10.0", "amd64"), of("1.10.0", "arm64")}, got)
}

func TestAPublishedVersionCannotChange(t *testing.T) {
	t.Parallel()

	for _, ext := range []string{".deb", ".rpm"} {
		t.Run(ext, func(t *testing.T) {
			t.Parallel()

			s := newStore(t)
			s.releaseFixtures(t, "1.0.0")
			s.publish(t, "v1.0.0", january)

			files := map[string][]byte{}
			for _, f := range fixturesOf("1.0.0") {
				files[f] = fixture(t, f)
				if strings.HasSuffix(f, ext) {
					// Bytes after the payload, which no reader looks at.
					files[f] = append(files[f], 0)
				}
			}

			s.release(t, "v1.0.0", files)

			_, err := s.build(t, "v1.0.0", january)
			require.ErrorIs(t, err, errRedefined)
		})
	}
}

// The yum data file of a kind, where the store has it.
func (s store) dataFile(t *testing.T, kind string) string {
	t.Helper()

	found, err := filepath.Glob(filepath.Join(s.dir, "rpm", "repodata", "*-"+kind+".xml.gz"))
	require.NoError(t, err)
	require.Len(t, found, 1)

	rel, err := filepath.Rel(s.dir, found[0])
	require.NoError(t, err)

	return filepath.ToSlash(rel)
}

// rewrite edits a published file and signs the indexes again, as the release
// key would for a file it had made.
func (s store) rewrite(t *testing.T, name, old, replacement string) {
	t.Helper()

	before := string(s.get(t, name))
	require.Contains(t, before, old)
	s.put(t, name, []byte(strings.Replace(before, old, replacement, 1)))
	s.resign(t)
}

// rewriteData replaces a published yum data file and names it in repomd.xml.
func (s store) rewriteData(t *testing.T, kind string, plain []byte) {
	t.Helper()

	zipped, err := gzipBytes(plain)
	require.NoError(t, err)

	old := s.dataFile(t, kind)
	name := "rpm/repodata/" + sha256Hex(zipped) + "-" + kind + ".xml.gz"
	s.put(t, name, zipped)

	repomd := strings.ReplaceAll(string(s.get(t, "rpm/repodata/repomd.xml")), digest.FindString(old), sha256Hex(zipped))
	s.put(t, "rpm/repodata/repomd.xml", []byte(repomd))
	s.resign(t)
}

func TestAPublishedIndexThatDoesNotReadIsNotReplaced(t *testing.T) {
	t.Parallel()

	remove := func(name string) func(t *testing.T, s store) {
		return func(t *testing.T, s store) {
			require.NoError(t, os.Remove(filepath.Join(s.dir, filepath.FromSlash(name))))
		}
	}

	for name, c := range map[string]struct {
		change func(t *testing.T, s store)
		want   error
		says   string
	}{
		"Release changed": {func(t *testing.T, s store) {
			s.put(t, "apt/Release", append(s.get(t, "apt/Release"), '\n'))
		}, errUnverified, ""},
		"repomd.xml changed": {func(t *testing.T, s store) {
			s.put(t, "rpm/repodata/repomd.xml", append(s.get(t, "rpm/repodata/repomd.xml"), '\n'))
		}, errUnverified, ""},
		"Release signed by another key": {func(t *testing.T, s store) {
			s.put(t, "apt/Release.gpg", newReleaseKey(t).sign(t, s.get(t, "apt/Release")))
		}, errUnverified, ""},
		"repomd.xml signed by another key": {func(t *testing.T, s store) {
			s.put(t, "rpm/repodata/repomd.xml.asc", newReleaseKey(t).sign(t, s.get(t, "rpm/repodata/repomd.xml")))
		}, errUnverified, ""},
		"no Release":        {remove("apt/Release"), errUnverified, "apt/Release.gpg is published and apt/Release is not"},
		"no repomd.xml":     {remove("rpm/repodata/repomd.xml"), errUnverified, "is published and"},
		"no Release.gpg":    {remove("apt/Release.gpg"), fs.ErrNotExist, ""},
		"no repomd.xml.asc": {remove("rpm/repodata/repomd.xml.asc"), fs.ErrNotExist, ""},
		"no Packages":       {remove("apt/Packages"), fs.ErrNotExist, ""},
		"no Packages.gz":    {remove("apt/Packages.gz"), fs.ErrNotExist, ""},
		"Packages is not Release's": {func(t *testing.T, s store) {
			s.rewrite(t, "apt/Packages", "Section: devel", "Section: other")
		}, errUnverified, ""},
		"Packages.gz is not Release's": {func(t *testing.T, s store) {
			s.put(t, "apt/Packages.gz", append(s.get(t, "apt/Packages.gz"), 0))
		}, errUnverified, ""},
		"Packages.gz is not gzip": {func(t *testing.T, s store) {
			old := s.get(t, "apt/Packages.gz")
			s.put(t, "apt/Packages.gz", []byte("plain"))
			s.rewrite(t, "apt/Release", sha256Hex(old), sha256Hex([]byte("plain")))
		}, errPackage, "apt/Packages.gz"},
		"primary is not repomd.xml's": {func(t *testing.T, s store) {
			s.put(t, s.dataFile(t, "primary"), s.get(t, s.dataFile(t, "other")))
		}, errUnverified, ""},
		"other is not repomd.xml's": {func(t *testing.T, s store) {
			s.put(t, s.dataFile(t, "other"), s.get(t, s.dataFile(t, "primary")))
		}, errUnverified, ""},
		"Release has no date": {func(t *testing.T, s store) {
			s.rewrite(t, "apt/Release", "Date:", "Dated:")
		}, errIndex, ""},
		"Release's hashes do not read": {func(t *testing.T, s store) {
			s.rewrite(t, "apt/Release", " Packages\n", "\n")
		}, errIndex, ""},
		"Release does not read": {func(t *testing.T, s store) {
			s.rewrite(t, "apt/Release", "Origin:", "Origin")
		}, errPackage, ""},
		"repomd.xml does not read": {func(t *testing.T, s store) {
			s.rewrite(t, "rpm/repodata/repomd.xml", "</repomd>", "")
		}, errIndex, ""},
		"repomd.xml names a file elsewhere": {func(t *testing.T, s store) {
			s.rewrite(t, "rpm/repodata/repomd.xml", `href="repodata/`, `href="../repodata/`)
		}, errIndex, ""},
		"repomd.xml has no primary": {func(t *testing.T, s store) {
			s.rewrite(t, "rpm/repodata/repomd.xml", `type="primary"`, `type="renamed"`)
		}, errIndex, ""},
		"repomd.xml has two primaries": {func(t *testing.T, s store) {
			repomd := string(s.get(t, "rpm/repodata/repomd.xml"))
			_, rest, _ := strings.Cut(repomd, "  <data ")
			block := "  <data " + strings.SplitAfter(rest, "</data>\n")[0]
			s.rewrite(t, "rpm/repodata/repomd.xml", "</repomd>", block+"</repomd>")
		}, errIndex, ""},
		"repomd.xml has no other": {func(t *testing.T, s store) {
			_, last, found := strings.Cut(string(s.get(t, "rpm/repodata/repomd.xml")), `  <data type="other"`)
			require.True(t, found)
			s.rewrite(t, "rpm/repodata/repomd.xml", `  <data type="other"`+last, "</repomd>\n")
		}, errIndex, ""},
		"repomd.xml names nothing": {func(t *testing.T, s store) {
			s.put(t, "rpm/repodata/repomd.xml", []byte(`<repomd xmlns="http://linux.duke.edu/metadata/repo"/>`))
			s.resign(t)
		}, errIndex, ""},
		"primary does not read": {func(t *testing.T, s store) {
			s.rewriteData(t, "primary", []byte("<metadata>"))
		}, errIndex, ""},
		"a package is missing from filelists": {func(t *testing.T, s store) {
			s.rewriteData(t, "filelists", []byte("<filelists/>"))
		}, errIndex, ""},
		"a package is missing from primary": {func(t *testing.T, s store) {
			s.rewriteData(t, "primary", []byte("<metadata/>"))
		}, errIndex, ""},
		"a package in primary has no name": {func(t *testing.T, s store) {
			_, plain := s.plain(t, "primary")
			s.rewriteData(t, "primary", []byte(strings.Replace(plain, "<name>gtb</name>", "", 1)))
		}, errPackage, ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := newStore(t)
			s.releaseFixtures(t, "1.0.0")
			s.releaseFixtures(t, "1.1.0")
			s.publish(t, "v1.0.0", january)
			c.change(t, s)

			files, err := s.build(t, "v1.1.0", february)
			require.ErrorIs(t, err, c.want)
			require.ErrorContains(t, err, c.says)
			assert.Nil(t, files)
		})
	}
}

// An index file that cannot be read is not one that isn't there: taking it as
// missing would publish indexes that list only the new release.
func TestAnIndexThatCannotBeReadIsNotReplaced(t *testing.T) {
	t.Parallel()

	status := func(code int) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
	}

	answers := map[string]http.HandlerFunc{
		"forbidden":   status(http.StatusForbidden),
		"an error":    status(http.StatusInternalServerError),
		"unavailable": status(http.StatusServiceUnavailable),
		"no content":  status(http.StatusNoContent),
		"redirected":  status(http.StatusFound),
		"cut short": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "100")
			_, _ = w.Write([]byte("Origin"))
		},
		"dropped": func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) },
	}

	s := newStore(t)
	s.releaseFixtures(t, "1.0.0")
	s.releaseFixtures(t, "1.1.0")
	s.publish(t, "v1.0.0", january)

	primary := s.dataFile(t, "primary")

	for what, c := range map[string]struct {
		unreadable, missing []string
	}{
		"Release":                     {unreadable: []string{"apt/Release"}},
		"Release.gpg":                 {unreadable: []string{"apt/Release.gpg"}},
		"Release with no signature":   {[]string{"apt/Release"}, []string{"apt/Release.gpg"}},
		"Release.gpg with no Release": {[]string{"apt/Release.gpg"}, []string{"apt/Release"}},
		"both apt files":              {unreadable: []string{"apt/Release", "apt/Release.gpg"}},
		"Packages":                    {unreadable: []string{"apt/Packages"}},
		"Packages.gz":                 {unreadable: []string{"apt/Packages.gz"}},
		"repomd.xml":                  {unreadable: []string{"rpm/repodata/repomd.xml"}},
		"repomd.xml.asc":              {unreadable: []string{"rpm/repodata/repomd.xml.asc"}},
		"repomd.xml, unsigned":        {[]string{"rpm/repodata/repomd.xml"}, []string{"rpm/repodata/repomd.xml.asc"}},
		"its signature alone":         {[]string{"rpm/repodata/repomd.xml.asc"}, []string{"rpm/repodata/repomd.xml"}},
		"both yum files":              {unreadable: []string{"rpm/repodata/repomd.xml", "rpm/repodata/repomd.xml.asc"}},
		"a yum data file":             {unreadable: []string{primary}},
		"the checksums":               {unreadable: []string{"v1.1.0/checksums.txt"}},
		"the checksums' signature":    {unreadable: []string{"v1.1.0/checksums.txt.sig"}},
		"a package":                   {unreadable: []string{"v1.1.0/gtb_1.1.0_amd64.deb"}},
	} {
		for how, answer := range answers {
			t.Run(what+" "+how, func(t *testing.T) {
				t.Parallel()

				files := http.FileServerFS(os.DirFS(s.dir))
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					name := strings.TrimPrefix(r.URL.Path, "/")

					switch {
					case slices.Contains(c.unreadable, name):
						answer(w, r)
					case slices.Contains(c.missing, name):
						http.NotFound(w, r)
					default:
						files.ServeHTTP(w, r)
					}
				}))
				t.Cleanup(server.Close)

				src := newHTTPSource(server.URL)
				src.client.Transport = server.Client().Transport

				got, err := build(t.Context(), src, s.key.trust(t), release{tag: "v1.1.0", origin: "gtb", date: february})
				require.Error(t, err)
				require.NotErrorIs(t, err, fs.ErrNotExist)
				assert.Nil(t, got)
			})
		}
	}
}

func (s store) plain(t *testing.T, kind string) (name, text string) {
	t.Helper()

	name = s.dataFile(t, kind)
	plain, err := gunzip(s.get(t, name))
	require.NoError(t, err)

	return name, string(plain)
}

// A published Packages this command cannot read is refused, though its hashes
// and signature are right.
func TestAPublishedPackagesThatDoesNotReadIsNotReplaced(t *testing.T) {
	t.Parallel()

	for name, edit := range map[string][2]string{
		"a line that is no field":  {"Section: devel", "Section devel"},
		"an entry with no version": {"Version: 1.0.0\n", ""},
		"a field set twice":        {"Section: devel", "Version: 9.9.9"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := newStore(t)
			s.releaseFixtures(t, "1.0.0")
			s.publish(t, "v1.0.0", january)

			packages := strings.Replace(string(s.get(t, "apt/Packages")), edit[0], edit[1], 1)
			zipped, err := gzipBytes([]byte(packages))
			require.NoError(t, err)

			s.put(t, "apt/Packages", []byte(packages))
			s.put(t, "apt/Packages.gz", zipped)
			s.put(t, "apt/Release", []byte("Date: Thu, 01 Jan 2026 12:00:00 UTC\nSHA256:\n "+
				sha256Hex([]byte(packages))+" 1 Packages\n "+sha256Hex(zipped)+" 1 Packages.gz\n"))
			s.resign(t)

			_, err = s.build(t, "v1.0.0", january)
			require.ErrorIs(t, err, errPackage)
		})
	}
}
