package generator

// Fuzz tests for the template-escape helpers. Each asserts the two
// properties that matter operationally:
//
//   - No panic on any input.
//   - Output is syntactically valid in the target format (YAML, TOML,
//     parseable shell argument).
//
// Textual idempotence is tested in the non-fuzz suite where
// applicable; it is not a fuzz invariant because quote-wrapping
// escape functions naturally grow across repeated application.

import (
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

func FuzzEscapeYAML(f *testing.F) {
	seeds := []string{
		"", "plain", "key: value", "line1\nline2",
		`"quoted"`, `back\slash`, "yes", "no", "null", "1.0",
		"\x00\x01\x02", "\x7f", "*list", "[1, 2]", "---",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		out := escapeYAML(s)

		var parsed struct {
			V string `yaml:"v"`
		}
		if err := yaml.Unmarshal([]byte("v: "+out+"\n"), &parsed); err != nil {
			t.Fatalf("escapeYAML output does not parse as YAML. input=%q output=%q err=%v",
				s, out, err)
		}

		// Match the helper's own order: invalid UTF-8 is replaced
		// with U+FFFD first, then NUL bytes are stripped.
		wantStr := strings.ToValidUTF8(s, "\uFFFD")
		wantStr = strings.ReplaceAll(wantStr, "\x00", "")

		if parsed.V != wantStr {
			t.Fatalf("YAML round-trip mismatch. input=%q output=%q parsed=%q want=%q",
				s, out, parsed.V, wantStr)
		}
	})
}

func FuzzEscapeTOML(f *testing.F) {
	seeds := []string{
		"", "plain", `"quoted"`, `back\slash`, "line1\nline2",
		"\x00\x01\x02", "\x7f",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		out := escapeTOML(s)

		doc := []byte(`v = "` + out + "\"\n")

		var parsed struct {
			V string `toml:"v"`
		}
		if err := toml.Unmarshal(doc, &parsed); err != nil {
			t.Fatalf("escapeTOML output does not parse as TOML. input=%q output=%q err=%v",
				s, out, err)
		}

		wantStr := strings.ToValidUTF8(s, "\uFFFD")
		wantStr = strings.ReplaceAll(wantStr, "\x00", "")

		if parsed.V != wantStr {
			t.Fatalf("TOML round-trip mismatch. input=%q output=%q parsed=%q want=%q",
				s, out, parsed.V, wantStr)
		}
	})
}

func FuzzEscapeMarkdown(f *testing.F) {
	seeds := []string{
		"", "plain", "*bold*", "# heading", "[link](x)",
		"`code`", "v1.0.0", "a|b", "<script>",
		"multi\nline", "\x00", "invalid\xff",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		// No panic is the main invariant; also assert bounded growth
		// so a future pattern change cannot produce runaway output.
		out := escapeMarkdown(s)

		// Upper bound: every input rune could trigger a single-byte
		// backslash prefix, so len(out) <= 2*len(s) + 8 for safety.
		if len(out) > 2*len(s)+8 {
			t.Fatalf("escapeMarkdown grew input unreasonably: len=%d -> %d", len(s), len(out))
		}
	})
}

func FuzzEscapeMarkdownCodeBlock(f *testing.F) {
	seeds := []string{
		"", "plain code", "```fence", "````long-fence",
		"code with `single`", "multi\nline\ncode", "\x00",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		out := escapeMarkdownCodeBlock(s)

		// Critical invariant: output must contain no run of 3+
		// consecutive backticks. A run would close the enclosing
		// fence prematurely.
		if strings.Contains(out, "```") {
			t.Fatalf("escapeMarkdownCodeBlock left a triple-backtick in output: input=%q output=%q",
				s, out)
		}

		// Idempotent by construction of the regex rewrite: second
		// application finds no 3-runs to break.
		again := escapeMarkdownCodeBlock(out)
		if again != out {
			t.Fatalf("escapeMarkdownCodeBlock not idempotent: f(%q)=%q, f(f(x))=%q", s, out, again)
		}
	})
}

func FuzzEscapeShellArg(f *testing.F) {
	seeds := []string{
		"", "plain", "it's", "$HOME", "`cmd`",
		"a; b", "a && b", "arg with spaces",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		out := escapeShellArg(s)

		// Property: result must start and end with single quotes.
		if !strings.HasPrefix(out, "'") || !strings.HasSuffix(out, "'") {
			t.Fatalf("escapeShellArg result not enclosed in single quotes: %q -> %q", s, out)
		}

		// Property: unwrapping a single-quoted shell argument
		// (replacing '\'' with ') yields the input. Invalid UTF-8
		// is replaced with U+FFFD by the helper; account for that.
		wantStr := strings.ToValidUTF8(s, "\uFFFD")

		unwrapped := strings.ReplaceAll(out[1:len(out)-1], `'\''`, "'")
		if unwrapped != wantStr {
			t.Fatalf("escapeShellArg round-trip mismatch: input=%q output=%q unwrapped=%q want=%q",
				s, out, unwrapped, wantStr)
		}
	})
}
