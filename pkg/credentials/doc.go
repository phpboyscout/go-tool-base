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
//     (macOS Keychain, Linux Secret Service via godbus, Windows
//     Credential Manager). Available only when the process has a
//     keychain-capable [Backend] registered — typically by blank-
//     importing the optional
//     github.com/phpboyscout/go-tool-base/pkg/credentials/keychain
//     subpackage. Without such a registration, [Store] / [Retrieve] /
//     [Delete] return [ErrCredentialUnsupported] and resolvers fall
//     through.
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
// # Separation of the keychain backend
//
// The go-keyring-backed implementation lives in a dedicated
// subpackage (github.com/phpboyscout/go-tool-base/pkg/credentials/
// keychain) that registers itself via [RegisterBackend] during its
// init(). Downstream tools that want OS keychain support blank-
// import that subpackage from their cmd/main; regulated or
// compliance-constrained downstreams omit the import and run with
// the stub [Backend], so linker dead-code elimination keeps go-
// keyring, godbus, and wincred out of their binary even though the
// packages exist in the module. Binary-level SBOM review (syft,
// cyclonedx-gomod on the linked artefact) shows the opt-out surface
// unambiguously.
//
// See docs/development/specs/2026-04-02-credential-storage-hardening.md
// for the full threat model, trust-model guidance, and test matrix.
package credentials
