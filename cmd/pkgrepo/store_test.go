package main

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/signing/verify"
)

// releaseKey is a throwaway signing key.
type releaseKey struct {
	entity *openpgp.Entity
	public []byte
}

func newReleaseKey(t *testing.T) releaseKey {
	t.Helper()

	// RSA, as gtb's release keys are.
	entity, err := openpgp.NewEntity("Test", "", "test@example.test", &packet.Config{
		Algorithm: packet.PubKeyAlgoRSA, RSABits: 3072,
		// Dated back, so a container whose clock is behind accepts it.
		Time: func() time.Time { return time.Now().Add(-time.Hour) },
	})
	require.NoError(t, err)

	var b bytes.Buffer

	w, err := armor.Encode(&b, openpgp.PublicKeyType, nil)
	require.NoError(t, err)
	require.NoError(t, entity.Serialize(w))
	require.NoError(t, w.Close())

	return releaseKey{entity: entity, public: b.Bytes()}
}

func (k releaseKey) sign(t *testing.T, data []byte) []byte {
	t.Helper()

	var b bytes.Buffer

	require.NoError(t, openpgp.ArmoredDetachSign(&b, k.entity, bytes.NewReader(data), nil))

	return b.Bytes()
}

func (k releaseKey) trust(t *testing.T) *verify.TrustSet {
	t.Helper()

	set, err := verify.LoadTrustSet(k.public)
	require.NoError(t, err)

	return set
}

// store is a release store on disk, as the tag pipeline leaves it.
type store struct {
	dir string
	key releaseKey
}

func newStore(t *testing.T) store {
	t.Helper()

	return store{dir: t.TempDir(), key: newReleaseKey(t)}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", "packages", name))
	require.NoError(t, err)

	return data
}

func fixturesOf(version string) []string {
	return []string{
		"gtb-" + version + "-1.aarch64.rpm", "gtb-" + version + "-1.x86_64.rpm",
		"gtb_" + version + "_amd64.deb", "gtb_" + version + "_arm64.deb",
	}
}

func (s store) put(t *testing.T, name string, data []byte) {
	t.Helper()

	path := filepath.Join(s.dir, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, data, 0o600))
}

func (s store) get(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(s.dir, filepath.FromSlash(name)))
	require.NoError(t, err)

	return data
}

func (s store) putSigned(t *testing.T, name, sig string, data []byte) {
	t.Helper()

	s.put(t, name, data)
	s.put(t, sig, s.key.sign(t, data))
}

// release publishes a tag holding files, by name, with signed checksums.
func (s store) release(t *testing.T, tag string, files map[string][]byte) {
	t.Helper()

	var sums strings.Builder

	for _, name := range slices.Sorted(maps.Keys(files)) {
		s.put(t, tag+"/"+name, files[name])
		fmt.Fprintf(&sums, "%s  %s\n", sha256Hex(files[name]), name)
	}

	s.putSigned(t, tag+"/checksums.txt", tag+"/checksums.txt.sig", []byte(sums.String()))
}

// releaseFixtures publishes a version's four fixture packages, beside an
// archive, which is not listed.
func (s store) releaseFixtures(t *testing.T, version string) {
	t.Helper()

	files := map[string][]byte{"gtb_Linux_x86_64.tar.gz": []byte("an archive")}
	for _, name := range fixturesOf(version) {
		files[name] = fixture(t, name)
	}

	s.release(t, "v"+version, files)
}

func (s store) build(t *testing.T, tag string, date time.Time) (map[string][]byte, error) {
	t.Helper()

	return build(t.Context(), dirSource(s.dir), s.key.trust(t), release{tag: tag, origin: "gtb", date: date})
}

// publish builds a tag's indexes, signs them and puts them in the store.
func (s store) publish(t *testing.T, tag string, date time.Time) map[string][]byte {
	t.Helper()

	files, err := s.build(t, tag, date)
	require.NoError(t, err)

	for name, data := range files {
		if !strings.HasPrefix(name, baseDir) {
			s.put(t, name, data)
		}
	}

	s.resign(t)

	return files
}

func (s store) resign(t *testing.T) {
	t.Helper()

	s.put(t, "apt/Release.gpg", s.key.sign(t, s.get(t, "apt/Release")))
	s.put(t, "rpm/repodata/repomd.xml.asc", s.key.sign(t, s.get(t, "rpm/repodata/repomd.xml")))
}

// indexes are the built files, less the base copies.
func indexes(files map[string][]byte) map[string][]byte {
	out := map[string][]byte{}

	for name, data := range files {
		if !strings.HasPrefix(name, baseDir) {
			out[name] = data
		}
	}

	return out
}
