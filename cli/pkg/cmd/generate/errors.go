package generate

import "gitlab.com/phpboyscout/go/errors"

var (
	ErrCommandNameRequired      = errors.NewSentinel("gtb.generate.command_name_required", "command name is required")
	ErrFlagNameRequired         = errors.NewSentinel("gtb.generate.flag_name_required", "flag name is required")
	ErrNameRequired             = errors.NewSentinel("gtb.generate.name_required", "name is required")
	ErrRepositoryRequired       = errors.NewSentinel("gtb.generate.repository_required", "repository is required")
	ErrEmptyCommandPath         = errors.NewSentinel("gtb.generate.empty_command_path", "empty command path")
	ErrCommandNotFound          = errors.NewSentinel("gtb.generate.command_not_found", "command not found in manifest")
	ErrUpdateManifestFailed     = errors.NewSentinel("gtb.generate.update_manifest_failed", "failed to update manifest")
	ErrNonInteractive           = errors.NewSentinel("gtb.generate.non_interactive", "non-interactive mode detected, missing required flags")
	ErrInvalidOverwriteValue    = errors.NewSentinel("gtb.generate.invalid_overwrite_value", "invalid --overwrite value: must be allow, deny, or ask")
	ErrInvalidSigningKeySource  = errors.NewSentinel("gtb.generate.invalid_signing_key_source", "invalid --signing-key-source: must be embedded, external, or both")
	ErrInvalidSigningBackend    = errors.NewSentinel("gtb.generate.invalid_signing_backend", "invalid --signing-backend: not a registered signing backend")
	ErrGitFlagsConflict         = errors.NewSentinel("gtb.generate.git_flags_conflict", "conflicting flags: --no-git skips the initial commit that --push would publish")
	ErrModuleRequired           = errors.NewSentinel("gtb.generate.module_required", "a project that is not hosted on a forge needs --module")
	ErrReleaseChannelRequired   = errors.NewSentinel("gtb.generate.release_channel_required", "self-update needs a release channel: a forge backend, or --release-channel direct with --release-url-template and --release-version-url; or drop update from --features")
	ErrDirectSourceIncomplete   = errors.NewSentinel("gtb.generate.direct_source_incomplete", "--release-channel direct needs --release-url-template and --release-version-url")
	ErrHelpChannelRequired      = errors.NewSentinel("gtb.generate.help_channel_required", "a help channel type needs its channel: --slack-channel for slack, --teams-channel for teams")
	ErrSigningKeyWithoutSigning = errors.NewSentinel("gtb.generate.signing_key_without_signing", "--signing-key-id wires the release pipeline's signs block, which needs --signing (or --signing-email)")
)
