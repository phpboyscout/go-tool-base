package templates

import "testing"

// A generated command that takes positional arguments has to SAY so. Cobra builds
// its usage line from Use, and the generator emitted the bare command name — so
// `Args: cobra.ExactArgs(1)` enforced an argument that `--help` never mentioned:
//
//	Usage:
//	  keryx probeargs [flags]
//
// A user found out by running it and being rejected, and the same omission
// reached --help-derived documentation and the MCP tool surface. Issue #25.
func TestUsageArgs(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		args string
		want string
	}{
		// The common cases, and the shape a reader already knows from man pages:
		// angle brackets are required, square brackets optional.
		"one required":   {"ExactArgs(1)", " <arg>"},
		"two required":   {"ExactArgs(2)", " <arg1> <arg2>"},
		"three required": {"ExactArgs(3)", " <arg1> <arg2> <arg3>"},
		"at least one":   {"MinimumNArgs(1)", " <arg> [more...]"},
		"at least two":   {"MinimumNArgs(2)", " <arg1> <arg2> [more...]"},
		"any number":     {"ArbitraryArgs", " [args...]"},
		"at least none":  {"MinimumNArgs(0)", " [args...]"},
		"up to two":      {"MaximumNArgs(2)", " [arg1] [arg2]"},
		"one or two":     {"RangeArgs(1, 2)", " <arg> [arg2]"},
		"one to three":   {"RangeArgs(1, 3)", " <arg> [arg2] [arg3]"},

		// Nothing to say.
		"none":  {"NoArgs", ""},
		"unset": {"", ""},

		// A validator about which values are allowed, not how many. Guessing a
		// count here would state something untrue.
		"only valid args": {"OnlyValidArgs", ""},
		"unrecognised":    {"SomethingElse(2)", ""},
		"malformed":       {"ExactArgs(", ""},
		"non-numeric":     {"ExactArgs(n)", ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := usageArgs(tc.args); got != tc.want {
				t.Errorf("usageArgs(%q) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

// The placeholder is appended to the command name, because cobra takes the
// command's Name() from the first word of Use — so anything after it is usage
// text and nothing else breaks.
func TestUsageArgsAppendsAfterTheName(t *testing.T) {
	t.Parallel()

	data := CommandData{Name: "probeargs", Args: "ExactArgs(1)"}

	if got := commandUse(data); got != "probeargs <arg>" {
		t.Errorf("commandUse = %q, want %q", got, "probeargs <arg>")
	}

	bare := CommandData{Name: "probeargs"}
	if got := commandUse(bare); got != "probeargs" {
		t.Errorf("a command with no args must keep a bare Use, got %q", got)
	}
}

// A pure group's RunE is setup.GroupRunE, which answers any positional argument
// with "unknown command". Advertising arguments in its usage line would promise
// something the command refuses — the exact dishonesty both this change and spec
// 0190 exist to remove.
func TestUsageArgsAreNotAdvertisedByAPureGroup(t *testing.T) {
	t.Parallel()

	group := CommandData{Name: "voice", Args: "ExactArgs(1)", PureGroup: true}

	if got := commandUse(group); got != "voice" {
		t.Errorf("a pure group must not advertise arguments it will reject, got %q", got)
	}

	working := CommandData{Name: "voice", Args: "ExactArgs(1)"}
	if got := commandUse(working); got != "voice <arg>" {
		t.Errorf("a command that does its own work still states them, got %q", got)
	}
}
