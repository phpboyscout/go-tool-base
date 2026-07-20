package props

import (
	"log/slog"

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

// ViewOrNil pins a view of p's store, or returns a true-nil Reader when p or
// its store is absent. The nil handling is the point: returning a typed
// (*config.View)(nil) would box a nil pointer into a non-nil interface and
// silently defeat the cfg == nil guards the read adapters rely on.
func ViewOrNil(p *Props) config.Reader {
	if p == nil || p.Config == nil {
		return nil
	}

	return p.Config.View()
}

// ConfigFileSources returns the paths of the loaded file layers in snapshot
// precedence order, deduplicating multi-document entries. The last element is
// the highest-precedence loaded file — the same answer Viper's
// ConfigFileUsed() gave (it reported the last file it loaded), which several
// consumers (doctor's report, telemetry's data dir, the config command's
// writable-target resolution) depend on for parity.
func ConfigFileSources(snap *config.Snapshot) []string {
	var files []string

	seen := map[string]bool{}

	for _, layer := range snap.Layers() {
		if layer.Source.Kind == config.SourceFile && !seen[layer.Source.Name] {
			seen[layer.Source.Name] = true

			files = append(files, layer.Source.Name)
		}
	}

	return files
}

// SlogLogger adapts p's logger to a *slog.Logger, falling back to a discarding
// logger when p is nil. The config adapters in pkg/chat and pkg/telemetry both
// need a slog handle from possibly-nil Props; hosting the conversion here keeps
// them from carrying identical copies (compare ViewOrNil).
func SlogLogger(p *Props) *slog.Logger {
	if p != nil {
		return logger.ToSlog(p.Logger)
	}

	return slog.New(slog.DiscardHandler)
}

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
