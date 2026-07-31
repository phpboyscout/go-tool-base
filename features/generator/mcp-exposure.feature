@generator @integration
Feature: MCP command exposure gating
  gtb disable mcp withholds a command from the MCP tool surface and gtb
  enable mcp puts it back. Each verb records the tri-state mcp_enabled
  decision in the manifest and re-renders the command's cmd.go so the
  generated setup.ExcludeFromMCP / setup.IncludeInMCP marker matches.
  CLI behaviour is unaffected; only the MCP surface is gated.

  Covers https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0089-mcp-command-exposure-gating.

  Scenario: disable mcp withholds a command from the MCP surface
    Given a gtb project with a "post" command
    When I run gtb in the project with "disable mcp post"
    Then the project exit code is 0
    And the generated "pkg/cmd/post/cmd.go" file contains "setup.ExcludeFromMCP(cmd)"
    And the project manifest contains "mcp_enabled: false"

  Scenario: enable mcp restores a previously-excluded command
    Given a gtb project with a "post" command
    When I run gtb in the project with "disable mcp post"
    And I run gtb in the project with "enable mcp post"
    Then the project exit code is 0
    And the generated "pkg/cmd/post/cmd.go" file contains "setup.IncludeInMCP(cmd)"
    And the project manifest contains "mcp_enabled: true"
