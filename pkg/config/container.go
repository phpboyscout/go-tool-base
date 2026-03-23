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

	"github.com/phpboyscout/gtb/pkg/logger"
)

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
}

// Container container for configuration.
type Container struct {
	ID        string
	viper     *viper.Viper
	logger    logger.Logger
	observers []Observable
}

// Get interface value from config.
func (c *Container) Get(key string) any {
	return c.viper.Get(key)
}

// GetBool get Bool value from config.
func (c *Container) GetBool(key string) bool {
	return c.viper.GetBool(key)
}

// GetInt get Bool value from config.
func (c *Container) GetInt(key string) int {
	return c.viper.GetInt(key)
}

// GetFloat get Float value from config.
func (c *Container) GetFloat(key string) float64 {
	return c.viper.GetFloat64(key)
}

// GetString get string value from config.
func (c *Container) GetString(key string) string {
	return c.viper.GetString(key)
}

// GetTime get time value from config.
func (c *Container) GetTime(key string) time.Time {
	return c.viper.GetTime(key)
}

// GetDuration get duration value from config.
func (c *Container) GetDuration(key string) time.Duration {
	return c.viper.GetDuration(key)
}

// GetViper retrieves the underlying Viper configuration.
func (c *Container) GetViper() *viper.Viper {
	return c.viper
}

// Has retrieves the underlying Viper configuration.
func (c *Container) Has(key string) bool {
	return c.viper.InConfig(key)
}

// IsSet checks if the key has been set.
func (c *Container) IsSet(key string) bool {
	return c.viper.IsSet(key)
}

// Set sets the value for the given key.
func (c *Container) Set(key string, value any) {
	c.viper.Set(key, value)
}

// WriteConfigAs writes the current configuration to the given path.
func (c *Container) WriteConfigAs(dest string) error {
	return c.viper.WriteConfigAs(dest)
}

// Sub returns a subtree of the parent configuration.
func (c *Container) Sub(key string) Containable {
	subV := c.viper.Sub(key)
	if subV == nil {
		return nil
	}

	return &Container{
		ID:        fmt.Sprintf("%s#%s", c.ID, key),
		viper:     subV,
		logger:    c.logger,
		observers: make([]Observable, 0),
	}
}

func (c *Container) handleReadFileError(err error) {
	// just use the default value(s) if the config file was not found.
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		c.logger.Warn("could not load config file. Using default values", "stacktrace", fmt.Sprintf("%+v", err))
	} else if err != nil { // Handle other errors that occurred while reading the config file
		c.logger.Warn(fmt.Sprintf("Could not read the config file (%s)", err), "stacktrace", fmt.Sprintf("%+v", err))
	}
}

// watchConfig monitor the changes in the config file.
func (c *Container) watchConfig() {
	c.viper.OnConfigChange(func(e fsnotify.Event) {
		c.logger.Info(fmt.Sprintf("Config updated %v", e))

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
		c.logger.Error("unable to marshal config to JSON", "stacktrace", fmt.Sprintf("%+v", err))
	}

	return string(bs)
}

func (c *Container) Dump(w io.Writer) {
	_, _ = fmt.Fprintln(w, c.ToJSON())
}
