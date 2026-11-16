package props

import (
	"slices"
	"strings"
	"unicode"

	"gitlab.com/phpboyscout/go/errors"
)

// MCPMode is the publication mode the tool's MCP server uses: compact (three
// discovery tools) or direct (one native tool per command). The zero value is
// compact, so a tool that states nothing publishes compactly (spec 0201 D3).
type MCPMode string

const (
	// MCPCompact publishes search_tools, get_tool_details and call_tool.
	MCPCompact MCPMode = "compact"
	// MCPDirect publishes every exposed command as its own tool.
	MCPDirect MCPMode = "direct"
)

// MCPConfig is the tool author's MCP posture, rendered into the generated
// root from properties.mcp in the manifest. The running binary never reads
// the manifest; changing it is a regenerate and a rebuild.
type MCPConfig struct {
	Mode MCPMode `json:"mode,omitempty" yaml:"mode,omitempty"`
	// Withhold lists the commands kept off the MCP surface, each a
	// slash-separated path from the root such as "config/trust". A path
	// covers its subtree, and no annotation overrules it (spec 0213).
	Withhold []string `json:"withhold,omitempty" yaml:"withhold,omitempty"`
}

// Direct reports whether the tool publishes native per-command tools.
func (c MCPConfig) Direct() bool { return c.Mode == MCPDirect }

func (c MCPConfig) validate() error {
	switch c.Mode {
	case "", MCPCompact, MCPDirect:
	default:
		return errors.Newf("props: Tool.MCP.Mode %q is not compact or direct", string(c.Mode))
	}

	for _, path := range c.Withhold {
		if slices.ContainsFunc(strings.Split(path, "/"), notACommandName) {
			return errors.Newf("props: Tool.MCP.Withhold path %q is not command names separated by /", path)
		}
	}

	return nil
}

func notACommandName(segment string) bool {
	return segment == "" || strings.ContainsFunc(segment, unicode.IsSpace)
}
