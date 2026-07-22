// Package forge provides a single, provider-parameterised interactive setup
// initialiser for git forges.
//
// One generic [Initialiser] is driven by a [Profile] describing a forge's
// credential shape. Two profiles are registered:
//
//   - GitHub ([SingleToken]): a token recorded via an env-var reference, the OS
//     keychain, or a literal in config; forge-driven OAuth login with a
//     manual-PAT fallback on headless hosts; and optional SSH key discovery,
//     generation, and upload. Registered with the `init github` command
//     ([NewCmdInitGitHub]) and an embedded asset bundle.
//   - Bitbucket ([DualUserPass]): the dual-credential model (username +
//     app_password) across the same three storage modes — env-var mode records
//     two env-var names, keychain mode stores a single JSON blob, literal mode
//     writes both fields to config. No login, no SSH. Registered with the
//     `init bitbucket` command ([NewCmdInitBitbucket]).
//
// Runtime credential resolution lives in pkg/vcs and the forge providers; this
// package handles first-run setup, not API client construction. All three
// storage modes honour the credential-storage hardening spec: literal mode is
// refused under CI, and every write commits its mode's keys exclusively so
// switching modes never leaves a stale secret or reference behind.
package forge
