@generator @integration @mcplink
Feature: The mcp feature is a link the generator writes and removes
  The mcp feature is a link kind: cmd/<name>/mcp.go blank-imports the
  framework's pkg/mcp, which declares the feature and contributes the mcp
  command. The manifest entry decides whether the file exists, so a tool that
  disables mcp ships without go/mcp and the MCP SDK, and the rendered root
  never toggles it through SetFeatures.

  Covers https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0202-mcp-as-a-link-kind-and-a-root-command-slot.

  Scenario: A fresh project links mcp and says nothing about it in the manifest
    Given a freshly generated gtb project
    Then the generated "cmd/feattool/mcp.go" file exists
    And the generated "cmd/feattool/mcp.go" file contains "gitlab.com/phpboyscout/go-tool-base/pkg/mcp"
    And the project manifest does not contain "name: mcp"
    And the generated "pkg/cmd/root/cmd.go" file does not contain "McpCmd"

  Scenario: Leaving mcp out of the selected features records the delta and writes no file
    When I generate a gtb project with features "init,update,docs"
    Then the project exit code is 0
    And the project manifest contains "name: mcp"
    And the generated "cmd/feattool/mcp.go" file does not exist
    And the generated "pkg/cmd/root/cmd.go" file does not contain "McpCmd"

  Scenario: Disabling mcp removes the link, and enabling it writes it back
    Given a freshly generated gtb project
    When I run gtb in the project with "disable mcp"
    Then the project exit code is 0
    And the generated "cmd/feattool/mcp.go" file does not exist
    And the project manifest contains "name: mcp"
    And the generated "pkg/cmd/root/cmd.go" file does not contain "McpCmd"
    When I run gtb in the project with "enable mcp"
    Then the project exit code is 0
    And the generated "cmd/feattool/mcp.go" file exists
    And the project manifest does not contain "name: mcp"

  Scenario: The link follows the manifest through regenerate, and a deleted file comes back
    Given a freshly generated gtb project
    When I delete the generated "cmd/feattool/mcp.go" file
    And I run gtb in the project with "regenerate project"
    Then the project exit code is 0
    And the generated "cmd/feattool/mcp.go" file exists
    And the generated "cmd/feattool/keychain.go" file contains "gitlab.com/phpboyscout/go-tool-base/pkg/setup/keychain"

  Scenario: A manifest rebuilt from scratch reads the link's absence
    Given a freshly generated gtb project
    When I run gtb in the project with "disable mcp"
    And I delete the generated ".gtb/manifest.yaml" file
    And I run gtb in the project with "regenerate manifest"
    Then the project exit code is 0
    And the project manifest contains "name: mcp"
    And the generated "cmd/feattool/mcp.go" file does not exist
