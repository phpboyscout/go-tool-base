package config

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/afero"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
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
	Unmarshal(target any) error
	UnmarshalKey(key string, target any) error
	// GetViper returns the underlying Viper instance for advanced operations
	// not exposed by the Containable interface. This is an intentional escape
	// hatch for power users who need Viper's full API (e.g., MergeConfig,
	// BindPFlag, or direct access to config file watching).
	//
	// Prefer the typed accessor methods (GetString, GetInt, etc.) for standard
	// configuration reads. Use GetViper only when the Containable interface
	// does not cover your use case.
	GetViper() *viper.Viper
	// BindPFlag binds a single pflag to the given configuration key. Once
	// bound, the flag's value participates in Viper's precedence resolution,
	// sitting above environment variables and file config. Callers should only
	// bind flags the user explicitly changed (flag.Changed) so that a flag left
	// at its default does not clobber configured values.
	BindPFlag(key string, flag *pflag.Flag) error
	Has(key string) bool
	IsSet(key string) bool
	SectionExists(key string) bool
	Set(key string, value any)
	WriteConfigAs(dest string) error
	// ConfigFiles returns the ordered list of config files that
	// contributed to this container's live configuration, in merge
	// order (lowest to highest precedence among file sources). Returns
	// an empty slice for reader/embedded containers that were not loaded
	// from any file. The slice is a copy; mutating it does not affect
	// the container's internal state.
	ConfigFiles() []string
	Sub(key string) Containable
	AddObserver(o Observable)
	AddObserverFunc(f func(Containable) error)
	// OnReloadError registers a callback invoked whenever a hot-reload is
	// rejected (candidate build or schema validation failed) and the
	// last-known-good config is retained. It is never called for a
	// successful reload; observers handle that case.
	OnReloadError(f func(error))
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
	ID     string
	viper  *viper.Viper
	logger *slog.Logger
	// mu guards viper (swapped on reload), observers, and schema
	// against concurrent access from the watcher goroutine.
	mu        sync.Mutex
	observers []Observable
	schema    *Schema
	// reloadErrFuncs are callbacks invoked when a reload is rejected
	// (candidate build or validation failed). Guarded by mu, copied
	// under lock, and invoked outside it (same discipline as observers).
	reloadErrFuncs []func(error)
	// fs and configFiles record the filesystem and ordered list of
	// config files this container was loaded from, so the watcher can
	// re-read and re-merge them on change. Empty for reader/embedded
	// containers, which are not watched.
	fs          afero.Fs
	configFiles []string
	// reloadDebounce is the coalescing window for hot-reload events.
	reloadDebounce time.Duration
	// watcher is the container-owned fsnotify watcher. nil until
	// watchConfig starts it; closed by Close.
	watcher *fsnotify.Watcher
	// watchDone is closed to signal the watch goroutine to stop.
	watchDone chan struct{}
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

// BindPFlag binds a single pflag to the given configuration key.
//
// Binding is routed through the same Viper instance used for value
// resolution (the root container's Viper on sub-containers), and the key is
// qualified with the sub-container's prefix, so a bound flag is visible to the
// typed Get methods at Viper's documented precedence (flag > env > file).
//
// Bind only flags the user explicitly changed (flag.Changed); binding a flag
// at its default value would clobber configured values — Viper's classic
// default-clobber footgun.
func (c *Container) BindPFlag(key string, flag *pflag.Flag) error {
	if flag == nil {
		return errors.Newf("cannot bind nil flag to key %q", key)
	}

	if err := c.resolverViper().BindPFlag(c.qualifyKey(key), flag); err != nil {
		return errors.Wrapf(err, "failed to bind flag %q to config key %q", flag.Name, key)
	}

	return nil
}

// Has reports whether the given key is present in the loaded file
// configuration. It is backed by Viper's InConfig, which inspects only
// keys read from a config file — env vars picked up by AutomaticEnv,
// flag bindings, and Set overrides are not counted. Use IsSet for a
// presence check that spans file, env, and flag sources.
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
//
// The viper pointer is read under the owning container's lock because
// hot-reload swaps it atomically on a successful reload.
func (c *Container) resolverViper() *viper.Viper {
	if c.root != nil {
		return c.root.liveViper()
	}

	return c.liveViper()
}

// liveViper returns the container's current viper instance, read under
// the container lock so concurrent reload swaps are observed safely.
func (c *Container) liveViper() *viper.Viper {
	c.mu.Lock()
	defer c.mu.Unlock()

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
	return c.liveViper().WriteConfigAs(dest)
}

