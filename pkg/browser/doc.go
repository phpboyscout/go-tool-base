// Package browser provides a safe entry point for opening URLs in the
// user's default browser or mail client.
//
// The design goal is to constrain every URL-opening code path in the
// codebase through a single validation point so that:
//
//   - Only scheme://url of types https, http, and mailto are passed to the
//     OS. Dangerous schemes such as file://, javascript:, data:, and custom
//     protocol handlers are rejected.
//   - Oversize URLs (above 8 KiB, chosen to sit below all supported
//     platforms' command-line length limits) are rejected.
//   - URLs containing ASCII control characters or NUL bytes are rejected
//     before they reach any platform URL handler.
//
// Callers must never invoke the underlying OS URL opener (such as
// github.com/cli/browser or exec.Command("open"|"xdg-open"|"rundll32"))
// directly. All URL-opening in GTB routes through [OpenURL].
//
// # mailto: and caller responsibility
//
// [OpenURL] validates only the scheme and the URL's overall shape. It
// cannot detect header-injection attacks in mailto: URLs such as an
// attacker-supplied cc=, bcc=, or body= parameter. Callers constructing
// mailto: URLs from user-influenced data must escape every parameter using
// url.QueryEscape. See the EmailDeletionRequestor in pkg/telemetry for an
// example.
//
// # Context cancellation
//
// The underlying URL opener is not context-aware; once the OS spawns a
// browser or mail client process, [OpenURL] cannot cancel it. Callers
// pass a context so that pre-open cancellation (e.g. the enclosing command
// was cancelled before the opener ran) is respected.
//
// This package closes M-5 from the 2026-04-02 security audit. See
// docs/development/specs/2026-04-02-url-scheme-validation.md for the full
// threat model and design rationale.
package browser
