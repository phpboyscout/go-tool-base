package props

import (
	"slices"
	"time"

	"gitlab.com/phpboyscout/go-tool-base/pkg/errorhandling"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

// FeatureCmd identifies a built-in feature that can be enabled or disabled.
type FeatureCmd string

const (
	UpdateCmd    = FeatureCmd("update")
	InitCmd      = FeatureCmd("init")
	McpCmd       = FeatureCmd("mcp")
	DocsCmd      = FeatureCmd("docs")
	AiCmd        = FeatureCmd("ai")
	DoctorCmd    = FeatureCmd("doctor")
	ConfigCmd    = FeatureCmd("config")
	ChangelogCmd = FeatureCmd("changelog")
	ManCmd       = FeatureCmd("man")
)

// DefaultFeatures is the list of features enabled by default.
var DefaultFeatures = []FeatureState{
	Enable(UpdateCmd),
	Enable(InitCmd),
	Enable(McpCmd),
	Enable(DocsCmd),
	Enable(DoctorCmd),
	Enable(ChangelogCmd),
}

// Feature represents the state of a feature (Enabled/Disabled).
type Feature struct {
	Cmd     FeatureCmd `json:"cmd" yaml:"cmd"`
	Enabled bool       `json:"enabled" yaml:"enabled"`
}

// FeatureState is a functional option that mutates the list of features.
type FeatureState func([]Feature) []Feature

// SetFeatures applies a series of mutators to the standard default set.
func SetFeatures(mutators ...FeatureState) []Feature {
	var features []Feature

	// Apply defaults first
	for _, fn := range DefaultFeatures {
		features = fn(features)
	}

	// Apply user overrides
	for _, fn := range mutators {
		features = fn(features)
	}

	return features
}

// Enable returns a FeatureState that enables the given command.
func Enable(cmd FeatureCmd) FeatureState {
	return func(features []Feature) []Feature {
		// Remove existing entry if present to avoid duplicates
		for i, f := range features {
			if f.Cmd == cmd {
				features = slices.Delete(features, i, i+1)

				break
			}
		}

		return append(features, Feature{Cmd: cmd, Enabled: true})
	}
}

// Disable returns a FeatureState that disables the given command.
func Disable(cmd FeatureCmd) FeatureState {
	return func(features []Feature) []Feature {
		// Remove existing entry if present to avoid duplicates
		for i, f := range features {
			if f.Cmd == cmd {
				features = slices.Delete(features, i, i+1)

				break
			}
		}

		return append(features, Feature{Cmd: cmd, Enabled: false})
	}
}

// ReleaseSource identifies the platform and repository where the tool's
// releases are published. Used by the self-update system to check for and
// download new versions. Supported types: "github", "gitlab", "bitbucket",
// "gitea", "codeberg", "direct".
type ReleaseSource struct {
	Type    string `json:"type"    yaml:"type"`
	Host    string `json:"host"    yaml:"host"`
	Owner   string `json:"owner"   yaml:"owner"`
	Repo    string `json:"repo"    yaml:"repo"`
	Private bool   `json:"private" yaml:"private"`
	// Params holds provider-specific configuration key/value pairs.
	// Keys use snake_case. Valid keys are documented per provider.
	Params map[string]string `json:"params,omitempty" yaml:"params,omitempty"`
}

// SigningConfig holds tool-author configuration for self-update
// signature verification (Phase 2 of the remote-update-checksum spec).
// The zero value is safe: with no embedded keys and no external key
// email configured, signature verification is governed entirely by
// setup.DefaultRequireSignature (default false) and stays dormant.
type SigningConfig struct {
	// EmbeddedKeys are ASCII-armored public keys baked into the binary,
	// typically via //go:embed in an internal trustkeys package. They are
	// the embedded trust anchor the SelfUpdater uses to verify the
	// signature over checksums.txt. Not serialised — keys are compile-time
	// build artefacts, never runtime config.
	EmbeddedKeys [][]byte `json:"-" yaml:"-"`
}

// Tool holds metadata about the CLI tool: its identity, feature flags,
// release source for self-updates, help channel configuration, and
// telemetry settings. It is embedded in Props and passed to all commands.
type Tool struct {
	Name        string                   `json:"name" yaml:"name"`
	Summary     string                   `json:"summary" yaml:"summary"`
	Description string                   `json:"description" yaml:"description"`
	Features    []Feature                `json:"features" yaml:"features"`
	Help        errorhandling.HelpConfig `json:"-" yaml:"-"`

	// ReleaseSource is the source of truth for the tool's releases (GitHub or GitLab)
	ReleaseSource ReleaseSource `json:"release_source" yaml:"release_source"`

	// ReleaseProvider, when non-nil, is the release backend the self-update
	// subsystem uses, taking precedence over ReleaseSource.Type registry
	// lookup. It is a runtime-only dependency-injection seam — for tests
	// (see pkg/vcs/release/releasetest) and custom/embedded providers — and is
	// never serialised. Production tools leave it nil and are resolved from the
	// release registry by ReleaseSource.Type as before. An explicit
	// setup.WithReleaseProvider option takes precedence over this field.
	ReleaseProvider release.Provider `json:"-" yaml:"-"`

	// UpdatePolicy is the tool author's baseline self-update posture
	// (disabled / prompt / enabled). The zero value normalises to
	// UpdatePolicyDisabled — the framework default — so a tool does not
	// force updates out of the box. Overridable at runtime by the
	// `update.policy` config key. See ResolveUpdatePolicy.
	UpdatePolicy UpdatePolicy `json:"update_policy,omitempty" yaml:"update_policy,omitempty"`

	// UpdateCheckInterval is the tool author's baseline throttle between
	// self-update checks. The zero value means "use the framework default"
	// (setup.DefaultCheckInterval, 24h) — it is NOT interpreted as
	// "check every run". Overridable at runtime by the `update.check_interval`
	// config key (where "0"/"0s" does mean every run). See
	// setup.ResolveCheckInterval.
	UpdateCheckInterval time.Duration `json:"update_check_interval,omitempty" yaml:"update_check_interval,omitempty"`

	// Telemetry holds tool-author telemetry configuration.
	// Zero-value is safe — tools that don't set it are unaffected.
	Telemetry TelemetryConfig `json:"telemetry" yaml:"telemetry"`

	// Signing holds tool-author self-update signature configuration.
	// Zero-value is safe — verification stays dormant until keys and/or
	// require_signature are configured.
	Signing SigningConfig `json:"-" yaml:"-"`

	// EnvPrefix is the environment variable prefix used by the config package.
	// When set, only env vars starting with this prefix (e.g., "GTB_") are
	// considered for config overrides. Empty means no prefix (all env vars match).
	EnvPrefix string `json:"env_prefix,omitempty" yaml:"env_prefix,omitempty"`

	// InstallHint is shown when a feature requires a full release binary
	// (e.g. embedded docs) that the running binary lacks. Set it to your
	// tool's recommended installation command. When empty, a generic
	// message referencing Name is shown — the framework never assumes a
	// particular installer.
	InstallHint string `json:"install_hint,omitempty" yaml:"install_hint,omitempty"`
}

// isDefaultEnabled returns true if the feature is enabled by default.
func isDefaultEnabled(cmd FeatureCmd) bool {
	switch cmd {
	case UpdateCmd, InitCmd, McpCmd, DocsCmd, DoctorCmd, ChangelogCmd:
		return true
	case AiCmd, ConfigCmd, TelemetryCmd, ManCmd:
		return false
	default:
		return false
	}
}

// IsEnabled checks if a feature is enabled.
// It checks the Features slice first, falling back to built-in defaults.
func (t Tool) IsEnabled(cmd FeatureCmd) bool {
	for _, f := range t.Features {
		if f.Cmd == cmd {
			return f.Enabled
		}
	}

	return isDefaultEnabled(cmd)
}

// IsDisabled checks if a feature is disabled.
func (t Tool) IsDisabled(cmd FeatureCmd) bool {
	return !t.IsEnabled(cmd)
}

// GetReleaseSource returns the release source type, owner, and repo.
func (t Tool) GetReleaseSource() (sourceType, owner, repo string) {
	return t.ReleaseSource.Type, t.ReleaseSource.Owner, t.ReleaseSource.Repo
}
