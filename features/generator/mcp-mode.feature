@generator @integration @annotations
Feature: MCP publication mode
  A generated tool publishes its commands over MCP compactly by default: three
  discovery tools, whatever the size of the command tree. A project that wants
  one native tool per command records properties.mcp.mode: direct, which the
  generator renders into props.Tool.MCP so the built binary carries the
  decision without reading the manifest. The mode survives regeneration and
  reconstructs from the code.

  Covers https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0201-gtb-consumes-go-mcp (D3).

  Scenario: a fresh project says nothing about the mode, which is compact
    Given a freshly generated gtb project
    Then the project manifest does not contain "mcp:"
    And the generated "pkg/cmd/root/cmd.go" file does not contain "MCPConfig"

  Scenario: set mcp.mode direct renders the Tool field and survives regenerate
    Given a freshly generated gtb project
    When I run gtb in the project with "set mcp.mode direct"
    Then the project exit code is 0
    And the project manifest contains "mode: direct"
    And the generated "pkg/cmd/root/cmd.go" file contains "props.MCPConfig{Mode: props.MCPDirect}"
    When I run gtb in the project with "regenerate project"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file contains "props.MCPConfig{Mode: props.MCPDirect}"
    When I run gtb in the project with "regenerate manifest"
    Then the project exit code is 0
    And the project manifest contains "mode: direct"

  Scenario: set mcp.mode compact returns to the default rendering
    Given a freshly generated gtb project
    When I run gtb in the project with "set mcp.mode direct"
    And I run gtb in the project with "set mcp.mode compact"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file does not contain "MCPConfig"

  Scenario: an unknown mode is refused and nothing changes
    Given a freshly generated gtb project
    When I run gtb in the project with "set mcp.mode sideways"
    Then the project exit code is not zero
    And the project manifest does not contain "mcp:"
    And the generated "pkg/cmd/root/cmd.go" file does not contain "MCPConfig"
