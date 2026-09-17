@generator @integration @annotations
Feature: MCP tool annotations a consumer can set
  gtb annotate records a command's MCP tool annotations (a display title and
  the read-only, destructive, idempotent and open-world hints) in the manifest
  and re-renders the command's cmd.go to carry them through setup.AnnotateMCP,
  which adds ophis's annotation keys beside gtb's own. Each hint is tri-state:
  a bare flag records true, --flag=false records false, an absent flag leaves
  the recorded value alone. Hints do not inherit down a subtree.

  Covers https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0201-gtb-consumes-go-mcp
  and closes https://gitlab.com/phpboyscout/go-tool-base/-/issues/36.

  Scenario: annotate records the hints and renders the call
    Given a gtb project with a "post" command
    When I run gtb in the project with "annotate post --title Publish --read-only=false --open-world"
    Then the project exit code is 0
    And the project manifest contains "mcp_hints:"
    And the project manifest contains "title: Publish"
    And the project manifest contains "read_only: false"
    And the project manifest contains "open_world: true"
    And the project manifest does not contain "destructive"
    And the generated "pkg/cmd/post/cmd.go" file contains 'setup.AnnotateMCP(cmd, setup.MCPHints{Title: "Publish", ReadOnly: new(false), OpenWorld: new(true)})'

  Scenario: a second annotate merges rather than replaces
    Given a gtb project with a "post" command
    When I run gtb in the project with "annotate post --title Publish --read-only"
    And I run gtb in the project with "annotate post --read-only=false --idempotent"
    Then the project exit code is 0
    And the project manifest contains "title: Publish"
    And the project manifest contains "read_only: false"
    And the project manifest contains "idempotent: true"

  Scenario: clear removes the block and the call
    Given a gtb project with a "post" command
    When I run gtb in the project with "annotate post --destructive"
    And I run gtb in the project with "annotate post --clear"
    Then the project exit code is 0
    And the project manifest does not contain "mcp_hints"
    And the generated "pkg/cmd/post/cmd.go" file does not contain "AnnotateMCP"

  Scenario: annotations survive a regenerate and reconstruct from the code
    Given a gtb project with a "post" command
    When I run gtb in the project with "annotate post --title Publish --open-world"
    And I run gtb in the project with "regenerate project"
    Then the project exit code is 0
    And the generated "pkg/cmd/post/cmd.go" file contains 'setup.MCPHints{Title: "Publish", OpenWorld: new(true)}'
    When I run gtb in the project with "regenerate manifest"
    Then the project exit code is 0
    And the project manifest contains "title: Publish"
    And the project manifest contains "open_world: true"

  Scenario: annotate refuses an invocation that states nothing
    Given a gtb project with a "post" command
    When I run gtb in the project with "annotate post"
    Then the project exit code is not zero
    And the project manifest does not contain "mcp_hints"
