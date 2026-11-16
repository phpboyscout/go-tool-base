@generator @integration
Feature: MCP command exposure gating
  gtb disable mcp withholds a command from the MCP tool surface and gtb
  enable mcp puts it back. Each verb records the tri-state mcp_enabled
  decision in the manifest and re-renders the command's cmd.go so the
  generated setup.ExcludeFromMCP / setup.IncludeInMCP marker matches.
  CLI behaviour is unaffected; only the MCP surface is gated.

  A command gtb did not generate (a built-in, or one attached from a module)
  has no cmd.go to mark, so its path goes on properties.mcp.withhold and into
  the root's props.Tool.MCP.

  Covers https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0089-mcp-command-exposure-gating
  and https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0213-a-tool-can-withhold-any-command-from-mcp.

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

  Scenario: a hand-edited command named first does not cost a later one its subcommands
    # Issue #167: given several paths, the first cmd.go kept as hand-edited
    # left every later parent re-rendered without its subcommands.
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name hook --short hooks"
    And I run gtb in the project with "generate command --name worklist --short worklists"
    And I run gtb in the project with "generate command --name list --parent worklist --short list-them"
    And I hand-edit the generated "pkg/cmd/hook/cmd.go" file
    And I run gtb in the project with "disable mcp hook worklist"
    Then the project exit code is 0
    And the generated "pkg/cmd/hook/cmd.go" file contains "hand-edited, do not clobber"
    And the generated "pkg/cmd/worklist/cmd.go" file contains "setup.ExcludeFromMCP(cmd)"
    And the generated "pkg/cmd/worklist/cmd.go" file contains "Register(list.NewCmdList"

  Scenario: a built-in or attached command is withheld by path
    Given a freshly generated gtb project
    When I run gtb in the project with "disable mcp sign config/trust"
    Then the project exit code is 0
    And the project output contains "Withheld by path: sign, config/trust."
    And the project output contains "mcp tools"
    And the project manifest contains "withhold:"
    And the generated "pkg/cmd/root/cmd.go" file contains 'Withhold: []string{"config/trust", "sign"}'

  Scenario: a withheld path survives a regenerate and comes off with enable mcp
    Given a freshly generated gtb project
    When I run gtb in the project with "disable mcp sign config/trust"
    And I run gtb in the project with "regenerate project --overwrite allow"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file contains 'Withhold: []string{"config/trust", "sign"}'
    When I run gtb in the project with "enable mcp sign"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file contains 'Withhold: []string{"config/trust"}'
    When I run gtb in the project with "enable mcp config/trust"
    Then the project exit code is 0
    And the project manifest does not contain "withhold"
    And the generated "pkg/cmd/root/cmd.go" file does not contain "Withhold"

  Scenario: a path under a withheld command cannot be exposed on its own
    Given a freshly generated gtb project
    When I run gtb in the project with "disable mcp keys"
    And I run gtb in the project with "enable mcp keys/publish"
    Then the project exit code is not zero
    And the project output contains "keys/publish is under keys"
    And the project manifest contains "withhold:"
