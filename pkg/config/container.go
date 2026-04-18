package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

// Containable is the primary configuration interface. It provides typed
// accessors, observation for hot-reload, and schema validation.
type Containable interface {
	Get(key string) any
	GetBool(key string) bool
	GetInt(key string) int
	GetFloat(key string) float64
	GetString(key string) string
	GetTime(key string) time.Time
	GetDuration(key string) time.Duration
	// GetViper returns the underlying Viper instance for advanced operations
	// not exposed by the Containable interface. This is an intentional escape
	// hatch for power users who need Viper's full API (e.g., MergeConfig,
	// BindPFlag, or direct access to config file watching).
	//
	// Prefer the typed accessor methods (GetString, GetInt, etc.) for standard
	// configuration reads. Use GetViper only when the Containable interface
	// does not cover your use case.
	GetViper() *viper.Viper
	Has(key string) bool
	IsSet(key string) bool
	Set(key string, value any)
	WriteConfigAs(dest string) error
	Sub(key string) Containable
	AddObserver(o Observable)
	AddObserverFunc(f func(Containable, chan error))
	ToJSON() string
	Dump(w io.Writer)
	// Validate checks the container's current values against the provided schema.
	// Returns a ValidationResult; callers should check result.Valid().
	Validate(schema *Schema) *ValidationResult
}

// Container container for configuration.
//
// Sub-containers returned by [Container.Sub] track two Viper
// instances:
//
//   - viper (the "structural view") points at the sub-tree and is
//     used by Write/Dump/ToJSON/Validate operations so those
//     continue to scope correctly to the sub-path.
//   - root + prefix together identify the original container; every
//     Get/Set/Has/IsSet call is routed through root.viper with the
//     key qualified by the accumulated prefix. This is what keeps
//     Viper's AutomaticEnv + prefix-aware env binding alive across
//     Sub() calls — `cfg.Sub("github").GetString("auth.value")` now
//     honours `<TOOL_PREFIX>_GITHUB_AUTH_VALUE`, which a Viper-native
//     Sub would silently drop.
type Container struct {
	ID        string
	viper     *viper.Viper
	logger    logger.Logger
	observers []Observable
	schema    *Schema
	// root is nil on the top-level container and points at the
	// original container on every sub-container. Enables Get/Set
	// to reach the Viper instance that owns the env-binding setup.
	root *Container
	// prefix is the dot-separated path of this sub-container,
	// accumulated from every Sub() call since the root. Empty on
	// the top-level container.
	prefix string
}

// Get interface value from config.
func (c *Container) Get(key string) any {
	return c.resolverViper().Get(c.qualifyKey(key))
}

// GetBool get Bool value from config.
func (c *Container) GetBool(key string) bool {
	return c.resolverViper().GetBool(c.qualifyKey(key))
}

// GetInt get Bool value from config.
func (c *Container) GetInt(key string) int {
	return c.resolverViper().GetInt(c.qualifyKey(key))
}

// GetFloat get Float value from config.
func (c *Container) GetFloat(key string) float64 {
	return c.resolverViper().GetFloat64(c.qualifyKey(key))
}

// GetString get string value from config.
func (c *Container) GetString(key string) string {
	return c.resolverViper().GetString(c.qualifyKey(key))
}

// GetTime get time value from config.
func (c *Container) GetTime(key string) time.Time {
	return c.resolverViper().GetTime(c.qualifyKey(key))
}

// GetDuration get duration value from config.
func (c *Container) GetDuration(key string) time.Duration {
	return c.resolverViper().GetDuration(c.qualifyKey(key))
}

// GetViper retrieves the underlying Viper configuration. On
// sub-containers this returns the sub-tree structural view; use
// this only for bulk reads that do not need env binding (the full
// env-aware resolution pipeline is only available via the typed
// Get methods).
func (c *Container) GetViper() *viper.Viper {
	return c.viper
}

// Has reports whether the given key exists in the underlying
// configuration. Routed through the root container so env vars
// picked up by Viper's AutomaticEnv are counted as "set".
func (c *Container) Has(key string) bool {
	return c.resolverViper().InConfig(c.qualifyKey(key))
}

// IsSet checks if the key has been set (file, env, or flag).
func (c *Container) IsSet(key string) bool {
	return c.resolverViper().IsSet(c.qualifyKey(key))
}

// Set sets the value for the given key.
func (c *Container) Set(key string, value any) {
	c.resolverViper().Set(c.qualifyKey(key), value)
}

