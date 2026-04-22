// Package bitbucket implements the interactive setup wizard for
// Bitbucket Cloud authentication.
//
// Bitbucket uses a dual-credential model (username + app password),
// so the wizard collects or references both values in a single flow.
// Three storage modes are supported, matching the credential-storage
// hardening spec:
//
//   - Env-var reference (recommended default): two config entries,
//     bitbucket.username.env and bitbucket.app_password.env, each
//     pointing at an environment variable name. Neither value hits
//     the config file. Refused under CI only for literal mode —
//     env-var references are the permitted CI path.
//   - OS keychain: a single JSON blob {"username": ..., "app_password":
//     ...} stored under <toolname>/bitbucket.auth. The config records
//     only bitbucket.keychain: "<toolname>/bitbucket.auth".
//   - Literal: bitbucket.username and bitbucket.app_password written
//     directly. Plaintext on disk; refused under CI.
//
// Resolver side (pkg/vcs/bitbucket) walks the full per-field chain at
// runtime. Corrupt or incomplete keychain blobs abort resolution
// rather than falling through to stale literals (R3).
package bitbucket
