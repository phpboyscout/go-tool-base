// Package credentials describes how a user-supplied secret (VCS
// token, AI-provider API key, Bitbucket dual-credential blob) is
// persisted by the interactive setup wizard and resolved by the
// runtime config chain.
//
// # Storage modes
//
// Three modes are supported, in descending order of preference:
//
//  1. [ModeEnvVar] — the config records the NAME of an environment
//     variable. The actual secret lives outside the config file,
//     typically in the user's shell profile or a CI platform's
//     secret-injection mechanism. This is the recommended default
//     and the only mode permitted under CI.
//  2. [ModeKeychain] — the config records a keychain reference
//     (`<service>/<account>`). The secret lives in the OS keychain
//     (macOS Keychain, Linux libsecret, Windows Credential Manager).
//     Available only when the binary is built with `-tags keychain`;
//     otherwise the reference surfaces
//     [ErrCredentialUnsupported] at resolution time.
//  3. [ModeLiteral] — the secret is written as plaintext in the
//     config file. Supported for backward compatibility and for
//     air-gapped or throwaway environments. Refused under CI.
//
// # Why a dedicated package
//
// The setup wizard, config masking, doctor checks, and migration
// tooling all reason about "which storage modes are available" and
// "how do we retrieve a credential stored in each mode". Consolidating
// those concerns here avoids scattering the same switch statement
// across eight call sites.
//
// # Build tag behaviour
//
// The default build compiles [keychain_stub.go] — [Store], [Retrieve],
// and [Delete] all return [ErrCredentialUnsupported], and
// [KeychainAvailable] reports false. A binary built with
// `-tags keychain` compiles [keychain_enabled.go] (Phase 2) instead
// and links against `github.com/zalando/go-keyring`, enabling real
// OS keychain I/O.
//
// See docs/development/specs/2026-04-02-credential-storage-hardening.md
// for the full threat model, trust-model guidance, and test matrix.
package credentials
