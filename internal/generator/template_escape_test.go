package generator

// Tests for the template-escape helpers. Uses the internal test
// package because the helpers are unexported — the template function
// map is the public surface, verified indirectly through
// skeleton-render tests.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// safeCharClass covers the characters for which every escape function
// is the identity. Keeping the regex anchored here means a helper
// that decides to escape one of these characters will fail a test,
// forcing the authoring decision to be explicit.
const safeCharClass = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 _.,/-"

func TestEscapeYAML_IdentityOnSafeClass(t *testing.T) {
	t.Parallel()

	got := escapeYAML(safeCharClass)
	// Wrapped in quotes, but the interior must equal the input.
	require.True(t, strings.HasPrefix(got, `"`) && strings.HasSuffix(got, `"`))
	assert.Equal(t, safeCharClass, got[1:len(got)-1])
}

func TestEscapeYAML_EscapesSpecials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		parses  bool // output must parse as YAML
		wantNot string
	}{
		{name: "colon", input: "key: value", parses: true},
		{name: "newline", input: "line1\nline2", parses: true},
		{name: "quote", input: `She said "hi"`, parses: true},
		{name: "backslash", input: `path\name`, parses: true},
		{name: "control char", input: "\x1b[31m", parses: true},
		{name: "nul stripped", input: "before\x00after", parses: true, wantNot: "\x00"},
		{name: "boolean-like", input: "yes", parses: true},
		{name: "numeric-like", input: "1.0", parses: true},
		{name: "yaml directive", input: "---", parses: true},
		{name: "flow brace", input: "[1, 2, 3]", parses: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := escapeYAML(tc.input)
			if tc.wantNot != "" {
				assert.NotContains(t, got, tc.wantNot)
			}

			if tc.parses {
				// Wrap in a minimal document and round-trip through
				// a YAML parser to prove validity.
				var out struct {
					V string `yaml:"v"`
				}

				require.NoError(t, yaml.Unmarshal([]byte("v: "+got+"\n"), &out),
					"YAML parse failure: %q -> %q", tc.input, got)
			}
		})
	}
}

func TestEscapeMarkdown_IdentityOnSafeClass(t *testing.T) {
	t.Parallel()

	assert.Equal(t, safeCharClass, escapeMarkdown(safeCharClass))
}

func TestEscapeMarkdown_EscapesSpecials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		contains []string
	}{
		{name: "star", input: "*bold*", contains: []string{`\*bold\*`}},
		{name: "backtick", input: "`code`", contains: []string{"\\`code\\`"}},
		// Underscore is in the safe class and intentionally NOT
		// escaped — see the doc on escapeMarkdown for rationale.
		{name: "brackets", input: "[link](x)", contains: []string{`\[link\]`}},
		{name: "heading-like", input: "# malicious", contains: []string{`\#`}},
		{name: "html-like", input: "<script>", contains: []string{`\<`, `\>`}},
		{name: "pipe", input: "a|b", contains: []string{`a\|b`}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := escapeMarkdown(tc.input)
			for _, want := range tc.contains {
				assert.Contains(t, got, want)
			}
		})
	}
}

func TestEscapeMarkdown_PreservesVersionStrings(t *testing.T) {
	t.Parallel()

	// v1.0.0 must NOT become v1\.0\.0; ordinary dots are not escaped.
	assert.Equal(t, "v1.0.0", escapeMarkdown("v1.0.0"))
	assert.Equal(t, "1 + 2 = 3", escapeMarkdown("1 + 2 = 3"))
}

func TestEscapeMarkdownCodeBlock_NeutralisesFence(t *testing.T) {
	t.Parallel()

	// A closing fence inside the input must be neutralised so it
	// does not terminate the enclosing fence.
	in := "code\n```\nmore"
	got := escapeMarkdownCodeBlock(in)
	assert.NotContains(t, got, "\n```\n", "closing fence must be interrupted")
	assert.Contains(t, got, "`")
}

func TestEscapeTOML_IdentityOnSafeClass(t *testing.T) {
	t.Parallel()

	assert.Equal(t, safeCharClass, escapeTOML(safeCharClass))
}

func TestEscapeTOML_ProducesParseableOutput(t *testing.T) {
	t.Parallel()

	tests := []string{
		"plain",
		`quoted "word"`,
		`back\slash`,
		"line1\nline2",
		"\x07 BEL",
		"\x7f DEL",
		"before\x00after",
	}

	for _, in := range tests {
		t.Run(in, func(t *testing.T) {
			t.Parallel()

			got := escapeTOML(in)
			doc := []byte(`v = "` + got + "\"\n")

			var out struct {
				V string `toml:"v"`
			}

			require.NoError(t, toml.Unmarshal(doc, &out),
				"TOML parse failure: %q -> %q", in, got)
		})
	}
}

