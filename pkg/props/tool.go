package props

import (
	"slices"

	"github.com/phpboyscout/gtb/pkg/errorhandling"
)

type FeatureCmd string

const (
	UpdateCmd = FeatureCmd("update")
	InitCmd   = FeatureCmd("init")
	McpCmd    = FeatureCmd("mcp")
	DocsCmd   = FeatureCmd("docs")
	AiCmd     = FeatureCmd("ai")
)

// DefaultFeatures is the list of features enabled by default.
var DefaultFeatures = []FeatureState{
	Enable(UpdateCmd),
	Enable(InitCmd),
	Enable(McpCmd),
	Enable(DocsCmd),
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

type ReleaseSource struct {
	Type    string `json:"type" yaml:"type"`
	Host    string `json:"host" yaml:"host"`
	Owner   string `json:"owner" yaml:"owner"`
	Repo    string `json:"repo" yaml:"repo"`
	Private bool   `json:"private" yaml:"private"`
}

type Tool struct {
	Name        string                   `json:"name" yaml:"name"`
	Summary     string                   `json:"summary" yaml:"summary"`
	Description string                   `json:"description" yaml:"description"`
	Features    []Feature                `json:"features" yaml:"features"`
	Help        errorhandling.HelpConfig `json:"-" yaml:"-"`

	// ReleaseSource is the source of truth for the tool's releases (GitHub or GitLab)
	ReleaseSource ReleaseSource `json:"release_source" yaml:"release_source"`

	// Deprecated: Use Features instead
	Disable []FeatureCmd `json:"disable" yaml:"disable"`
	// Deprecated: Use Features instead
	Enable []FeatureCmd `json:"enable" yaml:"enable"`
}

// isDefaultEnabled returns true if the feature is enabled by default.
func isDefaultEnabled(cmd FeatureCmd) bool {
	switch cmd {
	case UpdateCmd, InitCmd, McpCmd, DocsCmd:
		return true
	case AiCmd:
		return false
	default:
		return false
	}
}

// IsEnabled checks if a feature is enabled.
// It prioritizes the new Features slice, falling back to Disable/Enable slices for backward compatibility,
// and finally falling back to the built-in defaults.
func (t Tool) IsEnabled(cmd FeatureCmd) bool {
	// 1. Check Features slice (the new source of truth)
	for _, f := range t.Features {
		if f.Cmd == cmd {
			return f.Enabled
		}
	}

	// 2. Fallback: Check explicit Enable list (if populated)
	if slices.Contains(t.Enable, cmd) {
		return true
	}

	// 3. Fallback: Check explicit Disable list
	if slices.Contains(t.Disable, cmd) {
		return false
	}

	// 4. Default behavior: fallback to built-in defaults
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
