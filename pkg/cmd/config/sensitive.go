package config

import (
	"regexp"
	"strings"
)

// defaultKeyPatterns are substrings that mark a config key as holding a
// credential when they appear in its leaf segment. Only the leaf is matched:
// a credential's pointer keys (auth.env names a variable, auth.keychain names
// a service/account) share its mid-path and are not secrets, and a literal
// whose leaf is generic (github.auth.value, bitbucket.username) is the
// credential registry's to name through WithLiteralKeys.
var defaultKeyPatterns = []string{
	"token",
	"password",
	"secret",
	"apikey",
	"api_key",
	"auth",
	"app_password",
}

// defaultExactLeaves mark a key as sensitive only when they are the whole
// leaf: "key" as a substring would also mask key types, key sources and key
// ids (ssh.key.type, update.key_source, signing.key_id), which are settings.
var defaultExactLeaves = []string{"key"}

var defaultValuePatterns = []*regexp.Regexp{
	regexp.MustCompile(`ghp_[A-Za-z0-9]{36}`),
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{82}`),
}

// Masker detects and masks sensitive configuration values using two independent
// strategies: key-name pattern matching and value content regular expressions.
// The zero value is not useful; construct with NewMasker.
type Masker struct {
	keyPatterns   []string
	exactLeaves   []string
	literalKeys   map[string]struct{}
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

// WithLiteralKeys marks whole config keys (case-insensitive) as holding a
// secret regardless of their name. NewCmdConfig passes the credential
// registry's literal keys for the tool's enabled features, so GTB's own
// credentials are masked by declaration rather than by the shape of their name.
func WithLiteralKeys(keys ...string) MaskerOption {
	return func(m *Masker) {
		for _, k := range keys {
			if k != "" {
				m.literalKeys[strings.ToLower(k)] = struct{}{}
			}
		}
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
		exactLeaves:   append([]string(nil), defaultExactLeaves...),
		literalKeys:   map[string]struct{}{},
		valuePatterns: append([]*regexp.Regexp(nil), defaultValuePatterns...),
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// IsSensitive reports whether key names a declared literal, whether its leaf
// segment looks like a credential, or whether value matches a known token
// shape. A value pattern applies whatever the key, so a token pasted into a
// pointer key is still masked.
func (m *Masker) IsSensitive(key, value string) bool {
	lowerKey := strings.ToLower(key)
	if _, ok := m.literalKeys[lowerKey]; ok {
		return true
	}

	leaf := lowerKey
	if i := strings.LastIndex(lowerKey, "."); i >= 0 {
		leaf = lowerKey[i+1:]
	}

	for _, pat := range m.keyPatterns {
		if strings.Contains(leaf, pat) {
			return true
		}
	}

	for _, exact := range m.exactLeaves {
		if leaf == exact {
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
