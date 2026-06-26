// Package github provides the interactive GitHub setup initialiser.
//
// It registers a setup [Initialiser] (and the `init github` command via
// [NewCmdInitGitHub]) that configures GitHub authentication — a token recorded via
// an env-var reference, the OS keychain, or a literal in config — and optionally
// discovers, generates, and uploads an SSH key. Runtime token resolution lives in
// pkg/vcs; this package handles first-run setup, not API client construction.
package github
