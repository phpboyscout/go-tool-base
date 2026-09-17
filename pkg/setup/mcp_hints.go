package setup

import (
	"strconv"

	"github.com/spf13/cobra"
)

// The annotation keys are ophis's spellings (ophis v1.1.4 annotations.go), so a
// consumer that sets them by hand today keeps working, and go/mcp's Cobra
// binding reads the same keys after the replacement (spec 0201 D4).
const (
	MCPTitleAnnotation       = "title"
	MCPReadOnlyAnnotation    = "readOnlyHint"
	MCPDestructiveAnnotation = "destructiveHint"
	MCPIdempotentAnnotation  = "idempotentHint"
	MCPOpenWorldAnnotation   = "openWorldHint"
)

// MCPHints are the MCP tool annotations a command declares about itself: a
// display title and the four behavioural hints. A nil hint states nothing and
// writes no key. Hints describe one command and do not inherit down a
// subtree, unlike [MCPExposure].
type MCPHints struct {
	Title       string
	ReadOnly    *bool
	Destructive *bool
	Idempotent  *bool
	OpenWorld   *bool
}

// MCPReadOnly describes a command that reads and never mutates anything.
func MCPReadOnly() MCPHints {
	return MCPHints{ReadOnly: new(true), Destructive: new(false), Idempotent: new(true), OpenWorld: new(false)}
}

// MCPLocalWrite describes a command that mutates local state additively and
// can be repeated with the same arguments to the same effect.
func MCPLocalWrite() MCPHints {
	return MCPHints{ReadOnly: new(false), Destructive: new(false), Idempotent: new(true), OpenWorld: new(false)}
}

// MCPOpenWorld describes a command that reaches outside the tool (a release
// source, a provider, a wizard) and cannot promise the same result twice.
func MCPOpenWorld() MCPHints {
	return MCPHints{ReadOnly: new(false), Destructive: new(false), Idempotent: new(false), OpenWorld: new(true)}
}

// MCPDestructive describes a command that may remove or rewrite state in a way
// the caller should confirm first.
func MCPDestructive() MCPHints {
	return MCPHints{ReadOnly: new(false), Destructive: new(true), Idempotent: new(false), OpenWorld: new(false)}
}

// IsZero reports whether the hints state nothing at all.
func (h MCPHints) IsZero() bool {
	return h.Title == "" && h.ReadOnly == nil && h.Destructive == nil && h.Idempotent == nil && h.OpenWorld == nil
}

// annotate adds the hints to cmd's annotations, initialising the map when it
// is nil and leaving every other key (the feature and exposure stamps
// included) in place, exactly as stampMCPExposure does. Nil-safe.
func (h MCPHints) annotate(cmd *cobra.Command) *cobra.Command {
	if cmd == nil {
		return nil
	}

	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}

	if h.Title != "" {
		cmd.Annotations[MCPTitleAnnotation] = h.Title
	}

	for _, hint := range []struct {
		key   string
		value *bool
	}{
		{MCPReadOnlyAnnotation, h.ReadOnly},
		{MCPDestructiveAnnotation, h.Destructive},
		{MCPIdempotentAnnotation, h.Idempotent},
		{MCPOpenWorldAnnotation, h.OpenWorld},
	} {
		if hint.value != nil {
			cmd.Annotations[hint.key] = strconv.FormatBool(*hint.value)
		}
	}

	return cmd
}

// AnnotateMCP applies hints to the embedded cobra command, the way
// [ExcludeFromMCP] stamps exposure. Generated commands call it after their
// exposure marker. Returns cmd for chaining; a nil *Command is a no-op.
func AnnotateMCP(cmd *Command, hints MCPHints) *Command {
	if cmd == nil {
		return nil
	}

	hints.annotate(cmd.Command)

	return cmd
}

// MCPHintsOf reads cmd's own hints back. A key holding anything other than
// "true" or "false" is treated as unset rather than guessed at. Nil-safe.
func MCPHintsOf(cmd *cobra.Command) MCPHints {
	if cmd == nil || cmd.Annotations == nil {
		return MCPHints{}
	}

	return MCPHints{
		Title:       cmd.Annotations[MCPTitleAnnotation],
		ReadOnly:    parseHint(cmd.Annotations, MCPReadOnlyAnnotation),
		Destructive: parseHint(cmd.Annotations, MCPDestructiveAnnotation),
		Idempotent:  parseHint(cmd.Annotations, MCPIdempotentAnnotation),
		OpenWorld:   parseHint(cmd.Annotations, MCPOpenWorldAnnotation),
	}
}

func parseHint(annotations map[string]string, key string) *bool {
	raw, ok := annotations[key]
	if !ok {
		return nil
	}

	value, err := strconv.ParseBool(raw)
	if err != nil {
		return nil
	}

	return &value
}
