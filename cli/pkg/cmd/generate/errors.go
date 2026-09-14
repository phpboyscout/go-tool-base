package generate

import "gitlab.com/phpboyscout/go/errors"

var (
	ErrCommandNameRequired     = errors.NewSentinel("gtb.generate.command_name_required", "command name is required")
	ErrFlagNameRequired        = errors.NewSentinel("gtb.generate.flag_name_required", "flag name is required")
	ErrNameRequired            = errors.NewSentinel("gtb.generate.name_required", "name is required")
	ErrRepositoryRequired      = errors.NewSentinel("gtb.generate.repository_required", "repository is required")
	ErrRepositoryInvalidFormat = errors.NewSentinel("gtb.generate.repository_invalid_format", "repository must contain at least one '/' (e.g. org/repo)")
	ErrHostRequired            = errors.NewSentinel("gtb.generate.host_required", "host is required")
	ErrEmptyCommandPath        = errors.NewSentinel("gtb.generate.empty_command_path", "empty command path")
	ErrCommandNotFound         = errors.NewSentinel("gtb.generate.command_not_found", "command not found in manifest")
	ErrUpdateManifestFailed    = errors.NewSentinel("gtb.generate.update_manifest_failed", "failed to update manifest")
	ErrNonInteractive          = errors.NewSentinel("gtb.generate.non_interactive", "non-interactive mode detected, missing required flags")
	ErrInvalidOverwriteValue   = errors.NewSentinel("gtb.generate.invalid_overwrite_value", "invalid --overwrite value: must be allow, deny, or ask")
	ErrEnvPrefixInvalid        = errors.NewSentinel("gtb.generate.env_prefix_invalid", "env prefix must contain only uppercase letters, digits, and underscores (e.g. MY_APP)")
	ErrInvalidSigningKeySource = errors.NewSentinel("gtb.generate.invalid_signing_key_source", "invalid --signing-key-source: must be embedded, external, or both")
	ErrInvalidSigningBackend   = errors.NewSentinel("gtb.generate.invalid_signing_backend", "invalid --signing-backend: not a registered signing backend")
	ErrGitFlagsConflict        = errors.NewSentinel("gtb.generate.git_flags_conflict", "conflicting flags: --no-git skips the initial commit that --push would publish")
)