func TestEscapeComment_CollapsesNewlines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "lf", input: "line1\nline2", want: "line1 line2"},
		{name: "crlf", input: "line1\r\nline2", want: "line1 line2"},
		{name: "cr", input: "line1\rline2", want: "line1 line2"},
		{name: "nul becomes space", input: "a\x00b", want: "a b"},
		{name: "preserves other content", input: "safe comment", want: "safe comment"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, escapeComment(tc.input))
		})
	}
}

func TestEscapeShellArg_WrapsAndEscapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain", input: "hello", want: `'hello'`},
		{name: "with single quote", input: `it's`, want: `'it'\''s'`},
		{name: "with dollar", input: `$HOME`, want: `'$HOME'`},
		{name: "with backtick", input: "`cmd`", want: "'`cmd`'"},
		{name: "with semicolon", input: `a; b`, want: `'a; b'`},
		{name: "empty", input: "", want: `''`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, escapeShellArg(tc.input))
		})
	}
}

// TestEscape_Idempotent_WhereApplicable asserts textual idempotence
// only for helpers whose output has no escape-bearing characters in
// itself. Functions that escape `\` or `"` (escapeYAML, escapeTOML,
// escapeMarkdown, escapeShellArg) produce output that contains
// `\`/`"` by construction, so `f(f(x))` re-escapes them and differs
// from `f(x)` as a string. For those helpers the relevant property
// is parse-round-trip — tested separately in the format-specific
// assertions above.
func TestEscape_Idempotent_WhereApplicable(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"plain text",
		"multi\nline",
		"\x00\x01\x02 control",
		"code ```fence",
	}

	for _, in := range inputs {
		assertIdempotent(t, "escapeMarkdownCodeBlock", escapeMarkdownCodeBlock, in)
		assertIdempotent(t, "escapeComment", escapeComment, in)
	}
}

// TestEscapeYAML_RoundTrip verifies that a value passed through
// escapeYAML parses back to the (NUL-stripped) input. YAML is the
// primary target format for scaffolded config scalars, so the
// parse-equivalence property is what actually protects against
// injection.
func TestEscapeYAML_RoundTrip(t *testing.T) {
	t.Parallel()

	inputs := []string{"plain", `"quoted"`, "multi\nline", `back\slash`, "a'b", "yes", "1.0"}

	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			t.Parallel()

			got := parseYAMLScalar(t, escapeYAML(in))
			assert.Equal(t, in, got)
		})
	}
}

// TestEscapeTOML_RoundTrip — same property as the YAML test, for
// TOML basic-string values. NUL bytes are stripped; other content
// must survive byte-for-byte.
func TestEscapeTOML_RoundTrip(t *testing.T) {
	t.Parallel()

	inputs := []string{"plain", `back\slash`, `"quoted"`, "multi\nline"}

	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			t.Parallel()

			got := parseTOMLScalar(t, escapeTOML(in))
			assert.Equal(t, in, got)
		})
	}
}

func parseYAMLScalar(t *testing.T, quoted string) string {
	t.Helper()

	var out struct {
		V string `yaml:"v"`
	}

	require.NoError(t, yaml.Unmarshal([]byte("v: "+quoted+"\n"), &out))

	return out.V
}

func parseTOMLScalar(t *testing.T, interior string) string {
	t.Helper()

	var out struct {
		V string `toml:"v"`
	}

	require.NoError(t, toml.Unmarshal([]byte(`v = "`+interior+"\"\n"), &out))

	return out.V
}

func assertIdempotent(t *testing.T, name string, f func(string) string, in string) {
	t.Helper()

	once := f(in)
	twice := f(once)

	if once != twice {
		t.Errorf("%s is not idempotent: f(%q) = %q; f(f(%q)) = %q", name, in, once, in, twice)
	}
}

func TestReplaceInvalidUTF8(t *testing.T) {
	t.Parallel()

	// Valid input returns unchanged.
	assert.Equal(t, "hello", replaceInvalidUTF8("hello"))

	// Invalid byte sequences get replaced with U+FFFD.
	got := replaceInvalidUTF8("a\xffb")
	assert.Equal(t, "a\uFFFDb", got)
}

func TestEscape_TemplateFuncMapCompletenes(t *testing.T) {
	t.Parallel()

	// Every function this file tests must also appear in the exported
	// map — a safety net against a new helper being added without
	// registration.
	wantFns := []string{
		"escapeYAML",
		"escapeMarkdown",
		"escapeMarkdownCodeBlock",
		"escapeTOML",
		"escapeComment",
		"escapeShellArg",
	}
	for _, fn := range wantFns {
		_, ok := templateFuncMap[fn]
		assert.Truef(t, ok, "templateFuncMap missing %q", fn)
	}
}

// silence unused import in this file if go-toml is ever dropped.
var _ = bytes.NewReader
