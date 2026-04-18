package generator

// Context-aware escape helpers for skeleton template rendering.
//
// The input-validation layer in validate.go is the primary defence
// against the template injection class catalogued in
// docs/development/specs/2026-04-02-generator-template-escaping.md.
// These helpers provide defence-in-depth at the rendering boundary:
// if a future change widens a validator, or an adversarial value
// reaches template rendering through a new input path, the escape
// pipes keep the output syntactically valid in YAML, TOML, Markdown,
// and shell-comment contexts.
//
// All escape functions are pure and infallible:
//
//   - Identity on the "safe" character class — any input made of
//     [a-zA-Z0-9 _.,/-] returns unchanged. Clean-input projects see
//     no diff after piping values through these helpers.
//   - Idempotent (where applicable) — escape(escape(s)) == escape(s)
//     for escapeComment and escapeMarkdownCodeBlock. Quote-wrapping
//     helpers (YAML, TOML, shell) satisfy parse-round-trip
//     idempotence instead: parsing the output yields the input.
//   - Never panic. Invalid bytes (e.g. invalid UTF-8) are replaced
//     with U+FFFD rather than returned verbatim.

import (
	"fmt"
	"regexp"
	"strings"
	"text/template"
	"unicode/utf8"
)

const (
	// nulRune is the NUL code point. Named so the switch-case
	// comparisons in escapeYAML / escapeTOML / escapeMarkdown read
	// naturally without tripping the magic-number linter.
	nulRune = 0x00
	// asciiHighest is the highest ASCII code point treated as
	// control territory by the escapers. 0x1F is the last C0
	// control; 0x7F is DEL, handled separately alongside this bound.
	asciiControlBound = 0x20
	delRune           = 0x7F
	// yamlWrapOverhead accounts for the opening and closing `"`
	// characters written by escapeYAML.
	yamlWrapOverhead = 2
)

// fenceRun matches runs of three or more consecutive backticks.
// Used by [neutraliseFenceSequences] to insert a zero-width space
// between the second and third backtick, breaking the fence without
// discarding content. After insertion the input contains no more
// runs of 3+ — a key invariant that makes the operation idempotent.
var fenceRun = regexp.MustCompile("`{3,}")

// templateFuncMap is the shared function map registered on every
// [text/template] used by the skeleton generator. Call sites in
// non-code locations pipe their values through the appropriate
// helper (e.g. `{{ .Name | escapeMarkdown }}`); code-location sites
// deliberately omit the pipe because validation already guarantees
// the character class is safe.
var templateFuncMap = template.FuncMap{
	"escapeYAML":              escapeYAML,
	"escapeMarkdown":          escapeMarkdown,
	"escapeMarkdownCodeBlock": escapeMarkdownCodeBlock,
	"escapeTOML":              escapeTOML,
	"escapeComment":           escapeComment,
	"escapeShellArg":          escapeShellArg,
}

// escapeYAML returns the input as a double-quoted YAML scalar value
// with every interior `\` and `"` escaped and ASCII control bytes
// rendered as `\xHH`. Unconditional double-quoting avoids the quoting
// heuristic's edge cases (YAML 1.1 vs 1.2 implicit typing of `yes`,
// `no`, numerals, etc.). NUL bytes cannot appear in YAML even when
// escaped, per YAML 1.2 §5.5, so they are stripped.
func escapeYAML(s string) string {
	s = replaceInvalidUTF8(s)

	var b strings.Builder

	b.Grow(len(s) + yamlWrapOverhead)
	b.WriteByte('"')

	for _, r := range s {
		b.WriteString(escapeYAMLRune(r))
	}

	b.WriteByte('"')

	return b.String()
}

// escapeYAMLRune returns the YAML escape sequence for a single rune,
// or the rune rendered verbatim when no escape is needed. Extracted
// out of escapeYAML so the top-level function stays under the
// cyclomatic-complexity budget.
func escapeYAMLRune(r rune) string {
	switch r {
	case nulRune:
		return "" // stripped
	case '\\':
		return `\\`
	case '"':
		return `\"`
	case '\n':
		return `\n`
	case '\r':
		return `\r`
	case '\t':
		return `\t`
	}

	if r < asciiControlBound || r == delRune {
		return fmt.Sprintf(`\x%02X`, r)
	}

	return string(r)
}

