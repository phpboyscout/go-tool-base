package generate

import "errors"

var (
	ErrCommandNameRequired  = errors.New("command name is required")
	ErrFlagNameRequired     = errors.New("flag name is required")
	ErrNameRequired         = errors.New("name is required")
	ErrRepositoryRequired      = errors.New("repository is required")
	ErrRepositoryInvalidFormat = errors.New("repository must contain at least one '/' (e.g. org/repo)")
	ErrHostRequired         = errors.New("host is required")
	ErrEmptyCommandPath     = errors.New("empty command path")
	ErrCommandNotFound      = errors.New("command not found in manifest")
	ErrUpdateManifestFailed = errors.New("failed to update manifest")
	ErrNonInteractive       = errors.New("non-interactive mode detected, missing required flags")
)
