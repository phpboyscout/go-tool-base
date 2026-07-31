package props

import "strings"

// UpdatePolicy governs how the self-update check behaves when a newer release
// is found. It is the tool author's baseline (the [Tool.UpdatePolicy] field),
// overridable at runtime by the `update.policy` config key. See
// https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0087-forced-update-feature.
type UpdatePolicy string

const (
	// UpdatePolicyDisabled never auto-updates or prompts: it only logs that an
	// update is available (and a persistent out-of-date WARN). This is the
	// framework default (the zero value normalises to it), so a tool built on
	// GTB does nothing surprising out of the box.
	UpdatePolicyDisabled UpdatePolicy = "disabled"

	// UpdatePolicyPrompt asks the user to update when one is available; they may
	// decline and continue with their command. The GTB CLI itself uses this.
	UpdatePolicyPrompt UpdatePolicy = "prompt"

	// UpdatePolicyEnabled blocks every command until the tool is updated when an
	// update is available (the historical forced-update behaviour, now explicit
	// and opt-in).
	UpdatePolicyEnabled UpdatePolicy = "enabled"
)

// Normalize trims, lowercases, and maps the empty policy (the zero value /
// framework default) to [UpdatePolicyDisabled]. An unknown non-empty value is
// returned trimmed/lowercased so [UpdatePolicy.Valid] can reject it.
func (p UpdatePolicy) Normalize() UpdatePolicy {
	n := UpdatePolicy(strings.ToLower(strings.TrimSpace(string(p))))
	if n == "" {
		return UpdatePolicyDisabled
	}

	return n
}

// Valid reports whether p is one of the three defined policies. The empty
// string is NOT valid (it is a sentinel for "unset"); callers wanting the
// default-applied value use [UpdatePolicy.Normalize].
func (p UpdatePolicy) Valid() bool {
	switch p {
	case UpdatePolicyDisabled, UpdatePolicyPrompt, UpdatePolicyEnabled:
		return true
	default:
		return false
	}
}

// ResolveUpdatePolicy computes the effective update policy. A valid
// `update.policy` config value wins over the tool's baseline; an empty or
// invalid config value falls back to the baseline; an empty or invalid
// baseline falls back to the framework default ([UpdatePolicyDisabled]).
// Resolution is case-insensitive. `--ci` bypass is handled by the caller and
// is out of scope here.
func ResolveUpdatePolicy(toolDefault UpdatePolicy, configValue string) UpdatePolicy {
	if cfg := UpdatePolicy(configValue).Normalize(); cfg.Valid() && strings.TrimSpace(configValue) != "" {
		return cfg
	}

	if base := toolDefault.Normalize(); base.Valid() {
		return base
	}

	return UpdatePolicyDisabled
}