// ConfigFiles returns the ordered list of config files that contributed
// to this container's live configuration, in merge order (lowest to
// highest precedence among file sources). Returns an empty slice for
// reader/embedded containers that were not loaded from any file.
//
// The returned slice is a copy taken under the container lock (same
// discipline as reload), so callers may not mutate the container's
// internal state through it and concurrent reload swaps are observed
// safely. Sub-containers report the root container's files, since the
// file set is a property of the loaded configuration as a whole.
func (c *Container) ConfigFiles() []string {
	owner := c
	if c.root != nil {
		owner = c.root
	}

	owner.mu.Lock()
	defer owner.mu.Unlock()

	return append([]string(nil), owner.configFiles...)
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
	subV := root.liveViper().Sub(fullPrefix)
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
// When set, hot-reload will validate a candidate config before swapping it in;
// a candidate that fails validation is rejected and the last-known-good config
// is retained.
func (c *Container) SetSchema(schema *Schema) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.schema = schema
}

// watchConfig starts the container-owned fsnotify watcher over every
// configured file. On any change event (coalesced behind the reload debounce
// window), the container rebuilds and validates a candidate config and, only
// on success, swaps it in and notifies observers. The watcher is a no-op for
// containers with no backing files (reader/embedded). Start the watcher only
// after construction has fully completed.
func (c *Container) watchConfig() {
	if len(c.configFiles) == 0 {
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		c.logger.Warn("config hot-reload disabled: failed to create watcher", "error", err)

		return
	}

	for _, f := range c.configFiles {
		if addErr := watcher.Add(f); addErr != nil {
			c.logger.Debug("config watch: failed to add file", "file", f, "error", addErr)
		}
	}

	done := make(chan struct{})

	c.mu.Lock()
	c.watcher = watcher
	c.watchDone = done
	c.mu.Unlock()

	// The loop captures the watcher and done channel as locals so it never
	// reads the (Close-mutated) struct fields without the lock.
	go c.watchLoop(watcher, done)
}

// watchLoop consumes fsnotify events, debounces a save burst, re-establishes
// the watch on each path (atomic-rename saves replace the inode), then reloads.
func (c *Container) watchLoop(watcher *fsnotify.Watcher, done <-chan struct{}) {
	debounce := c.reloadDebounce
	if debounce <= 0 {
		debounce = DefaultReloadDebounce
	}

	timer := time.NewTimer(debounce)
	if !timer.Stop() {
		<-timer.C
	}

	defer timer.Stop()

	pending := false

	for {
		select {
		case <-done:
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			c.handleWatchEvent(watcher, event, timer, debounce)

			pending = true
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}

			c.logger.Warn("config watch: error", "error", err)
		case <-timer.C:
			pending = c.fireReload(pending)
		}
	}
}

// fireReload performs a reload when one is pending, returning the new pending
// state (always false after a fired reload).
func (c *Container) fireReload(pending bool) bool {
	if pending {
		c.reload()
	}

	return false
}

// handleWatchEvent logs an fsnotify event, re-establishes the watch on the
// affected path (atomic-rename saves replace the inode), and arms the debounce
// timer so a burst of events coalesces into a single reload.
func (c *Container) handleWatchEvent(watcher *fsnotify.Watcher, event fsnotify.Event, timer *time.Timer, debounce time.Duration) {
	c.logger.Debug("config watch: event", "event", event.String())

	c.rewatch(watcher, event.Name)

	timer.Reset(debounce)
}

// rewatch re-adds a single path to the watcher, tolerating the brief window
// during an atomic rename where the path may not yet exist.
func (c *Container) rewatch(watcher *fsnotify.Watcher, path string) {
	if watcher == nil || path == "" {
		return
	}

	_ = watcher.Remove(path)

	if err := watcher.Add(path); err != nil {
		c.logger.Debug("config watch: failed to re-add file", "file", path, "error", err)
	}
}

