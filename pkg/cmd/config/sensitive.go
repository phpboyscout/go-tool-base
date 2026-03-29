package config

import (
	"regexp"
	"strings"
)

var defaultKeyPatterns = []string{"token", "password", "secret", "key", "apikey", "auth"}

var defaultValuePatterns = []*regexp.Regexp{
	regexp.MustCompile(`ghp_[A-Za-z0-9]{36}`),
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{82}`),
}

// Masker detects and masks sensitive configuration values using two independent
// strategies: key-name pattern matching and value content regular expressions.
// The zero value is not useful; construct with NewMasker.
type Masker struct {
	keyPatterns   []string
	valuePatterns []*regexp.Regexp
}

// MaskerOption configures a Masker.
type MaskerOption func(*Masker)

// WithKeyPattern registers an additional key-name substring (case-insensitive)
// that marks a key as sensitive. Extends the built-in list; does not replace it.
func WithKeyPattern(pattern string) MaskerOption {
	return func(m *Masker) {
		m.keyPatterns = append(m.keyPatterns, strings.ToLower(pattern))
	}
}

// WithValuePattern registers an additional compiled regexp that, when matched
// against a value, marks it as sensitive regardless of the key name.
func WithValuePattern(re *regexp.Regexp) MaskerOption {
	return func(m *Masker) {
		m.valuePatterns = append(m.valuePatterns, re)
	}
}

// NewMasker constructs a Masker with built-in key patterns and value regexes,
// extended by any provided options. Built-in defaults are never mutated.
func NewMasker(opts ...MaskerOption) *Masker {
	m := &Masker{
		keyPatterns:   append([]string(nil), defaultKeyPatterns...),
		valuePatterns: append([]*regexp.Regexp(nil), defaultValuePatterns...),
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// IsSensitive returns true if the key's leaf segment matches a sensitive key
// pattern OR the value matches a sensitive value pattern.
func (m *Masker) IsSensitive(key, value string) bool {
	leaf := key
	if i := strings.LastIndex(key, "."); i >= 0 {
		leaf = key[i+1:]
	}

	lower := strings.ToLower(leaf)

	for _, pat := range m.keyPatterns {
		if strings.Contains(lower, pat) {
			return true
		}
	}

	for _, re := range m.valuePatterns {
		if re.MatchString(value) {
			return true
		}
	}

	return false
}

// maskTailLen is the number of trailing characters kept visible when masking.
const maskTailLen = 4

// Mask returns the value with all but the last 4 characters replaced by
// asterisks. Returns a fully asterisked string if the value is 4 characters
// or fewer.
func (m *Masker) Mask(value string) string {
	if len(value) == 0 {
		return ""
	}

	if len(value) <= maskTailLen {
		return strings.Repeat("*", len(value))
	}

	return strings.Repeat("*", len(value)-maskTailLen) + value[len(value)-maskTailLen:]
}

// MaskIfSensitive applies Mask only when IsSensitive returns true for the
// given key/value pair; otherwise returns the value unchanged.
func (m *Masker) MaskIfSensitive(key, value string) string {
	if m.IsSensitive(key, value) {
		return m.Mask(value)
	}

	return value
}
