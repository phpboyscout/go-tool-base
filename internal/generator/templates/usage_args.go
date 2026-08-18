package templates

import (
	"fmt"
	"strconv"
	"strings"
)

// commandUse builds the Use string cobra renders as the command's usage line.
//
// Cobra takes a command's Name() from the FIRST WORD of Use, so anything after
// the name is usage text and nothing else is affected by it.
func commandUse(data CommandData) string {
	// A pure group wires setup.GroupRunE, which answers any positional argument
	// with "unknown command" (spec 0190). Advertising arguments it will refuse
	// would be the same dishonesty this function exists to fix, one level up.
	if data.PureGroup {
		return data.Name
	}

	return data.Name + usageArgs(data.Args)
}

// usageArgs renders the positional arguments a command accepts, derived from the
// cobra validator it was generated with.
//
// `Args: cobra.ExactArgs(1)` made cobra ENFORCE an argument, while Use said only
// the command's name — so `--help` printed "[flags]" and never mentioned the
// argument at all (issue #25). A user found out by being rejected, and the same
// omission reached --help-derived documentation and the MCP tool surface.
//
// The names are generic because the validator is all there is to go on: it says
// how MANY arguments a command takes, never what they mean. That still answers
// the question the usage line was failing to answer — whether an argument is
// needed, how many, and which are optional — and it applies to every command
// already generated, with nothing for anyone to declare. Meaningful names would
// need a new input to carry them.
//
// Convention is the one a reader already knows from man pages: <required> and
// [optional].
func usageArgs(args string) string {
	name, n, ok := parseArgsValidator(args)
	if !ok {
		return ""
	}

	switch name {
	case "NoArgs":
		return ""

	case "ArbitraryArgs":
		return " [args...]"

	case "ExactArgs":
		return " " + placeholders("<arg", ">", n)

	case "MinimumNArgs":
		if n == 0 {
			return " [args...]"
		}

		return " " + placeholders("<arg", ">", n) + " [more...]"

	case "MaximumNArgs":
		return " " + placeholders("[arg", "]", n)

	case "RangeArgs":
		return rangeUsage(args)

	default:
		// A validator this does not recognise, or one about which values are
		// allowed rather than how many (OnlyValidArgs). Saying nothing is right:
		// a guessed count would state something untrue.
		return ""
	}
}

// placeholders renders n slots, numbering them only when there is more than one —
// a single "<arg>" reads better than "<arg1>".
func placeholders(open, close string, n int) string {
	if n == 1 {
		return open + close
	}

	parts := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		parts = append(parts, open+strconv.Itoa(i)+close)
	}

	return strings.Join(parts, " ")
}

// rangeArgsParts is how many numbers RangeArgs carries: a minimum and a maximum.
const rangeArgsParts = 2

// rangeUsage renders RangeArgs(min, max): the first min are required, the rest
// optional.
func rangeUsage(args string) string {
	inner, ok := validatorArgs(args)
	if !ok {
		return ""
	}

	parts := strings.Split(inner, ",")
	if len(parts) != rangeArgsParts {
		return ""
	}

	minArgs, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return ""
	}

	maxArgs, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || maxArgs < minArgs {
		return ""
	}

	var b strings.Builder

	for i := 1; i <= maxArgs; i++ {
		if i <= minArgs {
			b.WriteString(" " + placeholderAt("<arg", ">", i, minArgs))

			continue
		}

		fmt.Fprintf(&b, " [arg%d]", i)
	}

	return b.String()
}

// placeholderAt numbers a required slot only when more than one is required, so
// RangeArgs(1, 3) reads "<arg> [arg2] [arg3]" rather than "<arg1> …".
func placeholderAt(open, close string, i, total int) string {
	if total == 1 {
		return open + close
	}

	return open + strconv.Itoa(i) + close
}

// parseArgsValidator splits a validator expression into its name and its integer
// argument, reporting whether it is one this package can read at all.
func parseArgsValidator(args string) (string, int, bool) {
	args = strings.TrimSpace(args)
	if args == "" {
		return "", 0, false
	}

	if !strings.Contains(args, "(") {
		// A bare validator such as NoArgs or ArbitraryArgs.
		return args, 0, true
	}

	name, _, _ := strings.Cut(args, "(")

	inner, ok := validatorArgs(args)
	if !ok {
		return "", 0, false
	}

	// RangeArgs carries two numbers and is parsed separately.
	if strings.Contains(inner, ",") {
		return name, 0, true
	}

	n, err := strconv.Atoi(strings.TrimSpace(inner))
	if err != nil {
		return "", 0, false
	}

	return name, n, true
}

// validatorArgs returns whatever is between the parentheses.
func validatorArgs(args string) (string, bool) {
	open := strings.Index(args, "(")
	close := strings.LastIndex(args, ")")

	if open < 0 || close < open {
		return "", false
	}

	return args[open+1 : close], true
}