// escapeMarkdown returns the input with CommonMark punctuation that
// initiates a construct (backslash, backtick, asterisk, square
// brackets, angle brackets, pipe, curly braces, exclamation, hash)
// backslash-escaped. Dot, hyphen, plus, and underscore are
// intentionally NOT escaped:
//
//   - Dot, hyphen, plus have meaning only at line-start or in
//     specific positions; universal escaping would corrupt prose
//     (e.g. version strings like `v1.0.0`).
//   - Underscore only initiates CommonMark emphasis when it is
//     word-adjacent (`_foo_`), not mid-word (`foo_bar`). Universal
//     escaping would break common identifier-shaped content.
//     More importantly, `_` is in the identity-guaranteed safe
//     character class; escaping it would break that promise.
//
// Callers must place `escapeMarkdown` only in inline-prose contexts,
// never inside a heading line or fenced code block.
func escapeMarkdown(s string) string {
	s = replaceInvalidUTF8(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	var b strings.Builder

	b.Grow(len(s))

	for _, r := range s {
		switch r {
		case '\\', '`', '*', '[', ']', '<', '>', '|', '{', '}', '!', '#':
			b.WriteByte('\\')
			b.WriteRune(r)
		case nulRune:
			// stripped
		default:
			b.WriteRune(r)
		}
	}

	return b.String()
}

// escapeMarkdownCodeBlock sanitises content destined for a fenced
// Markdown code block. Runs of 3+ backticks would close the fence
// prematurely; we escape them by inserting a zero-width space after
// the opening backtick so the sequence no longer terminates a fence
// but renders identically in typical viewers.
func escapeMarkdownCodeBlock(s string) string {
	s = replaceInvalidUTF8(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\x00", "")

	return neutraliseFenceSequences(s)
}

// neutraliseFenceSequences inserts a U+200B ZERO WIDTH SPACE after
// every pair of consecutive backticks in any run of three or more,
// so no run of 3+ remains. Repeated application is a no-op: once a
// run has been split, subsequent rewrites find nothing to change.
// Visual rendering is unchanged in typical Markdown viewers; a
// Markdown parser no longer sees a closing fence.
func neutraliseFenceSequences(s string) string {
	const (
		zwsp     = "\u200B"
		pairSize = 2
	)

	return fenceRun.ReplaceAllStringFunc(s, func(match string) string {
		var b strings.Builder

		b.Grow(len(match) + len(zwsp)*len(match)/pairSize)

		for i := 0; i < len(match); i += pairSize {
			end := i + pairSize
			if end > len(match) {
				end = len(match)
			}

			b.WriteString(match[i:end])

			if end < len(match) {
				b.WriteString(zwsp)
			}
		}

		return b.String()
	})
}

// escapeTOML returns the input as a TOML basic-string value (without
// the enclosing quotes — callers still wrap in `"..."`). Backslash,
// quote, and control bytes are escaped using TOML's own escape
// sequences.
func escapeTOML(s string) string {
	s = replaceInvalidUTF8(s)

	var b strings.Builder

	b.Grow(len(s))

	for _, r := range s {
		b.WriteString(escapeTOMLRune(r))
	}

	return b.String()
}

// tomlEscapeTable maps runes with dedicated TOML escape sequences
// to those sequences. Runes not in the table fall through to the
// control-range test or are written verbatim.
var tomlEscapeTable = map[rune]string{
	nulRune: "",
	'\\':    `\\`,
	'"':     `\"`,
	'\b':    `\b`,
	'\t':    `\t`,
	'\n':    `\n`,
	'\f':    `\f`,
	'\r':    `\r`,
}

// escapeTOMLRune returns the TOML escape sequence for a single rune,
// or the rune rendered verbatim when no escape is needed.
func escapeTOMLRune(r rune) string {
	if esc, ok := tomlEscapeTable[r]; ok {
		return esc
	}

	if r < asciiControlBound || r == delRune {
		return fmt.Sprintf(`\u%04X`, r)
	}

	return string(r)
}

// escapeComment sanitises free-form text for single-line comment
// contexts (`#` in justfile / YAML / CODEOWNERS). Every newline
// sequence collapses to a single space so the next line cannot
// escape comment scope and inject content. NUL bytes become spaces
// for the same reason.
func escapeComment(s string) string {
	s = replaceInvalidUTF8(s)
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\x00", " ")

	return s
}

// escapeShellArg returns the input as a single-quoted POSIX shell
// argument with any interior single quote expanded to `'\”`. Safe
// to interpolate into `sh`-executed recipe bodies (e.g. justfile) so
// long as the surrounding context wraps the result in `'...'`.
func escapeShellArg(s string) string {
	s = replaceInvalidUTF8(s)

	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// replaceInvalidUTF8 returns s unchanged if it is valid UTF-8, or a
// copy with every invalid byte replaced by U+FFFD otherwise. This is
// the first step of every escape function so downstream logic can
// assume valid runes.
func replaceInvalidUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}

	return strings.ToValidUTF8(s, "\uFFFD")
}
