package setup

import "github.com/spf13/cobra"

// MCPExposure is a command's explicit decision about whether it appears on the
// MCP tool surface. The zero value is [MCPExposureInherit], so an unset field
// or absent annotation naturally means "inherit from the nearest ancestor that
// sets one". It mirrors the generator manifest's mcp_enabled *bool:
// nil↔Inherit, true↔Exposed, false↔Excluded.
//
// Exposure is build-time only: the decision is baked into the binary as a
// command annotation, with no runtime config lever — the MCP tool surface is
// fixed and auditable in the shipped binary. See
// docs/development/specs/2026-06-19-mcp-command-exposure-gating.md.
type MCPExposure uint8

const (
	// MCPExposureInherit means the command states no explicit preference and
	// inherits the nearest ancestor's; the tree default is exposed.
	MCPExposureInherit MCPExposure = iota
	// MCPExposureExposed means the command is explicitly on the MCP surface.
	// Its primary use is overriding an excluded ancestor.
	MCPExposureExposed
	// MCPExposureExcluded means the command is explicitly withheld from the
	// MCP surface. Its descendants inherit this unless they set Exposed.
	MCPExposureExcluded
)

// MCPExposureAnnotation is the cobra.Command.Annotations key under which a
// command's explicit MCP-exposure decision is recorded. It mirrors
// [FeatureAnnotation]. The value is [mcpExposureValueExposed] or
// [mcpExposureValueExcluded]; the key is absent when the command inherits.
const MCPExposureAnnotation = "gtb.mcp.exposure"

const (
	mcpExposureValueExposed  = "exposed"
	mcpExposureValueExcluded = "excluded"
)

// MCPExposureFromBool maps a tri-state *bool (the manifest/CLI representation)
// to the enum: nil→Inherit, true→Exposed, false→Excluded.
func MCPExposureFromBool(b *bool) MCPExposure {
	switch {
	case b == nil:
		return MCPExposureInherit
	case *b:
		return MCPExposureExposed
	default:
		return MCPExposureExcluded
	}
}

// ExcludeFromMCP marks cmd as excluded from the MCP tool surface: when the mcp
// feature is enabled, cmd — and, by inheritance, descendants that do not
// themselves call [IncludeInMCP] — is omitted from `mcp tools` / `mcp start`.
// CLI behaviour is unaffected; the command remains fully runnable. Returns cmd
// for chaining.
func ExcludeFromMCP(cmd *Command) *Command {
	return stampMCPExposure(cmd, mcpExposureValueExcluded)
}

// IncludeInMCP marks cmd as explicitly exposed on the MCP tool surface. Its
// primary use is to override an excluded ancestor so a specific subcommand
// stays exposed; it is also stamped for any command whose exposure is
// explicitly Exposed. Returns cmd for chaining.
func IncludeInMCP(cmd *Command) *Command {
	return stampMCPExposure(cmd, mcpExposureValueExposed)
}

// stampMCPExposure records value under [MCPExposureAnnotation] on the embedded
// cobra command, initialising the annotation map if needed — exactly as [Wrap]
// stamps [FeatureAnnotation]. A nil *Command or nil embedded command is a no-op.
func stampMCPExposure(cmd *Command, value string) *Command {
	if cmd == nil || cmd.Command == nil {
		return cmd
	}

	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}

	cmd.Annotations[MCPExposureAnnotation] = value

	return cmd
}

// MCPExposureOf returns cmd's own explicit exposure decision, or
// [MCPExposureInherit] when cmd carries no exposure annotation. It operates on
// the raw *cobra.Command and is nil-safe.
func MCPExposureOf(cmd *cobra.Command) MCPExposure {
	if cmd == nil || cmd.Annotations == nil {
		return MCPExposureInherit
	}

	switch cmd.Annotations[MCPExposureAnnotation] {
	case mcpExposureValueExposed:
		return MCPExposureExposed
	case mcpExposureValueExcluded:
		return MCPExposureExcluded
	default:
		return MCPExposureInherit
	}
}

// IsExposedToMCP reports whether cmd is exposed on the MCP tool surface. It
// walks cmd and its ancestors and returns the nearest explicit decision
// (Exposed→true, Excluded→false), defaulting to true when no command in the
// chain sets one. This yields subtree exclusion by default while letting a
// descendant re-expose itself via [IncludeInMCP]. Operates on the raw
// *cobra.Command so it is callable from the root MCP selector closure; nil-safe.
func IsExposedToMCP(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		switch MCPExposureOf(c) {
		case MCPExposureExposed:
			return true
		case MCPExposureExcluded:
			return false
		case MCPExposureInherit:
			// No explicit decision here; keep walking up the tree.
		}
	}

	return true
}