// resolverViper returns the Viper instance used for the
// Get/Set/Has/IsSet surface. Sub-containers return the root
// container's Viper so Viper's AutomaticEnv + prefix configuration
// fires; root containers return their own Viper.
func (c *Container) resolverViper() *viper.Viper {
	if c.root != nil {
		return c.root.viper
	}

	return c.viper
}

// qualifyKey prepends the sub-container's accumulated prefix to the
// key. Root containers (prefix == "") return the key unchanged.
func (c *Container) qualifyKey(key string) string {
	if c.prefix == "" {
		return key
	}

	return c.prefix + "." + key
}

// WriteConfigAs writes the current configuration to the given path.
func (c *Container) WriteConfigAs(dest string) error {
	return c.viper.WriteConfigAs(dest)
}

// Sub returns a view over a subtree of the parent configuration.
//
// Unlike Viper's native Sub — which constructs a fresh Viper
// instance that drops the parent's AutomaticEnv + prefix settings —
// this Sub returns a view that delegates every Get/Set/Has/IsSet
// call back to the root container's Viper with a fully-qualified
// key path. That means env-aware resolution (including prefix-aware
// env vars like `<TOOL>_GITHUB_AUTH_VALUE`) continues to apply no
// matter how many Sub() layers a caller walks through.
//
// The returned view's `viper` field is the Viper-native sub-tree
// used only by operations that legitimately need a scoped view:
// WriteConfigAs, Dump, ToJSON, and Validate.
//
// Returns nil when key is not present anywhere in the config
// hierarchy, matching Viper's Sub semantics.
func (c *Container) Sub(key string) Containable {
	root := c.root
	if root == nil {
		root = c
	}

	fullPrefix := key
	if c.prefix != "" {
		fullPrefix = c.prefix + "." + key
	}

	// Use the structural view for existence check so legacy
	// consumers see the same behaviour when no env or file value
	// is present. Viper's Sub also uses this path.
	subV := root.viper.Sub(fullPrefix)
	if subV == nil {
		return nil
	}

	return &Container{
		ID:        fmt.Sprintf("%s#%s", c.ID, key),
		viper:     subV,
		logger:    c.logger,
		observers: make([]Observable, 0),
		root:      root,
		prefix:    fullPrefix,
	}
}

func (c *Container) handleReadFileError(err error) {
	// just use the default value(s) if the config file was not found.
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		c.logger.Warn("could not load config file. Using default values")
		c.logger.Debug("config file error detail", "stacktrace", fmt.Sprintf("%+v", err))
	} else if err != nil { // Handle other errors that occurred while reading the config file
		c.logger.Warn(fmt.Sprintf("Could not read the config file (%s)", err))
		c.logger.Debug("config read error detail", "stacktrace", fmt.Sprintf("%+v", err))
	}
}

// SetSchema attaches a validation schema to the container.
// When set, hot-reload will validate config changes before notifying observers.
func (c *Container) SetSchema(schema *Schema) {
	c.schema = schema
}

// watchConfig monitor the changes in the config file.
func (c *Container) watchConfig() {
	c.viper.OnConfigChange(func(e fsnotify.Event) {
		c.logger.Info(fmt.Sprintf("Config updated %v", e))

		if c.schema != nil {
			result := c.Validate(c.schema)
			if !result.Valid() {
				c.logger.Error("config reload rejected: validation failed", "errors", result.Error())

				return
			}
		}

		errs := make(chan error)

		wg := &sync.WaitGroup{}
		for _, o := range c.observers {
			wg.Add(1)

			go func(o Observable, wg *sync.WaitGroup, errs chan error) {
				o.Run(c, errs)
				wg.Done()
			}(o, wg, errs)
		}

		wg.Wait()
	})
	c.viper.WatchConfig()
}

// AddObserver attach observer to trigger on config update.
func (c *Container) AddObserver(o Observable) {
	c.observers = append(c.observers, o)
}

// AddObserverFunc attach function to trigger on config update.
func (c *Container) AddObserverFunc(f func(Containable, chan error)) {
	c.observers = append(c.observers, Observer{f})
}

// GetObservers retrieve all currently attached Observers.
func (c *Container) GetObservers() []Observable {
	return c.observers
}

// Dump return config as json string.
func (c *Container) ToJSON() string {
	s := c.viper.AllSettings()

	bs, err := json.Marshal(s)
	if err != nil {
		c.logger.Error("unable to marshal config to JSON")
		c.logger.Debug("config marshal error detail", "stacktrace", fmt.Sprintf("%+v", err))
	}

	return string(bs)
}

func (c *Container) Dump(w io.Writer) {
	_, _ = fmt.Fprintln(w, c.ToJSON())
}
