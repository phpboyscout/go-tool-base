package root

import (
	"strings"

	"github.com/spf13/pflag"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// RootOption configures the root command constructed by
// [NewCmdRootWithOptions]. Options are the extensible way to register bound
// flags and subcommands without breaking the existing constructor signatures.
type RootOption func(*rootOptions)

// rootOptions accumulates configuration applied by [RootOption]s. The
// boundFlags map is config-key → flag and feeds the store's flags layer at
// construction; only flags the user explicitly changed contribute, so
// defaults never clobber configured values.
type rootOptions struct {
	configPaths []string
	subcommands []*setup.Command
	// boundFlags maps a config key to the persistent/root flag that should
	// override it. Applied once at the root during config load.
	boundFlags map[string]*pflag.Flag
}

func newRootOptions() *rootOptions {
	return &rootOptions{
		boundFlags: map[string]*pflag.Flag{},
	}
}

func applyRootOptions(opts []RootOption) *rootOptions {
	o := newRootOptions()

	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}

	return o
}

// WithConfigPaths registers additional configuration file paths to consider
// during initialisation. Equivalent to the configPaths argument of
// [NewCmdRootWithConfig].
func WithConfigPaths(paths ...string) RootOption {
	return func(o *rootOptions) {
		o.configPaths = append(o.configPaths, paths...)
	}
}

// WithSubcommands registers subcommands on the root command.
func WithSubcommands(subcommands ...*setup.Command) RootOption {
	return func(o *rootOptions) {
		o.subcommands = append(o.subcommands, subcommands...)
	}
}

// WithBoundFlags binds the given persistent/root pflags to configuration keys.
//
// The map key is the dotted configuration key (e.g. "server.port") and the
// value is the pflag to bind. Bound flags participate in the documented
// configuration precedence (flags > env > file > embedded > defaults): a flag
// the user explicitly set on the command line overrides the corresponding
// config value. A flag left at its default does not override config — binding
// is filtered by flag.Changed during config load.
//
// Example:
//
//	root.NewCmdRootWithOptions(props, root.WithBoundFlags(map[string]*pflag.Flag{
//	    "server.port": rootCmd.PersistentFlags().Lookup("server-port"),
//	}))
func WithBoundFlags(flags map[string]*pflag.Flag) RootOption {
	return func(o *rootOptions) {
		for key, flag := range flags {
			if flag == nil {
				continue
			}

			o.boundFlags[key] = flag
		}
	}
}

// WithConventionBoundFlags binds every flag in the given flag set to a config
// key derived from its name by the hyphen-to-dot convention: "--server-port"
// becomes the config key "server.port". This is the zero-boilerplate
// alternative to [WithBoundFlags] when flag names already mirror config keys.
//
// As with [WithBoundFlags], only flags the user explicitly changed override
// config (filtered by flag.Changed during load). Flags already containing a dot
// are mapped verbatim; authors should avoid dots in flag names to keep the
// mapping unambiguous.
func WithConventionBoundFlags(flags *pflag.FlagSet) RootOption {
	return func(o *rootOptions) {
		if flags == nil {
			return
		}

		flags.VisitAll(func(f *pflag.Flag) {
			o.boundFlags[ConventionKey(f.Name)] = f
		})
	}
}

// ConventionKey maps a flag name to a configuration key using the hyphen-to-dot
// convention: "server-port" → "server.port". It is exported so downstream tools
// can derive the same keys when constructing explicit [WithBoundFlags] maps.
func ConventionKey(flagName string) string {
	return strings.ReplaceAll(flagName, "-", ".")
}
