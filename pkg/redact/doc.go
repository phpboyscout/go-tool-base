// Package redact strips credential-like content from free-form strings
// at the boundary between trusted and untrusted observability surfaces
// (telemetry vendors, log aggregators, metric stores).
//
// # Threat model
//
// Error messages, command arguments, and HTTP header values routinely
// carry credentials by accident: an HTTP client wraps a URL with an
// embedded token, a command-line flag like `--api-key=sk-abc123` lands
// in `os.Args`, an error from a failed OTLP export quotes an
// Authorization header. The moment that content reaches a third-party
// ingest, it is outside the operator's control — potentially
// replicated, indexed, and retained longer than intended.
//
// The right defence is to redact at the boundary. This package
// applies pattern-based redaction in-process, before data is shipped.
// Callers do not need to remember to sanitise: the collector,
// middleware, and logger helpers route untrusted strings through
// [String] on their way out.
//
// # Discipline
//
// Use [String] or [Error] anywhere a caller-supplied or
// environment-derived string is written to telemetry, distributed
// logs, or any surface where credentials would be harmful. Do not use
// it on local process logs that never leave the host — those may need
// raw content for debugging.
//
// # Limitations
//
// Pattern catalogues never reach 100 % recall. The rules here catch
// the common shapes (URL userinfo, common query-parameter names,
// Authorization-header tokens, well-known provider prefixes like
// "sk-", "ghp_", "AIza", "AKIA", and a conservative ≥ 41-char opaque
// token fallback). A custom credential in a non-standard format will
// slip through; callers handling such inputs must supply their own
// redaction upstream.
//
// The fallback opaque-token pattern is intentionally conservative
// (≥ 41 chars) so it does not false-positive on UUIDs (36 with
// hyphens), MD5 (32) or SHA-1 (40) hashes. SHA-256 (64 chars) will
// match — accepted tradeoff; hashes rarely appear in error strings.
//
// Patterns are ASCII-only. Virtually all real-world provider tokens
// are ASCII; UTF-8 credentials in the wild are vanishingly rare.
//
// See docs/development/specs/2026-04-17-telemetry-redaction.md
// (M-5 and M-6 from the 2026-04-17 security audit) for the full
// threat model and design rationale.
package redact
