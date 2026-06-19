@cli @smoke
Feature: CLI MCP command
  The `gtb mcp` command group manages Model Context Protocol servers for AI
  assistants and editors, and exports the tool's MCP tool definitions. It is
  enabled by default, so these smoke scenarios assert the command tree is wired
  and its subcommands are reachable — without standing up a live MCP server.

  Background:
    Given the gtb binary is built

  Scenario: mcp help lists the server and editor subcommands
    When I run gtb with "mcp --help"
    Then the exit code is 0
    And stdout contains "Manage MCP servers"
    And stdout contains "start"
    And stdout contains "tools"
    And stdout contains "claude"
    And stdout contains "vscode"

  Scenario: mcp tools help describes the JSON export
    When I run gtb with "mcp tools --help"
    Then the exit code is 0
    And stdout contains "Export available MCP tools"

  Scenario: mcp start help shows usage
    When I run gtb with "mcp start --help"
    Then the exit code is 0
    And stdout contains "Start stdio server"

  Scenario: mcp editor subcommand help is reachable
    When I run gtb with "mcp cursor --help"
    Then the exit code is 0
