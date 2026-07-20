package props

import (
	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/config"
	configafero "gitlab.com/phpboyscout/go/config-afero"

	"gitlab.com/phpboyscout/go/errorhandling"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// Props is the primary dependency injection container for GTB applications.
// When writing functions that accept Props, consider whether a narrow interface
// (LoggerProvider, ConfigProvider, etc.) would suffice.
type Props struct {
	Tool   Tool
	Logger logger.Logger
	// Config is the live configuration store. It owns reloads, so it is the
	// thing to hold; a *config.View taken from it is pinned to one snapshot and
	// goes stale the moment the file changes.
	//
	// Read through Config.View(), which is the read surface. Where several
	// values must agree with each other, use Config.With so they all resolve
	// against the same snapshot rather than straddling a reload.
	Config       *config.Store
	Assets       Assets
	FS           afero.Fs
	Version      version.Version
	ErrorHandler errorhandling.ErrorHandler
	// Collector is always non-nil once the root command tree is built: the
	// bootstrap defaults it to a NoopCollector and later replaces it with the
	// resolved *telemetry.Collector. Consumers may call it unconditionally.
	Collector TelemetryCollector
}

// GetLogger returns the application logger.
func (p *Props) GetLogger() logger.Logger { return p.Logger }

// GetConfig returns the live configuration store.
func (p *Props) GetConfig() *config.Store { return p.Config }

// GetConfigView returns a read surface pinned to the current snapshot.
//
// Named GetConfigView rather than Config because Props already has a Config
// field and Go forbids a method sharing its name.
//
// Take one per logical operation and let it go — holding one across a reload
// silently serves stale values. Reading several related keys from one view is
// the point: they are guaranteed to come from the same snapshot.
func (p *Props) GetConfigView() *config.View { return p.Config.View() }

// GetConfigFS adapts the application filesystem for the config store.
//
// Props.FS stays a full afero.Fs — OsFs live, MemMapFs under test — because
// that is how the rest of GTB operates. config defines its own narrower
// interface rather than depending on afero, so the two are bridged here in one
// place instead of at every construction site.
func (p *Props) GetConfigFS() config.FS { return configafero.Wrap(p.FS) }

// GetAssets returns the embedded assets.
func (p *Props) GetAssets() Assets { return p.Assets }

// GetFS returns the application filesystem.
func (p *Props) GetFS() afero.Fs { return p.FS }

// GetVersion returns the version information.
func (p *Props) GetVersion() version.Version { return p.Version }

// GetErrorHandler returns the error handler.
func (p *Props) GetErrorHandler() errorhandling.ErrorHandler { return p.ErrorHandler }

// GetTool returns the tool metadata.
func (p *Props) GetTool() Tool { return p.Tool }

// GetCollector returns the telemetry collector. It is always non-nil once the
// root command tree is built, so consumers may call it unconditionally.
func (p *Props) GetCollector() TelemetryCollector { return p.Collector }
