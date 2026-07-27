package setup

import (
	"crypto/sha256"
	"encoding/hex"
	iofs "io/fs"
	"path/filepath"
	"sort"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"
)

// trustStoreFilename is the per-user record of project-local config files the
// user has explicitly trusted. It lives beside the tool's own config in the
// default config directory, never in a repository, so a clone cannot pre-seed
// its own trust.
const trustStoreFilename = "trusted-projects.yaml"

// dirPermTrust is the owner-only mode for the trust store directory. The store
// gates whether a repository file may change security posture, so it must not
// be group- or world-writable.
const dirPermTrust = 0o700

// filePermTrust is the owner-only mode for the trust store file itself.
const filePermTrust = 0o600

// trustStore maps the absolute path of a trusted project-local config file to
// the SHA-256 hash of the exact content that was trusted. Keying on content, not
// just path, is what makes trust direnv-style: editing a trusted file (or a
// clone replacing it) invalidates the record until the user re-trusts it.
type trustStore struct {
	Trusted map[string]string `yaml:"trusted"`
}

// DiscoverProjectConfig walks up from startDir looking for a project-local
// config file named ".<tool>.yaml" (e.g. .keryx.yaml), returning its path or ""
// if none is found before the filesystem root. This is a repo-root project
// config layer — a convention like .editorconfig — that the framework appends
// as the highest-precedence file layer. Generic across tools; a tool opts out
// simply by not having the file.
func DiscoverProjectConfig(fs afero.Fs, toolName, startDir string) string {
	if toolName == "" || startDir == "" {
		return ""
	}

	name := "." + toolName + ".yaml"

	for dir := startDir; ; {
		candidate := filepath.Join(dir, name)
		if _, serr := fs.Stat(candidate); serr == nil {
			return candidate
		}

		parent := filepath.Dir(dir)
		if parent == dir { // reached the filesystem root
			return ""
		}

		dir = parent
	}
}

// trustStorePath returns the absolute path of the per-user trust store for the
// named tool, or "" when the home directory cannot be resolved.
func trustStorePath(fs afero.Fs, toolName string) string {
	dir := GetDefaultConfigDir(fs, toolName)
	if dir == "" {
		return ""
	}

	return filepath.Join(dir, trustStoreFilename)
}

// loadTrustStore reads the tool's trust store. A missing store is not an error —
// it is an empty store, the default state before the user has trusted anything.
func loadTrustStore(fs afero.Fs, toolName string) (*trustStore, error) {
	store := &trustStore{Trusted: map[string]string{}}

	path := trustStorePath(fs, toolName)
	if path == "" {
		return store, nil
	}

	data, err := afero.ReadFile(fs, path)
	if err != nil {
		if errors.Is(err, iofs.ErrNotExist) {
			return store, nil
		}

		return nil, errors.Wrap(err, "reading trust store")
	}

	if err := yaml.Unmarshal(data, store); err != nil {
		return nil, errors.Wrap(err, "parsing trust store")
	}

	if store.Trusted == nil {
		store.Trusted = map[string]string{}
	}

	return store, nil
}

// saveTrustStore persists the trust store with owner-only permissions.
func saveTrustStore(fs afero.Fs, toolName string, store *trustStore) error {
	path := trustStorePath(fs, toolName)
	if path == "" {
		return errors.New("cannot resolve config directory for trust store (is HOME set?)")
	}

	if err := fs.MkdirAll(filepath.Dir(path), dirPermTrust); err != nil {
		return errors.Wrap(err, "creating config directory for trust store")
	}

	data, err := yaml.Marshal(store)
	if err != nil {
		return errors.Wrap(err, "encoding trust store")
	}

	if err := afero.WriteFile(fs, path, data, filePermTrust); err != nil {
		return errors.Wrap(err, "writing trust store")
	}

	return nil
}

// hashProjectConfig returns the hex SHA-256 of the file at path.
func hashProjectConfig(fs afero.Fs, path string) (string, error) {
	data, err := afero.ReadFile(fs, path)
	if err != nil {
		return "", errors.Wrap(err, "reading project config for hashing")
	}

	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:]), nil
}

// IsProjectConfigTrusted reports whether the project-local config file at path
// is currently trusted for the named tool: it must be recorded in the trust
// store AND its content hash must still match what was trusted. A file that was
// trusted and then edited (or swapped by a fresh clone) is no longer trusted,
// so its security-sensitive keys go back to being ignored.
//
// A missing store, a path not in the store, or an unreadable file all resolve
// to "not trusted" — the safe default. Only an unexpected I/O fault reading the
// store surfaces as an error.
func IsProjectConfigTrusted(fs afero.Fs, toolName, path string) (bool, error) {
	if path == "" {
		return false, nil
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}

	store, err := loadTrustStore(fs, toolName)
	if err != nil {
		return false, err
	}

	want, ok := store.Trusted[abs]
	if !ok {
		return false, nil
	}

	got, err := hashProjectConfig(fs, abs)
	if err != nil {
		// The file cannot be read now — treat as untrusted rather than
		// erroring the whole bootstrap.
		return false, nil //nolint:nilerr // unreadable file is "not trusted", not a fault
	}

	return got == want, nil
}

// TrustProjectConfig records the current content of the project-local config
// file at path as trusted for the named tool. This is the explicit user action —
// the `config trust` command — that unlocks the file's security-sensitive keys.
func TrustProjectConfig(fs afero.Fs, toolName, path string) error {
	if path == "" {
		return errors.New("no project-local config file to trust")
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}

	hash, err := hashProjectConfig(fs, abs)
	if err != nil {
		return err
	}

	store, err := loadTrustStore(fs, toolName)
	if err != nil {
		return err
	}

	store.Trusted[abs] = hash

	return saveTrustStore(fs, toolName, store)
}

// UntrustProjectConfig removes any trust record for the file at path. It is a
// no-op (nil error) when the path was not trusted.
func UntrustProjectConfig(fs afero.Fs, toolName, path string) error {
	if path == "" {
		return nil
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}

	store, err := loadTrustStore(fs, toolName)
	if err != nil {
		return err
	}

	if _, ok := store.Trusted[abs]; !ok {
		return nil
	}

	delete(store.Trusted, abs)

	return saveTrustStore(fs, toolName, store)
}

// ListTrustedProjects returns the absolute paths of every trusted project-local
// config file for the named tool, sorted for stable output.
func ListTrustedProjects(fs afero.Fs, toolName string) ([]string, error) {
	store, err := loadTrustStore(fs, toolName)
	if err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(store.Trusted))
	for p := range store.Trusted {
		paths = append(paths, p)
	}

	sort.Strings(paths)

	return paths, nil
}
