package release

import (
	"sort"
	"sync"

	"github.com/cockroachdb/errors"

	"github.com/phpboyscout/go-tool-base/pkg/config"
)

// ProviderFactory is a function that constructs a release.Provider from a
// ReleaseSourceConfig and a Viper configuration subtree.
type ProviderFactory func(source ReleaseSourceConfig, cfg config.Containable) (Provider, error)

var (
	registryMu sync.RWMutex
	registry   = map[string]ProviderFactory{}
)

// Register associates a source type string with a ProviderFactory.
// Safe to call concurrently. Intended to be called from init() functions or
// early in main() before any Lookup call.
// Calling Register with an already-registered type overwrites the previous factory.
func Register(sourceType string, factory ProviderFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()

	registry[sourceType] = factory
}

// Lookup returns the ProviderFactory registered for the given source type.
// Returns ErrProviderNotFound if no factory has been registered for that type.
func Lookup(sourceType string) (ProviderFactory, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()

	factory, ok := registry[sourceType]
	if !ok {
		return nil, errors.WithHintf(
			ErrProviderNotFound,
			"No provider is registered for source type %q. Registered types: %v. "+
				"Register a custom provider with release.Register().",
			sourceType,
			registeredTypesSorted(),
		)
	}

	return factory, nil
}

// RegisteredTypes returns a sorted slice of all currently registered source
// type strings. Used for generating user-facing error messages.
func RegisteredTypes() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	return registeredTypesSorted()
}

// registeredTypesSorted returns sorted keys without acquiring the lock.
// Must be called with registryMu held (at least for reading).
func registeredTypesSorted() []string {
	types := make([]string, 0, len(registry))
	for t := range registry {
		types = append(types, t)
	}

	sort.Strings(types)

	return types
}
