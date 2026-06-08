package signing

import (
	"sort"
	"strings"
	"sync"

	"github.com/cockroachdb/errors"
)

//nolint:gochecknoglobals // package-level registry is the explicit design (matches net/http/pprof and pkg/credentials/keychain)
var (
	mu       sync.RWMutex
	backends = map[string]Backend{}
)

// Register adds b to the global backend registry. Called from each
// backend package's init() so that blank-importing the package
// activates the backend.
//
// Panics on a duplicate name — duplicates indicate a programming
// error at process init (two backend packages claiming the same
// identifier) and failing fast is preferable to silent override.
//
// Panics on a nil Backend or one whose Name() is empty. Both are
// programmer errors that should never reach a deployed binary.
func Register(b Backend) {
	if b == nil {
		panic("signing: Register called with nil Backend")
	}

	name := b.Name()
	if name == "" {
		panic("signing: Backend.Name() returned empty string")
	}

	mu.Lock()
	defer mu.Unlock()

	if _, exists := backends[name]; exists {
		panic("signing: duplicate Backend registration: " + name)
	}

	backends[name] = b
}

// Get returns the registered backend with the given name. If no such
// backend is registered, returns ErrUnknownBackend wrapped with a
// listing of the available names so the user can immediately see
// what their binary actually supports.
func Get(name string) (Backend, error) {
	mu.RLock()
	defer mu.RUnlock()

	if b, ok := backends[name]; ok {
		return b, nil
	}

	available := namesLocked()
	if len(available) == 0 {
		return nil, errors.Wrapf(ErrUnknownBackend, "%q (no backends are registered — this binary was built without any signing backends compiled in)", name)
	}

	return nil, errors.Wrapf(ErrUnknownBackend, "%q (available: %s)", name, strings.Join(available, ", "))
}

// Names returns the names of all registered backends, sorted
// alphabetically. Used by `gtb keys mint --help` to enumerate the
// accepted --backend values for the current binary.
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()

	return namesLocked()
}

// namesLocked must be called with mu held (read or write).
func namesLocked() []string {
	out := make([]string, 0, len(backends))
	for name := range backends {
		out = append(out, name)
	}

	sort.Strings(out)

	return out
}

// resetForTesting clears the registry. Test-only — never call from
// production code. Exported via export_test.go so external callers
// can't reach it.
func resetForTesting() {
	mu.Lock()
	defer mu.Unlock()

	backends = map[string]Backend{}
}
