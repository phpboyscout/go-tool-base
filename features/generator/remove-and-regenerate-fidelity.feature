@generator @integration
Feature: remove command and regenerate preserve a buildable, faithful project
  Regression guards for the keryx v0.19.0 report:
   - remove command must drop BOTH the import and the registration call;
   - regenerate manifest must preserve command Short/Long descriptions;
   - subcommand docs must be keyed by the full command path, not the leaf name.

  Scenario: remove command leaves no dangling registration in the root
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name foo --short widget"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file contains "foo.NewCmdFoo"
    When I run gtb in the project with "remove command --name foo"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file does not contain "foo.NewCmdFoo"
    And the generated "pkg/cmd/root/cmd.go" file does not contain "pkg/cmd/foo"

  Scenario: removing a parent's last child reshapes it back to a leaf
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name serve --short serve-it"
    And I run gtb in the project with "generate command --name status --parent serve --short status-it"
    Then the project exit code is 0
    And the generated "pkg/cmd/serve/cmd.go" file contains "setup.GroupRunE"
    When I run gtb in the project with "remove command --name status --parent serve"
    Then the project exit code is 0
    And the generated "pkg/cmd/serve/cmd.go" file does not contain "setup.GroupRunE"
    And the generated "pkg/cmd/serve/cmd.go" file does not contain "status.NewCmdStatus"
    And the generated "pkg/cmd/serve/main.go" file exists

  Scenario: removing a command leaves a hand-edited parent's edit
    # Issue #252: the edit was recorded as generated, and the last removal replaced it.
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name serve --short serve-it"
    And I run gtb in the project with "generate command --name status --parent serve --short status-it"
    And I run gtb in the project with "generate command --name health --parent serve --short health-it"
    And I hand-edit the generated "pkg/cmd/serve/cmd.go" file
    And I run gtb in the project with "remove command --name health --parent serve"
    Then the project exit code is 0
    And the project output contains "has been modified since it was generated"
    And the generated "pkg/cmd/serve/cmd.go" file contains "hand-edited, do not clobber"
    And the generated "pkg/cmd/serve/cmd.go" file does not contain "health.NewCmdHealth"
    When I run gtb in the project with "remove command --name status --parent serve --force"
    Then the project exit code is 0
    And the generated "pkg/cmd/serve/cmd.go" file contains "hand-edited, do not clobber"
    And the generated "pkg/cmd/serve/cmd.go" file does not contain "status.NewCmdStatus"
    When I run gtb in the project with "regenerate project --overwrite deny"
    Then the generated "pkg/cmd/serve/cmd.go" file contains "hand-edited, do not clobber"

  Scenario: removing a command leaves a parent gtb has no hash for
    # A rebuilt manifest records no hashes, and regenerate keeps a file it has none for.
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name serve --short serve-it"
    And I run gtb in the project with "generate command --name status --parent serve --short status-it"
    And I run gtb in the project with "generate command --name health --parent serve --short health-it"
    And I hand-edit the generated "pkg/cmd/serve/cmd.go" file
    And I delete the generated ".gtb/manifest.yaml" file
    And I run gtb in the project with "regenerate manifest"
    And I run gtb in the project with "remove command --name health --parent serve"
    Then the project exit code is 0
    And the generated "pkg/cmd/serve/cmd.go" file contains "hand-edited, do not clobber"
    And the generated "pkg/cmd/serve/cmd.go" file does not contain "health.NewCmdHealth"
    When I run gtb in the project with "remove command --name status --parent serve --force"
    Then the project exit code is 0
    And the generated "pkg/cmd/serve/cmd.go" file contains "hand-edited, do not clobber"
    And the generated "pkg/cmd/serve/cmd.go" file does not contain "status.NewCmdStatus"
    When I run gtb in the project with "regenerate project --overwrite deny"
    Then the generated "pkg/cmd/serve/cmd.go" file contains "hand-edited, do not clobber"

  Scenario: adding a command leaves a parent gtb has no hash for unrecorded
    # Issue #255: adding a child recorded the edit, and a later last removal replaced it.
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name serve --short serve-it"
    And I run gtb in the project with "generate command --name status --parent serve --short status-it"
    And I hand-edit the generated "pkg/cmd/serve/cmd.go" file
    And I delete the generated ".gtb/manifest.yaml" file
    And I run gtb in the project with "regenerate manifest"
    And I run gtb in the project with "generate command --name extra --parent serve --short extra-it"
    Then the project exit code is 0
    And the generated "pkg/cmd/serve/cmd.go" file contains "extra.NewCmdExtra"
    When I run gtb in the project with "remove command --name extra --parent serve"
    And I run gtb in the project with "remove command --name status --parent serve --force"
    Then the project exit code is 0
    And the generated "pkg/cmd/serve/cmd.go" file contains "hand-edited, do not clobber"
    And the generated "pkg/cmd/serve/cmd.go" file does not contain "status.NewCmdStatus"

  Scenario: remove refuses a protected command unless forced
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name guarded --short guarded-widget --protected"
    Then the project exit code is 0
    When I run gtb in the project with "remove command --name guarded"
    Then the project exit code is not zero
    And the project output contains "protected"
    And the generated "pkg/cmd/guarded/cmd.go" file exists
    When I run gtb in the project with "remove command --name guarded --force"
    Then the project exit code is 0
    And the generated "pkg/cmd/guarded/cmd.go" file does not exist

  Scenario: regenerate manifest preserves command descriptions
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name social --short social-media-tools"
    Then the project exit code is 0
    When I run gtb in the project with "regenerate manifest"
    Then the project exit code is 0
    And the project manifest contains "description: social-media-tools"

  Scenario: same-named subcommands under different parents each get their own doc
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name a --short parent-a"
    Then the project exit code is 0
    When I run gtb in the project with "generate command --name b --short parent-b"
    Then the project exit code is 0
    When I run gtb in the project with "generate command --name run --parent a --short a-run"
    Then the project exit code is 0
    When I run gtb in the project with "generate command --name run --parent b --short b-run"
    Then the project exit code is 0
    And the generated "docs/reference/cli/a/run.md" file contains "run"
    And the generated "docs/reference/cli/b/run.md" file contains "run"

  Scenario: Generating a command again keeps what the second call leaves out
    # Issue #230: the aliases left the manifest but stayed in cmd.go until the
    # next regenerate, and an MCP exclusion left cmd.go but not the manifest.
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name deploy --short dep --long ships-it --alias ship --mcp-enabled=false"
    And I run gtb in the project with "generate command --name deploy --short dep --force"
    Then the project exit code is 0
    And the project manifest contains "- ship"
    And the project manifest contains "long_description: ships-it"
    And the generated "pkg/cmd/deploy/cmd.go" file contains "ships-it"
    And the generated "pkg/cmd/deploy/cmd.go" file contains "setup.ExcludeFromMCP(cmd)"
    When I run gtb in the project with "regenerate project --force"
    Then the project exit code is 0
    And the generated "pkg/cmd/deploy/cmd.go" file contains "Aliases: []string{"