// reload rebuilds a candidate viper from all configured files, validates it
// against the schema (if any), and on success swaps it in and notifies
// observers of the change. On any failure (parse, merge, or validation) the
// reload is rejected fail-closed: the last-known-good config is retained, the
// error is logged, and observers are not notified because no values changed.
func (c *Container) reload() {
	c.mu.Lock()
	schema := c.schema
	files := append([]string(nil), c.configFiles...)
	fs := c.fs
	c.mu.Unlock()

	candidate, err := c.buildCandidate(fs, files)
	if err != nil {
		// Fail-closed: a parse/merge failure in any file rejects the whole
		// reload. The last-known-good config is retained and observers are
		// not notified, because no values changed.
		c.logger.Error("config reload rejected: failed to build candidate", "error", err.Error())
		c.notifyReloadError(err)

		return
	}

	if schema != nil {
		result := validateViper(candidate, schema)
		if !result.Valid() {
			// Validation failure: keep last-known-good, do not swap, do not
			// notify (no change occurred).
			c.logger.Error("config reload rejected: validation failed", "errors", result.Error())
			c.notifyReloadError(errors.Newf("config reload rejected: validation failed: %s", result.Error()))

			return
		}
	}

	c.mu.Lock()
	c.viper = candidate
	c.mu.Unlock()

	c.logger.Info("config reloaded")
	c.notify()
}

// buildCandidate reads file[0] and merges files[1:] in order into a fresh
// viper, mirroring the initial load. The whole reload is fail-closed: if any
// file fails to parse, an error is returned and no swap occurs.
func (c *Container) buildCandidate(fs afero.Fs, files []string) (*viper.Viper, error) {
	if len(files) == 0 {
		return nil, errors.WithStack(ErrConfigFileNotFound)
	}

	c.mu.Lock()
	envPrefix := c.viper.GetEnvPrefix()
	c.mu.Unlock()

	v := newResolverViper(fs, envPrefix)

	exists, err := afero.Exists(fs, files[0])
	if err != nil {
		return nil, errors.Wrap(err, "failed to check config file existence")
	}

	if !exists {
		return nil, errors.WithStack(ErrConfigFileNotFound)
	}

	v.SetConfigFile(files[0])

	if err := v.ReadInConfig(); err != nil {
		return nil, errors.Newf("failed to read config file %s: %w", files[0], err)
	}

	for _, f := range files[1:] {
		exists, err := afero.Exists(fs, f)
		if err != nil || !exists {
			continue
		}

		v.SetConfigFile(f)

		if err := v.MergeInConfig(); err != nil {
			return nil, errors.Newf("failed to merge config file %s: %w", f, err)
		}
	}

	return v, nil
}

// notify copies the observer slice under lock and runs each observer outside
// the lock with the (already-swapped) live config. Observer errors are logged
// and never block subsequent observers or reloads.
func (c *Container) notify() {
	c.mu.Lock()
	observers := append([]Observable(nil), c.observers...)
	c.mu.Unlock()

	for _, o := range observers {
		if err := o.Run(c); err != nil {
			c.logger.Error("config observer returned an error", "error", err.Error())
		}
	}
}

// OnReloadError registers a callback invoked whenever a hot-reload is rejected:
// the candidate failed to build (fail-closed partial-merge, parse error, or a
// missing primary file) or failed schema validation, so the live config was NOT
// swapped and the last-known-good config is retained. The callback is never
// invoked for a successful reload (observers are notified of that change
// instead). Callbacks run in registration order on the watcher goroutine, after
// the container has logged the error; a slow callback delays subsequent reloads,
// so expensive work should be offloaded.
func (c *Container) OnReloadError(f func(error)) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.reloadErrFuncs = append(c.reloadErrFuncs, f)
}

// notifyReloadError copies the reload-error callback slice under lock and runs
// each callback outside the lock with the rejection error, mirroring notify()'s
// race-safe, deadlock-free discipline.
func (c *Container) notifyReloadError(err error) {
	c.mu.Lock()
	funcs := append([](func(error)){}, c.reloadErrFuncs...)
	c.mu.Unlock()

	for _, f := range funcs {
		f(err)
	}
}

// Close stops the hot-reload watcher and releases its resources. It is safe to
// call on containers that are not watching and safe to call more than once.
func (c *Container) Close() error {
	c.mu.Lock()
	watcher := c.watcher
	done := c.watchDone
	c.watcher = nil
	c.watchDone = nil
	c.mu.Unlock()

	if done != nil {
		close(done)
	}

	if watcher != nil {
		return watcher.Close()
	}

	return nil
}

// AddObserver attach observer to trigger on config update.
func (c *Container) AddObserver(o Observable) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.observers = append(c.observers, o)
}

// AddObserverFunc attach function to trigger on config update.
func (c *Container) AddObserverFunc(f func(Containable) error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.observers = append(c.observers, Observer{f})
}

// GetObservers retrieve all currently attached Observers.
func (c *Container) GetObservers() []Observable {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]Observable(nil), c.observers...)
}

// Dump return config as json string.
func (c *Container) ToJSON() string {
	s := c.liveViper().AllSettings()

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
